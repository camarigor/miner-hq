package config

import (
	"encoding/json"
	"os"
	"time"
)

// MinerConfig defines a single miner device to monitor
type MinerConfig struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
	Location string `json:"location,omitempty"`
}

// AlertConfig defines alerting thresholds and settings
type AlertConfig struct {
	Enabled            bool    `json:"enabled"`
	HashrateDropPct    float64 `json:"hashrate_drop_pct"`    // Alert if hashrate drops by this percentage
	TempThresholdC     float64 `json:"temp_threshold_c"`     // Alert if temp exceeds this value
	OfflineMinutes     int     `json:"offline_minutes"`      // Alert if miner offline for this duration
	ShareRejectPct     float64 `json:"share_reject_pct"`     // Alert if rejection rate exceeds this
	FanRPMBelow        int     `json:"fan_rpm_below"`        // Alert if fan RPM drops below this
	WifiSignalBelow    int     `json:"wifi_signal_below"`    // Alert if WiFi signal drops below this (dBm)
	OnShareRejected    bool    `json:"on_share_rejected"`    // Alert on rejected shares
	OnPoolDisconnected bool    `json:"on_pool_disconnected"` // Alert on pool disconnect
	OnNewBestDiff      bool    `json:"on_new_best_diff"`     // Alert on new best difficulty
	OnBlockFound       bool    `json:"on_block_found"`       // Alert when a block is found
	OnNewLeader        bool    `json:"on_new_leader"`        // Alert when weekly leader changes
	WebhookURL         string  `json:"webhook_url,omitempty"`
	EmailEnabled       bool    `json:"email_enabled"`
	EmailSMTPServer    string  `json:"email_smtp_server,omitempty"`
	EmailSMTPPort      int     `json:"email_smtp_port,omitempty"`
	EmailFrom          string  `json:"email_from,omitempty"`
	EmailTo            string  `json:"email_to,omitempty"`
	EmailPassword      string  `json:"email_password,omitempty"`
}

// EnergyConfig defines energy cost settings for profitability calculations
type EnergyConfig struct {
	CostPerKWh float64 `json:"cost_per_kwh"` // Cost in local currency per kWh
	Currency   string  `json:"currency"`     // Currency code (USD, EUR, etc.)
}

// PricingConfig defines cryptocurrency price fetching settings
type PricingConfig struct {
	Enabled        bool          `json:"enabled"`
	UpdateInterval time.Duration `json:"update_interval"`
	FiatCurrency   string        `json:"fiat_currency"`
}

// RetentionConfig defines data retention policies
type RetentionConfig struct {
	MetricsRetentionDays  int `json:"metrics_retention_days"`  // How long to keep detailed metrics
	SharesRetentionDays   int `json:"shares_retention_days"`   // How long to keep share data
	AlertsRetentionDays   int `json:"alerts_retention_days"`   // How long to keep alert history
	AggregationIntervalH  int `json:"aggregation_interval_h"`  // Hours between aggregation runs
}

// ScannerConfig defines network scanner settings
type ScannerConfig struct {
	Enabled      bool          `json:"enabled"`
	Networks     []string      `json:"networks"`      // CIDR ranges (empty = auto-detect)
	ScanInterval time.Duration `json:"scan_interval"`
	AutoAdd      bool          `json:"auto_add"`      // Automatically add discovered miners
}

// ServerConfig defines HTTP server settings
type ServerConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
}

// DisplayConfig defines chart display preferences
type DisplayConfig struct {
	SharesMinDifficulty float64 `json:"shares_min_difficulty"` // Hide shares below this difficulty (0 = show all)
}

// Config is the main configuration structure
type Config struct {
	Server    ServerConfig    `json:"server"`
	Miners    []MinerConfig   `json:"miners"`
	Alerts    AlertConfig     `json:"alerts"`
	Energy    EnergyConfig    `json:"energy"`
	Pricing   PricingConfig   `json:"pricing"`
	Retention RetentionConfig `json:"retention"`
	Scanner   ScannerConfig   `json:"scanner"`
	Display   DisplayConfig   `json:"display"`
	DBPath    string          `json:"db_path"`
	LogLevel  string          `json:"log_level"`
}

// DefaultConfig returns a Config with sensible default values
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 120 * time.Second,
		},
		Miners: []MinerConfig{},
		Alerts: AlertConfig{
			Enabled:            true,
			HashrateDropPct:    20.0,
			TempThresholdC:     80.0,
			OfflineMinutes:     5,
			ShareRejectPct:     5.0,
			FanRPMBelow:        1000,
			WifiSignalBelow:    -70,
			OnShareRejected:    true,
			OnPoolDisconnected: true,
			OnNewBestDiff:      false,
			OnBlockFound:       true,
			OnNewLeader:        true,
			EmailSMTPPort:      587,
		},
		Energy: EnergyConfig{
			CostPerKWh: 0.12,
			Currency:   "USD",
		},
		Pricing: PricingConfig{
			Enabled:        true,
			UpdateInterval: 5 * time.Minute,
			FiatCurrency:   "USD",
		},
		Retention: RetentionConfig{
			MetricsRetentionDays: 30,
			SharesRetentionDays:  7,
			AlertsRetentionDays:  90,
			AggregationIntervalH: 1,
		},
		Scanner: ScannerConfig{
			Enabled:      false,
			Networks:     []string{}, // Auto-detect all networks
			ScanInterval: 5 * time.Minute,
			AutoAdd:      false,
		},
		DBPath:   "/data/minerhq.db",
		LogLevel: "info",
	}
}

// Load reads configuration from a JSON file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// Save writes configuration to a JSON file
func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
