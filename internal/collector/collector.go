package collector

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/camarigor/miner-hq/internal/pricing"
	"github.com/camarigor/miner-hq/internal/storage"
)

type Collector struct {
	storage      *storage.SQLiteStorage
	pricing      *pricing.PriceService
	client       *MinerClient
	parser       *ShareParser
	blockParser  *BlockParser
	miners       map[string]*minerConn
	minersMu     sync.RWMutex
	pollInterval time.Duration

	// Channels for broadcasting to API WebSocket clients
	ShareChan    chan *storage.Share
	SnapshotChan chan *storage.MinerSnapshot
	BlockChan    chan *storage.Block
}

type minerConn struct {
	ip       string
	wsConn   *websocket.Conn
	cancel   context.CancelFunc
	lastSeen time.Time
}

func NewCollector(store *storage.SQLiteStorage, priceSvc *pricing.PriceService) *Collector {
	return &Collector{
		storage:      store,
		pricing:      priceSvc,
		client:       NewMinerClient(),
		parser:       NewShareParser(),
		blockParser:  NewBlockParser(),
		miners:       make(map[string]*minerConn),
		pollInterval: 2 * time.Second,
		ShareChan:    make(chan *storage.Share, 100),
		SnapshotChan: make(chan *storage.MinerSnapshot, 100),
		BlockChan:    make(chan *storage.Block, 10),
	}
}

// AddMiner starts collecting data from a miner
func (c *Collector) AddMiner(ip string) {
	c.minersMu.Lock()
	defer c.minersMu.Unlock()

	if _, exists := c.miners[ip]; exists {
		return // Already monitoring
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn := &minerConn{
		ip:     ip,
		cancel: cancel,
	}
	c.miners[ip] = conn

	// Start polling goroutine
	go c.pollMiner(ctx, ip)

	// Start WebSocket goroutine
	go c.connectWebSocket(ctx, ip)
}

// RemoveMiner stops collecting from a miner
func (c *Collector) RemoveMiner(ip string) {
	c.minersMu.Lock()
	defer c.minersMu.Unlock()

	if conn, exists := c.miners[ip]; exists {
		conn.cancel()
		if conn.wsConn != nil {
			conn.wsConn.Close()
		}
		delete(c.miners, ip)
	}
}

// pollMiner polls the REST API every pollInterval
func (c *Collector) pollMiner(ctx context.Context, ip string) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	// Initial poll
	c.fetchAndStore(ip)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.fetchAndStore(ip)
		}
	}
}

// fetchAndStore fetches miner info and stores snapshot
func (c *Collector) fetchAndStore(ip string) {
	info, err := c.client.FetchInfo(ip)
	if err != nil {
		log.Printf("Poll %s failed: %v", ip, err)
		return
	}

	// Update miner record
	miner := c.client.ToMiner(ip, info)
	if err := c.storage.UpsertMiner(miner); err != nil {
		log.Printf("UpsertMiner %s failed: %v", ip, err)
	}

	// Store snapshot
	snapshot := c.client.ToSnapshot(ip, info)
	if err := c.storage.InsertSnapshot(snapshot); err != nil {
		log.Printf("InsertSnapshot %s failed: %v", ip, err)
	}

	// Update last seen
	c.minersMu.Lock()
	if conn, exists := c.miners[ip]; exists {
		conn.lastSeen = time.Now()
	}
	c.minersMu.Unlock()

	// Broadcast to WebSocket clients (non-blocking)
	select {
	case c.SnapshotChan <- snapshot:
	default:
	}
}

// connectWebSocket maintains a persistent WebSocket connection
func (c *Collector) connectWebSocket(ctx context.Context, ip string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		u := url.URL{Scheme: "ws", Host: ip, Path: "/api/ws"}

		conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Printf("WebSocket connect %s failed: %v", ip, err)
			time.Sleep(5 * time.Second)
			continue
		}

		c.minersMu.Lock()
		if mc, exists := c.miners[ip]; exists {
			mc.wsConn = conn
		}
		c.minersMu.Unlock()

		log.Printf("WebSocket connected to %s", ip)

		// Get miner hostname for share attribution
		hostname := ip
		miners, _ := c.storage.GetMiners()
		for _, m := range miners {
			if m.IP == ip {
				hostname = m.Hostname
				break
			}
		}

		// Read messages until error
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read %s error: %v", ip, err)
				conn.Close()
				break
			}

			// Parse share from message
			share := c.parser.Parse(ip, string(message))
			if share != nil {
				share.Hostname = hostname

				if err := c.storage.InsertShare(share); err != nil {
					log.Printf("InsertShare failed: %v", err)
				}

				// Broadcast (non-blocking)
				select {
				case c.ShareChan <- share:
				default:
				}
			}

			// Parse block from message
			block := c.blockParser.Parse(ip, string(message))
			if block != nil {
				block.Hostname = hostname

				// Populate value tracking fields from pricing service
				// Use per-miner coin if configured, otherwise fall back to global
				if c.pricing != nil {
					var coin *pricing.Coin
					miners, _ := c.storage.GetMiners()
					for _, m := range miners {
						if m.IP == ip && m.CoinID != "" {
							coin = c.pricing.GetCoinInfoByID(m.CoinID)
							break
						}
					}
					if coin == nil {
						coin = c.pricing.GetCoinInfoByID("dgb") // default fallback
					}
					if coin != nil {
						block.CoinID = coin.ID
						block.CoinSymbol = coin.Symbol
						block.BlockReward = coin.BlockReward
						block.CoinPrice = c.pricing.GetPriceForCoin(coin.ID)
						block.ValueUSD = block.BlockReward * block.CoinPrice
					}
				}

				log.Printf("BLOCK FOUND by %s (%s)! Diff: %.0f > Network: %.0f | Value: %.2f %s ($%.2f)",
					hostname, ip, block.Difficulty, block.NetworkDifficulty,
					block.BlockReward, block.CoinSymbol, block.ValueUSD)

				if err := c.storage.InsertBlock(block); err != nil {
					log.Printf("InsertBlock failed: %v", err)
				}

				// Broadcast (non-blocking)
				select {
				case c.BlockChan <- block:
				default:
				}
			}
		}

		// Wait before reconnecting
		time.Sleep(5 * time.Second)
	}
}

// GetMinerStatus returns online status for all miners
func (c *Collector) GetMinerStatus() map[string]bool {
	c.minersMu.RLock()
	defer c.minersMu.RUnlock()

	status := make(map[string]bool)
	for ip, conn := range c.miners {
		status[ip] = time.Since(conn.lastSeen) < 30*time.Second
	}
	return status
}

// Start begins collecting from a list of miners
func (c *Collector) Start(miners []storage.Miner) {
	for _, m := range miners {
		if m.Enabled {
			c.AddMiner(m.IP)
		}
	}
}

// Stop stops all collection
func (c *Collector) Stop() {
	c.minersMu.Lock()
	defer c.minersMu.Unlock()

	for ip, conn := range c.miners {
		conn.cancel()
		if conn.wsConn != nil {
			conn.wsConn.Close()
		}
		delete(c.miners, ip)
	}

	close(c.ShareChan)
	close(c.SnapshotChan)
	close(c.BlockChan)
}
