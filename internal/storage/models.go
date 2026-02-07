package storage

import "time"

type MinerSnapshot struct {
	ID            int64     `json:"id"`
	MinerIP       string    `json:"minerIp"`
	Timestamp     time.Time `json:"timestamp"`
	Hostname      string    `json:"hostname"`
	DeviceModel   string    `json:"deviceModel"`
	HashRate      float64   `json:"hashRate"`      // GH/s - current
	HashRate1m    float64   `json:"hashRate1m"`    // 1 minute average
	HashRate10m   float64   `json:"hashRate10m"`   // 10 minute average
	HashRate1h    float64   `json:"hashRate1h"`    // 1 hour average
	HashRate1d    float64   `json:"hashRate1d"`    // 1 day average
	Temperature   float64   `json:"temperature"`   // Celsius
	VRTemp        float64   `json:"vrTemp"`
	Power         float64   `json:"power"`         // Watts
	Voltage       float64   `json:"voltage"`
	FanRPM        int       `json:"fanRpm"`
	FanPercent    int       `json:"fanPercent"`
	SharesAccept  int64     `json:"sharesAccepted"`
	SharesReject  int64     `json:"sharesRejected"`
	BestDiff      float64   `json:"bestDiff"`
	BestDiffSess  float64   `json:"bestDiffSession"`
	PoolDiff      float64   `json:"poolDifficulty"`
	PoolConnected    bool  `json:"poolConnected"`
	UptimeSecs       int64 `json:"uptimeSeconds"`
	WifiRSSI         int   `json:"wifiRssi"`
	FoundBlocks      int   `json:"foundBlocks"`
	TotalFoundBlocks int   `json:"totalFoundBlocks"`
}

type Share struct {
	ID         int64     `json:"id"`
	MinerIP    string    `json:"minerIp"`
	Hostname   string    `json:"hostname"`
	Timestamp  time.Time `json:"timestamp"`
	AsicNum    int       `json:"asicNum"`
	Difficulty float64   `json:"difficulty"`
	JobID      string    `json:"jobId"`
}

type Miner struct {
	IP          string    `json:"ip"`
	Hostname    string    `json:"hostname"`
	DeviceModel string    `json:"deviceModel"`
	ASICModel   string    `json:"asicModel"`
	Enabled     bool      `json:"enabled"`
	LastSeen    time.Time `json:"lastSeen"`
	Online      bool      `json:"online"`
	CoinID      string    `json:"coinId"` // Per-miner coin override ("", "btc", "dgb", etc)
}

// Block represents a found block event
type Block struct {
	ID                int64     `json:"id"`
	MinerIP           string    `json:"minerIp"`
	Hostname          string    `json:"hostname"`
	Timestamp         time.Time `json:"timestamp"`
	Difficulty        float64   `json:"difficulty"`
	NetworkDifficulty float64   `json:"networkDifficulty"`
	// Value tracking fields
	CoinID      string  `json:"coinId"`      // e.g., "dgb", "btc"
	CoinSymbol  string  `json:"coinSymbol"`  // e.g., "DGB", "BTC"
	BlockReward float64 `json:"blockReward"` // Coins earned (e.g., 274.28 DGB)
	CoinPrice   float64 `json:"coinPrice"`   // USD price at time of block
	ValueUSD    float64 `json:"valueUsd"`    // Total USD value (reward * price)
}
