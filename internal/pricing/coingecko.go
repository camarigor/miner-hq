package pricing

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Coin represents a supported cryptocurrency
type Coin struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Symbol      string  `json:"symbol"`      // Display symbol (BTC, DGB, etc.)
	Icon        string  `json:"icon"`        // Icon URL
	Binance     string  `json:"binance"`     // Binance trading pair (BTCUSDT, etc.) - empty if not on Binance
	CoinGecko   string  `json:"coingecko"`   // CoinGecko ID for fallback
	BlockReward float64 `json:"blockReward"` // Current block reward (updated from letsmine.it)
}

// SupportedCoins lists all available coins
var SupportedCoins = []Coin{
	{ID: "btc", Name: "Bitcoin", Symbol: "BTC", Icon: "https://assets.coingecko.com/coins/images/1/small/bitcoin.png", Binance: "BTCUSDT", CoinGecko: "bitcoin", BlockReward: 3.125},
	{ID: "bch", Name: "Bitcoin Cash", Symbol: "BCH", Icon: "https://assets.coingecko.com/coins/images/780/small/bitcoin-cash-circle.png", Binance: "BCHUSDT", CoinGecko: "bitcoin-cash", BlockReward: 3.125},
	{ID: "dgb", Name: "DigiByte", Symbol: "DGB", Icon: "https://assets.coingecko.com/coins/images/63/small/digibyte.png", Binance: "DGBUSDT", CoinGecko: "digibyte", BlockReward: 274.28},
	{ID: "xec", Name: "eCash", Symbol: "XEC", Icon: "https://assets.coingecko.com/coins/images/16646/small/Logo_final-22.png", Binance: "XECUSDT", CoinGecko: "ecash", BlockReward: 1812500},
	{ID: "bc2", Name: "BitcoinII", Symbol: "BC2", Icon: "https://bitcoin-ii.org/logo.png", Binance: "", CoinGecko: "bitcoinii", BlockReward: 50},
	{ID: "btcs", Name: "Fractal Bitcoin", Symbol: "BTCS", Icon: "https://fractalbitcoin.io/img/logo/fractal.svg", Binance: "", CoinGecko: "fractal-bitcoin", BlockReward: 50},
}

// blockRewards stores dynamically fetched block rewards
var blockRewards = make(map[string]float64)
var blockRewardsMu sync.RWMutex

// PriceService fetches and caches coin prices from Binance/CoinGecko
type PriceService struct {
	client *http.Client
}

// BinanceResponse represents the Binance API response
type BinanceResponse struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// NewPriceService creates a new price service
func NewPriceService() *PriceService {
	return &PriceService{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetCoinInfoByID returns info about a specific coin by its ID
func (p *PriceService) GetCoinInfoByID(coinID string) *Coin {
	for i := range SupportedCoins {
		if SupportedCoins[i].ID == coinID {
			coin := SupportedCoins[i]
			blockRewardsMu.RLock()
			if reward, ok := blockRewards[coinID]; ok {
				coin.BlockReward = reward
			}
			blockRewardsMu.RUnlock()
			return &coin
		}
	}
	return nil
}

// priceCache stores prices for all coins
var priceCache = make(map[string]float64)
var priceCacheMu sync.RWMutex
var priceCacheTime time.Time

// GetPriceForCoin returns the current price for a specific coin
func (p *PriceService) GetPriceForCoin(coinID string) float64 {
	priceCacheMu.RLock()
	price, ok := priceCache[coinID]
	cacheAge := time.Since(priceCacheTime)
	priceCacheMu.RUnlock()

	// Return cached price if fresh (within 5 minutes)
	if ok && cacheAge < 5*time.Minute {
		return price
	}

	// Find coin info
	var coin *Coin
	for i := range SupportedCoins {
		if SupportedCoins[i].ID == coinID {
			coin = &SupportedCoins[i]
			break
		}
	}
	if coin == nil {
		return 0
	}

	// Fetch fresh price
	var fetchedPrice float64
	var err error

	if coin.Binance != "" {
		fetchedPrice, err = p.fetchFromBinance(coin.Binance)
	}
	if fetchedPrice == 0 && coin.CoinGecko != "" {
		fetchedPrice, err = p.fetchFromCoinGecko(coin.CoinGecko)
	}

	if err != nil || fetchedPrice == 0 {
		// Return cached price even if stale
		return price
	}

	// Update cache
	priceCacheMu.Lock()
	priceCache[coinID] = fetchedPrice
	priceCacheTime = time.Now()
	priceCacheMu.Unlock()

	return fetchedPrice
}

// GetAllCoinPrices returns current prices for all supported coins
func (p *PriceService) GetAllCoinPrices() map[string]float64 {
	prices := make(map[string]float64)
	for _, coin := range SupportedCoins {
		prices[coin.ID] = p.GetPriceForCoin(coin.ID)
	}
	return prices
}

// fetchFromBinance fetches price from Binance API
func (p *PriceService) fetchFromBinance(symbol string) (float64, error) {
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	resp, err := p.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch from Binance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Binance returned status %d", resp.StatusCode)
	}

	var data BinanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("failed to decode Binance response: %w", err)
	}

	var price float64
	fmt.Sscanf(data.Price, "%f", &price)
	return price, nil
}

// fetchFromCoinGecko fetches price from CoinGecko API
func (p *PriceService) fetchFromCoinGecko(coinGeckoID string) (float64, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd", coinGeckoID)

	resp, err := p.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch from CoinGecko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CoinGecko returned status %d", resp.StatusCode)
	}

	var data map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("failed to decode CoinGecko response: %w", err)
	}

	if coinData, ok := data[coinGeckoID]; ok {
		if price, ok := coinData["usd"]; ok {
			return price, nil
		}
	}

	return 0, fmt.Errorf("price not found in CoinGecko response")
}

// FetchBlockRewards fetches block rewards from letsmine.it
func (p *PriceService) FetchBlockRewards() error {
	resp, err := p.client.Get("https://letsmine.it/solo")
	if err != nil {
		return fmt.Errorf("failed to fetch letsmine.it: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("letsmine.it returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	html := string(body)

	// Parse block rewards from HTML
	// Pattern: "Block reward" followed by reward value like "274.28 DGB"
	rewards := parseBlockRewards(html)

	blockRewardsMu.Lock()
	for coinID, reward := range rewards {
		blockRewards[coinID] = reward
		log.Printf("Block reward updated: %s = %.2f", strings.ToUpper(coinID), reward)
	}
	blockRewardsMu.Unlock()

	return nil
}

// parseBlockRewards extracts block rewards from letsmine.it HTML
func parseBlockRewards(html string) map[string]float64 {
	rewards := make(map[string]float64)

	// Coin symbols to look for
	coins := map[string]string{
		"BTC":  "btc",
		"BCH":  "bch",
		"DGB":  "dgb",
		"XEC":  "xec",
		"BC2":  "bc2",
		"BTCS": "btcs",
	}

	// Pattern to find reward values like "274.28 DGB" or "50 BC2" or "1812500 XEC"
	for symbol, id := range coins {
		// Try to find pattern: number followed by space and symbol
		pattern := fmt.Sprintf(`([\d.,]+)\s*%s`, symbol)
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) >= 2 {
			// Parse the number
			numStr := strings.ReplaceAll(matches[1], ",", "")
			if reward, err := strconv.ParseFloat(numStr, 64); err == nil && reward > 0 {
				rewards[id] = reward
			}
		}
	}

	return rewards
}

// StartBlockRewardUpdater starts a background goroutine that updates block rewards periodically
func (p *PriceService) StartBlockRewardUpdater(interval time.Duration) {
	go func() {
		// Initial fetch
		if err := p.FetchBlockRewards(); err != nil {
			log.Printf("Initial block reward fetch error: %v", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if err := p.FetchBlockRewards(); err != nil {
				log.Printf("Block reward fetch error: %v", err)
			}
		}
	}()
}

// GetSupportedCoins returns list of supported coins with current block rewards
func GetSupportedCoins() []Coin {
	coins := make([]Coin, len(SupportedCoins))
	copy(coins, SupportedCoins)

	// Update with dynamic block rewards
	blockRewardsMu.RLock()
	for i := range coins {
		if reward, ok := blockRewards[coins[i].ID]; ok {
			coins[i].BlockReward = reward
		}
	}
	blockRewardsMu.RUnlock()

	return coins
}

