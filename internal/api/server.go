package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/camarigor/miner-hq/internal/alerts"
	"github.com/camarigor/miner-hq/internal/collector"
	"github.com/camarigor/miner-hq/internal/config"
	"github.com/camarigor/miner-hq/internal/pricing"
	"github.com/camarigor/miner-hq/internal/scanner"
	"github.com/camarigor/miner-hq/internal/storage"
)

// Server represents the HTTP API server
type Server struct {
	cfg       *config.Config
	storage   *storage.SQLiteStorage
	collector *collector.Collector
	scanner   *scanner.Scanner
	pricing   *pricing.PriceService
	alerts    *alerts.AlertEngine
	hub       *WebSocketHub
	server    *http.Server
}

// NewServer creates a new API server
func NewServer(cfg *config.Config, store *storage.SQLiteStorage, coll *collector.Collector, price *pricing.PriceService, alertEngine *alerts.AlertEngine) *Server {
	return &Server{
		cfg:       cfg,
		storage:   store,
		collector: coll,
		scanner:   scanner.NewScanner(),
		pricing:   price,
		alerts:    alertEngine,
		hub:       NewWebSocketHub(),
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	// Start WebSocket hub
	go s.hub.Run()

	// Start event forwarding from collector
	go s.forwardEvents()

	// Setup chi router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Miners
		r.Get("/miners", s.handleGetMiners)
		r.Post("/miners", s.handleAddMiner)
		r.Get("/miners/{ip}", s.handleGetMiner)
		r.Delete("/miners/{ip}", s.handleRemoveMiner)
		r.Get("/miners/{ip}/history", s.handleGetMinerHistory)
		r.Put("/miners/{ip}/coin", s.handleSetMinerCoin)

		// Stats
		r.Get("/stats", s.handleGetStats)

		// History (aggregated)
		r.Get("/history", s.handleGetHistory)

		// Shares
		r.Get("/shares", s.handleGetShares)
		r.Get("/shares/best", s.handleGetBestShares)

		// Blocks
		r.Get("/blocks", s.handleGetBlocks)
		r.Get("/blocks/count", s.handleGetBlockCount)

		// Competition
		r.Get("/competition/weekly", s.handleGetWeeklyCompetition)
		r.Get("/competition/moneymakers", s.handleGetMoneyMakers)

		// Settings
		r.Get("/settings", s.handleGetSettings)
		r.Post("/settings", s.handleSaveSettings)

		// Alerts
		r.Post("/alerts/test", s.handleTestAlert)

		// Network scan
		r.Post("/scan", s.handleScan)

		// Pricing
		r.Get("/coins", s.handleGetCoins)

		// Earnings
		r.Get("/earnings", s.handleGetEarnings)

		// Database management
		r.Get("/dbsize", s.handleGetDBSize)
		r.Post("/purge", s.handlePurge)

		// WebSocket
		r.Get("/ws", s.handleWebSocket)
	})

	// Static files
	r.Get("/*", s.handleStatic)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  s.cfg.Server.ReadTimeout,
		WriteTimeout: s.cfg.Server.WriteTimeout,
	}

	log.Printf("Starting HTTP server on %s", addr)
	return s.server.ListenAndServe()
}

// Stop stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	// Stop WebSocket hub
	s.hub.Stop()

	// Shutdown HTTP server
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// initWeeklyLeader loads the current weekly leader from the database
// so a container restart doesn't trigger false "new leader" alerts.
func (s *Server) initWeeklyLeader() {
	if s.alerts == nil {
		return
	}

	now := time.Now()
	weekday := int(now.Weekday())
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())

	miners, err := s.storage.GetMiners()
	if err != nil {
		log.Printf("Failed to load miners for weekly leader init: %v", err)
		return
	}

	var bestDiff float64
	var leader string
	for _, m := range miners {
		share, err := s.storage.GetBestShareInRange(m.IP, weekStart, now)
		if err != nil || share == nil {
			continue
		}
		if share.Difficulty > bestDiff {
			bestDiff = share.Difficulty
			leader = share.Hostname
		}
	}

	s.alerts.InitWeeklyLeader(leader, bestDiff)
}

// forwardEvents forwards collector events to WebSocket hub
func (s *Server) forwardEvents() {
	s.initWeeklyLeader()

	for {
		select {
		case share, ok := <-s.collector.ShareChan:
			if !ok {
				return
			}
			s.hub.Broadcast(Message{
				Type: "share",
				Data: share,
			})
			if s.alerts != nil {
				s.alerts.CheckLeaderChange(share)
			}

		case snapshot, ok := <-s.collector.SnapshotChan:
			if !ok {
				return
			}
			// Check for alerts
			if s.alerts != nil {
				s.alerts.CheckSnapshot(snapshot)
			}

			s.hub.Broadcast(Message{
				Type: "snapshot",
				Data: snapshot,
			})

		case block, ok := <-s.collector.BlockChan:
			if !ok {
				return
			}
			log.Printf("Broadcasting block found event from %s", block.Hostname)
			s.hub.Broadcast(Message{
				Type: "block",
				Data: block,
			})
			if s.alerts != nil {
				s.alerts.CheckBlock(block)
			}
		}
	}
}

// GetHub returns the WebSocket hub for external access
func (s *Server) GetHub() *WebSocketHub {
	return s.hub
}
