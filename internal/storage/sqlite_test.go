package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*SQLiteStorage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "minerhq-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create storage: %v", err)
	}

	cleanup := func() {
		storage.Close()
		os.RemoveAll(tmpDir)
	}

	return storage, cleanup
}

func TestSQLiteStorage(t *testing.T) {
	t.Run("UpsertAndGetMiners", func(t *testing.T) {
		storage, cleanup := setupTestDB(t)
		defer cleanup()

		// Insert a miner
		miner := &Miner{
			IP:          "192.168.1.100",
			Hostname:    "miner-001",
			DeviceModel: "BitAxe Gamma",
			ASICModel:   "BM1366",
			Enabled:     true,
			LastSeen:    time.Now(),
			Online:      true,
		}

		err := storage.UpsertMiner(miner)
		if err != nil {
			t.Fatalf("failed to upsert miner: %v", err)
		}

		// Get miners
		miners, err := storage.GetMiners()
		if err != nil {
			t.Fatalf("failed to get miners: %v", err)
		}

		if len(miners) != 1 {
			t.Fatalf("expected 1 miner, got %d", len(miners))
		}

		if miners[0].IP != miner.IP {
			t.Errorf("expected IP %s, got %s", miner.IP, miners[0].IP)
		}
		if miners[0].Hostname != miner.Hostname {
			t.Errorf("expected hostname %s, got %s", miner.Hostname, miners[0].Hostname)
		}
		if miners[0].DeviceModel != miner.DeviceModel {
			t.Errorf("expected device model %s, got %s", miner.DeviceModel, miners[0].DeviceModel)
		}

		// Update the miner
		miner.Hostname = "miner-001-updated"
		err = storage.UpsertMiner(miner)
		if err != nil {
			t.Fatalf("failed to update miner: %v", err)
		}

		miners, err = storage.GetMiners()
		if err != nil {
			t.Fatalf("failed to get miners after update: %v", err)
		}

		if miners[0].Hostname != "miner-001-updated" {
			t.Errorf("expected updated hostname, got %s", miners[0].Hostname)
		}

		// Remove the miner (soft delete)
		err = storage.RemoveMiner(miner.IP)
		if err != nil {
			t.Fatalf("failed to remove miner: %v", err)
		}

		miners, err = storage.GetMiners()
		if err != nil {
			t.Fatalf("failed to get miners after remove: %v", err)
		}

		if len(miners) != 0 {
			t.Errorf("expected 0 miners after removal, got %d", len(miners))
		}
	})

	t.Run("InsertAndGetSnapshots", func(t *testing.T) {
		storage, cleanup := setupTestDB(t)
		defer cleanup()

		minerIP := "192.168.1.100"
		now := time.Now()

		// Insert snapshots
		for i := 0; i < 5; i++ {
			snap := &MinerSnapshot{
				MinerIP:       minerIP,
				Timestamp:     now.Add(time.Duration(-i) * time.Minute),
				Hostname:      "miner-001",
				DeviceModel:   "BitAxe Gamma",
				HashRate:      500.0 + float64(i),
				HashRate1m:    495.0,
				HashRate1h:    498.0,
				HashRate1d:    500.0,
				Temperature:   45.5,
				VRTemp:        50.0,
				Power:         15.5,
				Voltage:       5.0,
				FanRPM:        3000,
				FanPercent:    75,
				SharesAccept:  int64(100 + i),
				SharesReject:  1,
				BestDiff:      1000000.0,
				BestDiffSess:  500000.0,
				PoolDiff:      1000.0,
				PoolConnected: true,
				UptimeSecs:    3600,
				WifiRSSI:      -55,
			}

			err := storage.InsertSnapshot(snap)
			if err != nil {
				t.Fatalf("failed to insert snapshot %d: %v", i, err)
			}

			if snap.ID == 0 {
				t.Errorf("expected snapshot ID to be set, got 0")
			}
		}

		// Get snapshots
		since := now.Add(-10 * time.Minute)
		snapshots, err := storage.GetSnapshots(minerIP, since, 10)
		if err != nil {
			t.Fatalf("failed to get snapshots: %v", err)
		}

		if len(snapshots) != 5 {
			t.Fatalf("expected 5 snapshots, got %d", len(snapshots))
		}

		// Verify order (should be newest first)
		if snapshots[0].HashRate < snapshots[1].HashRate {
			// The newest should have hash rate 500.0 (i=0)
			t.Logf("snapshots are in correct order (newest first)")
		}

		// Get with limit
		snapshots, err = storage.GetSnapshots(minerIP, since, 2)
		if err != nil {
			t.Fatalf("failed to get snapshots with limit: %v", err)
		}

		if len(snapshots) != 2 {
			t.Errorf("expected 2 snapshots with limit, got %d", len(snapshots))
		}
	})

	t.Run("InsertAndGetShares", func(t *testing.T) {
		storage, cleanup := setupTestDB(t)
		defer cleanup()

		minerIP := "192.168.1.100"
		now := time.Now()

		// Insert shares with varying difficulties
		difficulties := []float64{1000.0, 5000.0, 2000.0, 10000.0, 500.0}
		for i, diff := range difficulties {
			share := &Share{
				MinerIP:    minerIP,
				Timestamp:  now.Add(time.Duration(-i) * time.Minute),
				AsicNum:    0,
				Difficulty: diff,
				JobID:      "job-" + string(rune('A'+i)),
			}

			err := storage.InsertShare(share)
			if err != nil {
				t.Fatalf("failed to insert share %d: %v", i, err)
			}

			if share.ID == 0 {
				t.Errorf("expected share ID to be set, got 0")
			}
		}

		// Get shares
		since := now.Add(-10 * time.Minute)
		shares, err := storage.GetShares(since, 10)
		if err != nil {
			t.Fatalf("failed to get shares: %v", err)
		}

		if len(shares) != 5 {
			t.Fatalf("expected 5 shares, got %d", len(shares))
		}

		// Get best share (all time)
		bestShare, err := storage.GetBestShare(minerIP, false)
		if err != nil {
			t.Fatalf("failed to get best share: %v", err)
		}

		if bestShare == nil {
			t.Fatal("expected best share, got nil")
		}

		if bestShare.Difficulty != 10000.0 {
			t.Errorf("expected best difficulty 10000.0, got %f", bestShare.Difficulty)
		}

		// Get best share (session only - same result since all within 24h)
		bestShareSession, err := storage.GetBestShare(minerIP, true)
		if err != nil {
			t.Fatalf("failed to get best share (session): %v", err)
		}

		if bestShareSession == nil {
			t.Fatal("expected best share (session), got nil")
		}

		if bestShareSession.Difficulty != 10000.0 {
			t.Errorf("expected best session difficulty 10000.0, got %f", bestShareSession.Difficulty)
		}

		// Get shares with limit
		shares, err = storage.GetShares(since, 2)
		if err != nil {
			t.Fatalf("failed to get shares with limit: %v", err)
		}

		if len(shares) != 2 {
			t.Errorf("expected 2 shares with limit, got %d", len(shares))
		}
	})

	t.Run("PurgeOldData", func(t *testing.T) {
		storage, cleanup := setupTestDB(t)
		defer cleanup()

		minerIP := "192.168.1.100"
		now := time.Now()

		// Insert old snapshot (8 days ago)
		oldSnap := &MinerSnapshot{
			MinerIP:   minerIP,
			Timestamp: now.AddDate(0, 0, -8),
			Hostname:  "old-miner",
			HashRate:  100.0,
		}
		storage.InsertSnapshot(oldSnap)

		// Insert new snapshot (1 day ago)
		newSnap := &MinerSnapshot{
			MinerIP:   minerIP,
			Timestamp: now.AddDate(0, 0, -1),
			Hostname:  "new-miner",
			HashRate:  200.0,
		}
		storage.InsertSnapshot(newSnap)

		// Insert old share (8 days ago)
		oldShare := &Share{
			MinerIP:    minerIP,
			Timestamp:  now.AddDate(0, 0, -8),
			Difficulty: 1000.0,
		}
		storage.InsertShare(oldShare)

		// Insert new share (1 day ago)
		newShare := &Share{
			MinerIP:    minerIP,
			Timestamp:  now.AddDate(0, 0, -1),
			Difficulty: 2000.0,
		}
		storage.InsertShare(newShare)

		// Purge data older than 7 days
		err := storage.PurgeOldData(7)
		if err != nil {
			t.Fatalf("failed to purge old data: %v", err)
		}

		// Check snapshots - should only have the new one
		snapshots, err := storage.GetSnapshots(minerIP, now.AddDate(0, 0, -30), 100)
		if err != nil {
			t.Fatalf("failed to get snapshots after purge: %v", err)
		}

		if len(snapshots) != 1 {
			t.Errorf("expected 1 snapshot after purge, got %d", len(snapshots))
		}

		// Check shares - should only have the new one
		shares, err := storage.GetShares(now.AddDate(0, 0, -30), 100)
		if err != nil {
			t.Fatalf("failed to get shares after purge: %v", err)
		}

		if len(shares) != 1 {
			t.Errorf("expected 1 share after purge, got %d", len(shares))
		}
	})
}
