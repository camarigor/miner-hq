package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStorage provides SQLite-based storage for miner data
type SQLiteStorage struct {
	db *sql.DB
}

// parseTimestamp parses a timestamp string from SQLite in multiple formats.
// All timestamps are stored in UTC.
func parseTimestamp(s string) time.Time {
	// Try RFC3339 first (modernc/sqlite driver converts DATETIME columns to this format)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Fallback to simple format (stored as UTC)
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}

// NewSQLiteStorage opens a SQLite database at the given path,
// runs migrations, and enables WAL mode
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Limit to single connection to avoid SQLite locking issues
	db.SetMaxOpenConns(1)

	// Set busy timeout to 5 seconds to handle concurrent writes
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	s := &SQLiteStorage{db: db}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

// migrate creates the necessary tables and indexes
func (s *SQLiteStorage) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS miners (
		ip TEXT PRIMARY KEY,
		hostname TEXT NOT NULL DEFAULT '',
		device_model TEXT NOT NULL DEFAULT '',
		asic_model TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		online INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS miner_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_ip TEXT NOT NULL,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		hostname TEXT NOT NULL DEFAULT '',
		device_model TEXT NOT NULL DEFAULT '',
		hash_rate REAL NOT NULL DEFAULT 0,
		hash_rate_1m REAL NOT NULL DEFAULT 0,
		hash_rate_10m REAL NOT NULL DEFAULT 0,
		hash_rate_1h REAL NOT NULL DEFAULT 0,
		hash_rate_1d REAL NOT NULL DEFAULT 0,
		temperature REAL NOT NULL DEFAULT 0,
		vr_temp REAL NOT NULL DEFAULT 0,
		power REAL NOT NULL DEFAULT 0,
		voltage REAL NOT NULL DEFAULT 0,
		fan_rpm INTEGER NOT NULL DEFAULT 0,
		fan_percent INTEGER NOT NULL DEFAULT 0,
		shares_accepted INTEGER NOT NULL DEFAULT 0,
		shares_rejected INTEGER NOT NULL DEFAULT 0,
		best_diff REAL NOT NULL DEFAULT 0,
		best_diff_session REAL NOT NULL DEFAULT 0,
		pool_difficulty REAL NOT NULL DEFAULT 0,
		pool_connected INTEGER NOT NULL DEFAULT 0,
		uptime_seconds INTEGER NOT NULL DEFAULT 0,
		wifi_rssi INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_miner_snapshots_miner_ip ON miner_snapshots(miner_ip);
	CREATE INDEX IF NOT EXISTS idx_miner_snapshots_timestamp ON miner_snapshots(timestamp);
	CREATE INDEX IF NOT EXISTS idx_miner_snapshots_miner_timestamp ON miner_snapshots(miner_ip, timestamp);

	CREATE TABLE IF NOT EXISTS shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_ip TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		asic_num INTEGER NOT NULL DEFAULT 0,
		difficulty REAL NOT NULL DEFAULT 0,
		job_id TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_shares_miner_ip ON shares(miner_ip);
	CREATE INDEX IF NOT EXISTS idx_shares_timestamp ON shares(timestamp);
	CREATE INDEX IF NOT EXISTS idx_shares_difficulty ON shares(difficulty);

	CREATE TABLE IF NOT EXISTS blocks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_ip TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		difficulty REAL NOT NULL DEFAULT 0,
		network_difficulty REAL NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_blocks_miner_ip ON blocks(miner_ip);
	CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add hostname column to shares if it doesn't exist
	_, _ = s.db.Exec("ALTER TABLE shares ADD COLUMN hostname TEXT NOT NULL DEFAULT ''")

	// Migration: add hash_rate_10m column if it doesn't exist
	_, _ = s.db.Exec("ALTER TABLE miner_snapshots ADD COLUMN hash_rate_10m REAL NOT NULL DEFAULT 0")

	// Migration: add block counters if they don't exist
	_, _ = s.db.Exec("ALTER TABLE miner_snapshots ADD COLUMN found_blocks INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE miner_snapshots ADD COLUMN total_found_blocks INTEGER NOT NULL DEFAULT 0")

	// Migration: add per-miner coin override
	_, _ = s.db.Exec("ALTER TABLE miners ADD COLUMN coin_id TEXT NOT NULL DEFAULT ''")

	// Migration: add value tracking columns to blocks table
	_, _ = s.db.Exec("ALTER TABLE blocks ADD COLUMN coin_id TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE blocks ADD COLUMN coin_symbol TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE blocks ADD COLUMN block_reward REAL NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE blocks ADD COLUMN coin_price REAL NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE blocks ADD COLUMN value_usd REAL NOT NULL DEFAULT 0")

	return nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// UpsertMiner inserts or updates a miner record
func (s *SQLiteStorage) UpsertMiner(m *Miner) error {
	query := `
	INSERT INTO miners (ip, hostname, device_model, asic_model, enabled, last_seen, online)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(ip) DO UPDATE SET
		hostname = excluded.hostname,
		device_model = excluded.device_model,
		asic_model = excluded.asic_model,
		enabled = excluded.enabled,
		last_seen = excluded.last_seen,
		online = excluded.online
	`

	_, err := s.db.Exec(query, m.IP, m.Hostname, m.DeviceModel, m.ASICModel, m.Enabled, m.LastSeen, m.Online)
	return err
}

// GetMiners returns all enabled miners
func (s *SQLiteStorage) GetMiners() ([]*Miner, error) {
	query := `
	SELECT ip, hostname, device_model, asic_model, enabled, last_seen, online, COALESCE(coin_id, '')
	FROM miners
	WHERE enabled = 1
	ORDER BY ip
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var miners []*Miner
	for rows.Next() {
		m := &Miner{}
		var lastSeen string
		err := rows.Scan(&m.IP, &m.Hostname, &m.DeviceModel, &m.ASICModel, &m.Enabled, &lastSeen, &m.Online, &m.CoinID)
		if err != nil {
			return nil, err
		}
		m.LastSeen = parseTimestamp(lastSeen)
		miners = append(miners, m)
	}

	return miners, rows.Err()
}

// RemoveMiner sets enabled=false for the given miner IP
func (s *SQLiteStorage) RemoveMiner(ip string) error {
	query := `UPDATE miners SET enabled = 0 WHERE ip = ?`
	_, err := s.db.Exec(query, ip)
	return err
}

// SetMinerCoin sets the coin override for a specific miner
func (s *SQLiteStorage) SetMinerCoin(ip string, coinID string) error {
	_, err := s.db.Exec("UPDATE miners SET coin_id = ? WHERE ip = ?", coinID, ip)
	return err
}

// InsertSnapshot inserts a new miner snapshot
func (s *SQLiteStorage) InsertSnapshot(snap *MinerSnapshot) error {
	query := `
	INSERT INTO miner_snapshots (
		miner_ip, timestamp, hostname, device_model,
		hash_rate, hash_rate_1m, hash_rate_10m, hash_rate_1h, hash_rate_1d,
		temperature, vr_temp, power, voltage,
		fan_rpm, fan_percent,
		shares_accepted, shares_rejected,
		best_diff, best_diff_session, pool_difficulty, pool_connected,
		uptime_seconds, wifi_rssi,
		found_blocks, total_found_blocks
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(query,
		snap.MinerIP, snap.Timestamp.UTC().Format("2006-01-02 15:04:05"), snap.Hostname, snap.DeviceModel,
		snap.HashRate, snap.HashRate1m, snap.HashRate10m, snap.HashRate1h, snap.HashRate1d,
		snap.Temperature, snap.VRTemp, snap.Power, snap.Voltage,
		snap.FanRPM, snap.FanPercent,
		snap.SharesAccept, snap.SharesReject,
		snap.BestDiff, snap.BestDiffSess, snap.PoolDiff, snap.PoolConnected,
		snap.UptimeSecs, snap.WifiRSSI,
		snap.FoundBlocks, snap.TotalFoundBlocks,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err == nil {
		snap.ID = id
	}
	return nil
}

// GetSnapshots retrieves snapshots for a miner since a given time
func (s *SQLiteStorage) GetSnapshots(minerIP string, since time.Time, limit int) ([]*MinerSnapshot, error) {
	query := `
	SELECT id, miner_ip, timestamp, hostname, device_model,
		hash_rate, hash_rate_1m, hash_rate_10m, hash_rate_1h, hash_rate_1d,
		temperature, vr_temp, power, voltage,
		fan_rpm, fan_percent,
		shares_accepted, shares_rejected,
		best_diff, best_diff_session, pool_difficulty, pool_connected,
		uptime_seconds, wifi_rssi,
		COALESCE(found_blocks, 0), COALESCE(total_found_blocks, 0)
	FROM miner_snapshots
	WHERE miner_ip = ? AND timestamp >= ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, minerIP, since.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*MinerSnapshot
	for rows.Next() {
		snap := &MinerSnapshot{}
		var timestamp string
		err := rows.Scan(
			&snap.ID, &snap.MinerIP, &timestamp, &snap.Hostname, &snap.DeviceModel,
			&snap.HashRate, &snap.HashRate1m, &snap.HashRate10m, &snap.HashRate1h, &snap.HashRate1d,
			&snap.Temperature, &snap.VRTemp, &snap.Power, &snap.Voltage,
			&snap.FanRPM, &snap.FanPercent,
			&snap.SharesAccept, &snap.SharesReject,
			&snap.BestDiff, &snap.BestDiffSess, &snap.PoolDiff, &snap.PoolConnected,
			&snap.UptimeSecs, &snap.WifiRSSI,
			&snap.FoundBlocks, &snap.TotalFoundBlocks,
		)
		if err != nil {
			return nil, err
		}
		snap.Timestamp = parseTimestamp(timestamp)
		snapshots = append(snapshots, snap)
	}

	return snapshots, rows.Err()
}

// InsertShare inserts a new share record
func (s *SQLiteStorage) InsertShare(share *Share) error {
	query := `
	INSERT INTO shares (miner_ip, hostname, timestamp, asic_num, difficulty, job_id)
	VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(query, share.MinerIP, share.Hostname, share.Timestamp.UTC().Format("2006-01-02 15:04:05"), share.AsicNum, share.Difficulty, share.JobID)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err == nil {
		share.ID = id
	}
	return nil
}

// GetShares retrieves shares since a given time
func (s *SQLiteStorage) GetShares(since time.Time, limit int) ([]*Share, error) {
	query := `
	SELECT id, miner_ip, hostname, timestamp, asic_num, difficulty, job_id
	FROM shares
	WHERE timestamp >= ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, since.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []*Share
	for rows.Next() {
		share := &Share{}
		var timestamp string
		err := rows.Scan(&share.ID, &share.MinerIP, &share.Hostname, &timestamp, &share.AsicNum, &share.Difficulty, &share.JobID)
		if err != nil {
			return nil, err
		}
		share.Timestamp = parseTimestamp(timestamp)
		shares = append(shares, share)
	}

	return shares, rows.Err()
}

// GetBestShare retrieves the best (highest difficulty) share for a miner
// If sessionOnly is true, only considers shares from the current session (last 24h)
func (s *SQLiteStorage) GetBestShare(minerIP string, sessionOnly bool) (*Share, error) {
	var query string
	var args []interface{}

	if sessionOnly {
		since := time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
		query = `
		SELECT id, miner_ip, hostname, timestamp, asic_num, difficulty, job_id
		FROM shares
		WHERE miner_ip = ? AND timestamp >= ?
		ORDER BY difficulty DESC
		LIMIT 1
		`
		args = []interface{}{minerIP, since}
	} else {
		query = `
		SELECT id, miner_ip, hostname, timestamp, asic_num, difficulty, job_id
		FROM shares
		WHERE miner_ip = ?
		ORDER BY difficulty DESC
		LIMIT 1
		`
		args = []interface{}{minerIP}
	}

	share := &Share{}
	var timestamp string
	err := s.db.QueryRow(query, args...).Scan(
		&share.ID, &share.MinerIP, &share.Hostname, &timestamp, &share.AsicNum, &share.Difficulty, &share.JobID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	share.Timestamp = parseTimestamp(timestamp)
	return share, nil
}

// InsertBlock inserts a new block record
func (s *SQLiteStorage) InsertBlock(block *Block) error {
	query := `
	INSERT INTO blocks (miner_ip, hostname, timestamp, difficulty, network_difficulty, coin_id, coin_symbol, block_reward, coin_price, value_usd)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(query,
		block.MinerIP,
		block.Hostname,
		block.Timestamp.UTC().Format("2006-01-02 15:04:05"),
		block.Difficulty,
		block.NetworkDifficulty,
		block.CoinID,
		block.CoinSymbol,
		block.BlockReward,
		block.CoinPrice,
		block.ValueUSD,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err == nil {
		block.ID = id
	}
	return nil
}

// GetBlocks retrieves blocks since a given time
func (s *SQLiteStorage) GetBlocks(since time.Time, limit int) ([]*Block, error) {
	query := `
	SELECT id, miner_ip, hostname, timestamp, difficulty, network_difficulty,
	       COALESCE(coin_id, ''), COALESCE(coin_symbol, ''), COALESCE(block_reward, 0),
	       COALESCE(coin_price, 0), COALESCE(value_usd, 0)
	FROM blocks
	WHERE timestamp >= ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, since.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []*Block
	for rows.Next() {
		block := &Block{}
		var timestamp string
		err := rows.Scan(&block.ID, &block.MinerIP, &block.Hostname, &timestamp,
			&block.Difficulty, &block.NetworkDifficulty,
			&block.CoinID, &block.CoinSymbol, &block.BlockReward,
			&block.CoinPrice, &block.ValueUSD)
		if err != nil {
			return nil, err
		}
		block.Timestamp = parseTimestamp(timestamp)
		blocks = append(blocks, block)
	}

	return blocks, rows.Err()
}

// GetBlockCount returns the total number of blocks found
func (s *SQLiteStorage) GetBlockCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM blocks").Scan(&count)
	return count, err
}

// MoneyMaker represents a miner's total earnings
type MoneyMaker struct {
	MinerIP     string  `json:"minerIp"`
	Hostname    string  `json:"hostname"`
	TotalUSD    float64 `json:"totalUsd"`
	BlockCount  int     `json:"blockCount"`
	WeeklyUSD   float64 `json:"weeklyUsd"`
	WeeklyBlocks int    `json:"weeklyBlocks"`
}

// GetMoneyMakers returns miners ranked by total USD earned
func (s *SQLiteStorage) GetMoneyMakers() ([]*MoneyMaker, error) {
	query := `
	SELECT
		miner_ip,
		MAX(hostname) as hostname,
		COALESCE(SUM(value_usd), 0) as total_usd,
		COUNT(*) as block_count
	FROM blocks
	GROUP BY miner_ip
	ORDER BY total_usd DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var makers []*MoneyMaker
	for rows.Next() {
		m := &MoneyMaker{}
		err := rows.Scan(&m.MinerIP, &m.Hostname, &m.TotalUSD, &m.BlockCount)
		if err != nil {
			return nil, err
		}
		makers = append(makers, m)
	}

	return makers, rows.Err()
}

// GetWeeklyEarnings returns earnings for a miner since a given time
func (s *SQLiteStorage) GetWeeklyEarnings(minerIP string, since time.Time) (float64, int, error) {
	query := `
	SELECT COALESCE(SUM(value_usd), 0), COUNT(*)
	FROM blocks
	WHERE miner_ip = ? AND timestamp >= ?
	`
	var totalUSD float64
	var blockCount int
	err := s.db.QueryRow(query, minerIP, since.UTC().Format("2006-01-02 15:04:05")).Scan(&totalUSD, &blockCount)
	return totalUSD, blockCount, err
}

// CoinHolding represents coins mined by a miner
type CoinHolding struct {
	MinerIP    string  `json:"minerIp"`
	CoinID     string  `json:"coinId"`
	CoinSymbol string  `json:"coinSymbol"`
	TotalCoins float64 `json:"totalCoins"`
	BlockCount int     `json:"blockCount"`
}

// CoinEarnings represents total earnings for a coin
type CoinEarnings struct {
	CoinID       string  `json:"coinId"`
	CoinSymbol   string  `json:"coinSymbol"`
	TotalCoins   float64 `json:"totalCoins"`
	BlockCount   int     `json:"blockCount"`
	HistoricalUSD float64 `json:"historicalUsd"` // Value when mined
}

// GetTotalEarnings returns total earnings grouped by coin
func (s *SQLiteStorage) GetTotalEarnings() ([]*CoinEarnings, error) {
	query := `
	SELECT
		coin_id,
		coin_symbol,
		COALESCE(SUM(block_reward), 0) as total_coins,
		COUNT(*) as block_count,
		COALESCE(SUM(value_usd), 0) as historical_usd
	FROM blocks
	WHERE coin_id != ''
	GROUP BY coin_id
	ORDER BY historical_usd DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var earnings []*CoinEarnings
	for rows.Next() {
		e := &CoinEarnings{}
		err := rows.Scan(&e.CoinID, &e.CoinSymbol, &e.TotalCoins, &e.BlockCount, &e.HistoricalUSD)
		if err != nil {
			return nil, err
		}
		earnings = append(earnings, e)
	}

	return earnings, rows.Err()
}

// GetEarningsForCoin returns earnings for a specific coin
func (s *SQLiteStorage) GetEarningsForCoin(coinID string) (*CoinEarnings, error) {
	query := `
	SELECT
		coin_id,
		coin_symbol,
		COALESCE(SUM(block_reward), 0) as total_coins,
		COUNT(*) as block_count,
		COALESCE(SUM(value_usd), 0) as historical_usd
	FROM blocks
	WHERE coin_id = ?
	GROUP BY coin_id
	`

	e := &CoinEarnings{}
	err := s.db.QueryRow(query, coinID).Scan(&e.CoinID, &e.CoinSymbol, &e.TotalCoins, &e.BlockCount, &e.HistoricalUSD)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return e, nil
}

// GetMinerCoinHoldings returns the breakdown of coins mined by each miner
func (s *SQLiteStorage) GetMinerCoinHoldings() ([]*CoinHolding, error) {
	query := `
	SELECT
		miner_ip,
		coin_id,
		coin_symbol,
		COALESCE(SUM(block_reward), 0) as total_coins,
		COUNT(*) as block_count
	FROM blocks
	WHERE coin_id != ''
	GROUP BY miner_ip, coin_id
	ORDER BY miner_ip, total_coins DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []*CoinHolding
	for rows.Next() {
		h := &CoinHolding{}
		err := rows.Scan(&h.MinerIP, &h.CoinID, &h.CoinSymbol, &h.TotalCoins, &h.BlockCount)
		if err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	return holdings, rows.Err()
}

// GetWeeklyCoinHoldings returns coin holdings for a miner since a given time
func (s *SQLiteStorage) GetWeeklyCoinHoldings(minerIP string, since time.Time) ([]*CoinHolding, error) {
	query := `
	SELECT
		miner_ip,
		coin_id,
		coin_symbol,
		COALESCE(SUM(block_reward), 0) as total_coins,
		COUNT(*) as block_count
	FROM blocks
	WHERE miner_ip = ? AND timestamp >= ? AND coin_id != ''
	GROUP BY miner_ip, coin_id
	`

	rows, err := s.db.Query(query, minerIP, since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []*CoinHolding
	for rows.Next() {
		h := &CoinHolding{}
		err := rows.Scan(&h.MinerIP, &h.CoinID, &h.CoinSymbol, &h.TotalCoins, &h.BlockCount)
		if err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	return holdings, rows.Err()
}

// GetBestShareInRange retrieves the best share for a miner within a time range
func (s *SQLiteStorage) GetBestShareInRange(minerIP string, start, end time.Time) (*Share, error) {
	query := `
	SELECT id, miner_ip, hostname, timestamp, asic_num, difficulty, job_id
	FROM shares
	WHERE miner_ip = ? AND timestamp >= ? AND timestamp <= ?
	ORDER BY difficulty DESC
	LIMIT 1
	`

	share := &Share{}
	var timestamp string
	err := s.db.QueryRow(query, minerIP, start.UTC().Format("2006-01-02 15:04:05"), end.UTC().Format("2006-01-02 15:04:05")).Scan(
		&share.ID, &share.MinerIP, &share.Hostname, &timestamp, &share.AsicNum, &share.Difficulty, &share.JobID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	share.Timestamp = parseTimestamp(timestamp)
	return share, nil
}

// GetShareCountInRange counts shares for a miner within a time range
func (s *SQLiteStorage) GetShareCountInRange(minerIP string, start, end time.Time) (int, error) {
	query := `
	SELECT COUNT(*) FROM shares
	WHERE miner_ip = ? AND timestamp >= ? AND timestamp <= ?
	`

	var count int
	err := s.db.QueryRow(query, minerIP, start.UTC().Format("2006-01-02 15:04:05"), end.UTC().Format("2006-01-02 15:04:05")).Scan(&count)
	return count, err
}

// GetBlockCountInRange counts blocks for a miner within a time range
func (s *SQLiteStorage) GetBlockCountInRange(minerIP string, start, end time.Time) (int, error) {
	query := `
	SELECT COUNT(*) FROM blocks
	WHERE miner_ip = ? AND timestamp >= ? AND timestamp <= ?
	`

	var count int
	err := s.db.QueryRow(query, minerIP, start.UTC().Format("2006-01-02 15:04:05"), end.UTC().Format("2006-01-02 15:04:05")).Scan(&count)
	return count, err
}

// GetBlockCountAllTime counts all blocks for a miner
func (s *SQLiteStorage) GetBlockCountAllTime(minerIP string) (int, error) {
	query := `SELECT COUNT(*) FROM blocks WHERE miner_ip = ?`
	var count int
	err := s.db.QueryRow(query, minerIP).Scan(&count)
	return count, err
}

// GetBlockStreak calculates consecutive weeks with at least 1 block for a miner
func (s *SQLiteStorage) GetBlockStreak(minerIP string) (int, error) {
	// Get all blocks for this miner ordered by timestamp
	query := `
	SELECT timestamp FROM blocks
	WHERE miner_ip = ?
	ORDER BY timestamp DESC
	`

	rows, err := s.db.Query(query, minerIP)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	// Collect all block timestamps
	var timestamps []time.Time
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			continue
		}
		timestamps = append(timestamps, parseTimestamp(ts))
	}

	if len(timestamps) == 0 {
		return 0, nil
	}

	// Calculate which weeks have blocks
	weeksWithBlocks := make(map[string]bool)
	for _, ts := range timestamps {
		// Get the Sunday of that week
		weekday := int(ts.Weekday())
		weekStart := time.Date(ts.Year(), ts.Month(), ts.Day()-weekday, 0, 0, 0, 0, ts.Location())
		weekKey := weekStart.Format("2006-01-02")
		weeksWithBlocks[weekKey] = true
	}

	// Calculate streak from current week backwards
	now := time.Now()
	weekday := int(now.Weekday())
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())

	streak := 0
	for {
		weekKey := currentWeekStart.Format("2006-01-02")
		if weeksWithBlocks[weekKey] {
			streak++
			currentWeekStart = currentWeekStart.AddDate(0, 0, -7) // Go to previous week
		} else {
			break
		}
	}

	return streak, nil
}

// PurgeOldData removes data older than the specified retention period
func (s *SQLiteStorage) PurgeOldData(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).UTC().Format("2006-01-02 15:04:05")

	// Delete old snapshots
	_, err := s.db.Exec("DELETE FROM miner_snapshots WHERE timestamp < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to purge old snapshots: %w", err)
	}

	// Delete old shares
	_, err = s.db.Exec("DELETE FROM shares WHERE timestamp < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to purge old shares: %w", err)
	}

	// Note: We don't delete blocks - they are rare and historically valuable

	// Run VACUUM to reclaim space
	_, err = s.db.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}

	return nil
}

// PurgeOldShares removes shares older than the specified number of hours
func (s *SQLiteStorage) PurgeOldShares(retentionHours int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour).UTC().Format("2006-01-02 15:04:05")

	result, err := s.db.Exec("DELETE FROM shares WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old shares: %w", err)
	}

	deleted, _ := result.RowsAffected()
	return deleted, nil
}

// PurgeOldSnapshots removes snapshots older than the specified number of hours
func (s *SQLiteStorage) PurgeOldSnapshots(retentionHours int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour).UTC().Format("2006-01-02 15:04:05")

	result, err := s.db.Exec("DELETE FROM miner_snapshots WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old snapshots: %w", err)
	}

	deleted, _ := result.RowsAffected()
	return deleted, nil
}

// Vacuum compacts the database file to reclaim disk space after deletions
func (s *SQLiteStorage) Vacuum() error {
	_, err := s.db.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}
	return nil
}
