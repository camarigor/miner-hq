package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/camarigor/miner-hq/internal/alerts"
	"github.com/camarigor/miner-hq/internal/api"
	"github.com/camarigor/miner-hq/internal/collector"
	"github.com/camarigor/miner-hq/internal/config"
	"github.com/camarigor/miner-hq/internal/pricing"
	"github.com/camarigor/miner-hq/internal/storage"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	log.Println("MinerHQ starting...")

	// Ensure /data directory exists for persistence
	if err := os.MkdirAll("/data", 0755); err != nil {
		log.Printf("Warning: could not create /data directory: %v", err)
	}

	// Load config (use defaults if file doesn't exist)
	cfg, err := config.Load(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file not found at %s, using defaults", *configPath)
			cfg = config.DefaultConfig()
			// Save default config so it persists
			if saveErr := cfg.Save(*configPath); saveErr != nil {
				log.Printf("Warning: could not save default config: %v", saveErr)
			}
		} else {
			log.Fatalf("Failed to load config: %v", err)
		}
	}

	// Determine database path and ensure parent directory exists
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "minerhq.db"
	}

	// Ensure parent directory exists for database file
	dbDir := filepath.Dir(dbPath)
	if dbDir != "" && dbDir != "." {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			log.Fatalf("Failed to create data directory: %v", err)
		}
	}

	// Initialize storage
	store, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()
	log.Printf("Database initialized at %s", dbPath)

	// Vacuum database on startup to reclaim space from previous purges
	if err := store.Vacuum(); err != nil {
		log.Printf("Warning: database vacuum failed: %v", err)
	} else {
		log.Println("Database vacuumed successfully")
	}

	// Initialize pricing service
	priceSvc := pricing.NewPriceService()
	// Start block reward updater (once per day)
	priceSvc.StartBlockRewardUpdater(24 * time.Hour)
	log.Println("Pricing service started (per-miner coins, on-demand price fetching)")

	// Initialize alert engine
	alertConfig := &alerts.AlertConfig{
		WebhookURL:          cfg.Alerts.WebhookURL,
		MinerOfflineSeconds: cfg.Alerts.OfflineMinutes * 60,
		TempAbove:           cfg.Alerts.TempThresholdC,
		HashrateDropPercent: cfg.Alerts.HashrateDropPct,
		FanRPMBelow:         cfg.Alerts.FanRPMBelow,
		WifiSignalBelow:     cfg.Alerts.WifiSignalBelow,
		OnShareRejected:     cfg.Alerts.OnShareRejected,
		OnPoolDisconnected:  cfg.Alerts.OnPoolDisconnected,
		OnNewBestDiff:       cfg.Alerts.OnNewBestDiff,
		OnBlockFound:        cfg.Alerts.OnBlockFound,
		OnNewLeader:         cfg.Alerts.OnNewLeader,
	}
	alertEngine := alerts.NewAlertEngine(alertConfig)
	log.Println("Alert engine initialized")

	// Initialize collector (with pricing service for block value tracking)
	coll := collector.NewCollector(store, priceSvc)

	// Load existing miners and start collecting
	miners, err := store.GetMiners()
	if err != nil {
		log.Printf("Warning: could not load miners: %v", err)
	} else if len(miners) > 0 {
		log.Printf("Starting collection for %d miners", len(miners))
		// Convert []*storage.Miner to []storage.Miner for collector.Start
		minerList := make([]storage.Miner, len(miners))
		for i, m := range miners {
			minerList[i] = *m
		}
		coll.Start(minerList)
	}

	// Start data retention cleanup (daily)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			days := cfg.Retention.MetricsRetentionDays
			if days <= 0 {
				days = 30
			}
			if err := store.PurgeOldData(days); err != nil {
				log.Printf("Data purge error: %v", err)
			} else {
				log.Printf("Purged data older than %d days", days)
			}
			// Vacuum to reclaim disk space
			if err := store.Vacuum(); err != nil {
				log.Printf("Daily vacuum error: %v", err)
			} else {
				log.Println("Daily vacuum completed")
			}
		}
	}()

	// Start hourly snapshot purge (keep only last hour for real-time display)
	go func() {
		// Run immediately on startup
		deletedSnaps, err := store.PurgeOldSnapshots(1)
		if err != nil {
			log.Printf("Snapshot purge error: %v", err)
		} else if deletedSnaps > 0 {
			log.Printf("Purged %d snapshots older than 1 hour", deletedSnaps)
		}

		// Then run every hour
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			deletedSnaps, err := store.PurgeOldSnapshots(1)
			if err != nil {
				log.Printf("Snapshot purge error: %v", err)
			} else {
				log.Printf("Hourly purge: removed %d old snapshots", deletedSnaps)
			}
		}
	}()

	// Start weekly share purge (Sunday at midnight) to preserve weekly best share history
	go func() {
		for {
			now := time.Now()
			// Calculate next Sunday at midnight
			daysUntilSunday := (7 - int(now.Weekday())) % 7
			if daysUntilSunday == 0 && now.Hour() >= 0 {
				// If it's already Sunday past midnight, wait until next Sunday
				daysUntilSunday = 7
			}
			nextSunday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 0, 0, 0, 0, now.Location())
			waitDuration := nextSunday.Sub(now)

			log.Printf("Weekly share purge scheduled for %s (in %v)", nextSunday.Format("2006-01-02 15:04:05"), waitDuration.Round(time.Minute))

			time.Sleep(waitDuration)

			// Purge shares older than 8 days (keeps 7 full days visible in the UI)
			deleted, err := store.PurgeOldShares(192) // 192 hours = 8 days
			if err != nil {
				log.Printf("Weekly share purge error: %v", err)
			} else {
				log.Printf("Weekly purge: removed %d old shares", deleted)
			}
			// Vacuum to reclaim disk space after weekly purge
			if err := store.Vacuum(); err != nil {
				log.Printf("Weekly vacuum error: %v", err)
			} else {
				log.Println("Weekly vacuum completed")
			}
		}
	}()

	// Initialize and start HTTP server
	server := api.NewServer(cfg, store, coll, priceSvc, alertEngine)
	go func() {
		log.Printf("HTTP server starting on http://%s:%d", cfg.Server.Host, cfg.Server.Port)
		if err := server.Start(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	log.Println("MinerHQ is running. Press Ctrl+C to stop.")

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("MinerHQ shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	coll.Stop()
	if err := server.Stop(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("MinerHQ stopped")
}
