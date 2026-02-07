package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/camarigor/miner-hq/internal/collector"
	"github.com/camarigor/miner-hq/internal/storage"
)

// AlertType represents the type of alert
type AlertType string

const (
	AlertMinerOffline     AlertType = "miner_offline"
	AlertTempHigh         AlertType = "temp_high"
	AlertHashrateDrop     AlertType = "hashrate_drop"
	AlertShareRejected    AlertType = "share_rejected"
	AlertPoolDisconnected AlertType = "pool_disconnected"
	AlertFanLow           AlertType = "fan_low"
	AlertWifiWeak         AlertType = "wifi_weak"
	AlertNewBestDiff      AlertType = "new_best_diff"
	AlertBlockFound       AlertType = "block_found"
	AlertNewLeader        AlertType = "new_leader"
)

// alertDisplay holds the visual representation for each alert type
type alertDisplay struct {
	Emoji string
	Title string
	Color int
}

// alertDisplayMap maps each AlertType to its display properties
var alertDisplayMap = map[AlertType]alertDisplay{
	AlertMinerOffline:     {Emoji: "🔴", Title: "Miner Offline", Color: 0xFF4444},
	AlertTempHigh:         {Emoji: "🌡️", Title: "High Temperature", Color: 0xFFAA00},
	AlertHashrateDrop:     {Emoji: "📉", Title: "Hashrate Drop", Color: 0xFFAA00},
	AlertShareRejected:    {Emoji: "❌", Title: "Share Rejected", Color: 0xFF6600},
	AlertPoolDisconnected: {Emoji: "🔌", Title: "Pool Disconnected", Color: 0xFF4444},
	AlertFanLow:           {Emoji: "💨", Title: "Low Fan Speed", Color: 0xFFAA00},
	AlertWifiWeak:         {Emoji: "📶", Title: "Weak WiFi Signal", Color: 0xFFAA00},
	AlertNewBestDiff:      {Emoji: "🏆", Title: "New Best Difficulty!", Color: 0x00FF88},
	AlertBlockFound:       {Emoji: "⛏️", Title: "Block Found!", Color: 0xFFD700},
	AlertNewLeader:        {Emoji: "👑", Title: "New Weekly Leader!", Color: 0xAA55FF},
}

// getAlertDisplay returns the display properties for an alert type
func getAlertDisplay(t AlertType) alertDisplay {
	if d, ok := alertDisplayMap[t]; ok {
		return d
	}
	return alertDisplay{Emoji: "⚠️", Title: string(t), Color: 0x00D4FF}
}

// AlertConfig holds alert configuration
type AlertConfig struct {
	WebhookURL          string  `json:"webhookUrl"`
	MinerOfflineSeconds int     `json:"minerOfflineSeconds"`
	TempAbove           float64 `json:"tempAbove"`
	HashrateDropPercent float64 `json:"hashrateDropPercent"`
	FanRPMBelow         int     `json:"fanRpmBelow"`
	WifiSignalBelow     int     `json:"wifiSignalBelow"`
	OnShareRejected     bool    `json:"onShareRejected"`
	OnPoolDisconnected  bool    `json:"onPoolDisconnected"`
	OnNewBestDiff       bool    `json:"onNewBestDiff"`
	OnBlockFound        bool    `json:"onBlockFound"`
	OnNewLeader         bool    `json:"onNewLeader"`
}

// Alert represents a triggered alert
type Alert struct {
	Type      AlertType              `json:"type"`
	MinerIP   string                 `json:"minerIp"`
	MinerName string                 `json:"minerName"`
	Message   string                 `json:"message"`
	Value     float64                `json:"value,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Fields    []map[string]interface{} `json:"fields,omitempty"`
}

// AlertEngine monitors miners and sends alerts
type AlertEngine struct {
	config        *AlertConfig
	client        *http.Client
	lastSeen      map[string]time.Time
	lastHashrate  map[string]float64
	lastBestDiff  map[string]float64
	alertCooldown map[string]time.Time // Prevent alert spam
	weeklyBestDiff float64
	weeklyLeader   string
	weekStart      time.Time
	mu            sync.RWMutex
}

// NewAlertEngine creates a new alert engine
func NewAlertEngine(config *AlertConfig) *AlertEngine {
	return &AlertEngine{
		config:        config,
		client:        &http.Client{Timeout: 10 * time.Second},
		lastSeen:      make(map[string]time.Time),
		lastHashrate:  make(map[string]float64),
		lastBestDiff:  make(map[string]float64),
		alertCooldown: make(map[string]time.Time),
		weekStart:     currentWeekStart(),
	}
}

// currentWeekStart returns the start of the current week (Sunday midnight)
func currentWeekStart() time.Time {
	now := time.Now()
	weekday := int(now.Weekday())
	return time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())
}

// InitWeeklyLeader seeds the in-memory weekly leader state so that a
// container restart doesn't trigger a false "new leader" alert.
func (e *AlertEngine) InitWeeklyLeader(leader string, bestDiff float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.weeklyLeader = leader
	e.weeklyBestDiff = bestDiff
	e.weekStart = currentWeekStart()
	if leader != "" {
		log.Printf("Weekly leader initialized: %s (diff: %.2f)", leader, bestDiff)
	}
}

// UpdateConfig updates the alert configuration
func (e *AlertEngine) UpdateConfig(config *AlertConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// CheckSnapshot evaluates a snapshot and triggers alerts if needed
func (e *AlertEngine) CheckSnapshot(snap *storage.MinerSnapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()

	minerKey := snap.MinerIP

	// Update last seen
	e.lastSeen[minerKey] = time.Now()

	// Check temperature
	if e.config.TempAbove > 0 && snap.Temperature > e.config.TempAbove {
		e.sendAlert(Alert{
			Type:      AlertTempHigh,
			MinerIP:   snap.MinerIP,
			MinerName: snap.Hostname,
			Message:   fmt.Sprintf("Temperature is %.1f°C (threshold: %.1f°C)", snap.Temperature, e.config.TempAbove),
			Value:     snap.Temperature,
			Timestamp: time.Now(),
		})
	}

	// Check hashrate drop
	if lastHash, ok := e.lastHashrate[minerKey]; ok && lastHash > 0 {
		dropPercent := ((lastHash - snap.HashRate) / lastHash) * 100
		if e.config.HashrateDropPercent > 0 && dropPercent > e.config.HashrateDropPercent {
			e.sendAlert(Alert{
				Type:      AlertHashrateDrop,
				MinerIP:   snap.MinerIP,
				MinerName: snap.Hostname,
				Message:   fmt.Sprintf("Hashrate dropped %.1f%% (%.2f GH/s -> %.2f GH/s)", dropPercent, lastHash, snap.HashRate),
				Value:     dropPercent,
				Timestamp: time.Now(),
			})
		}
	}
	e.lastHashrate[minerKey] = snap.HashRate

	// Check fan RPM
	if e.config.FanRPMBelow > 0 && snap.FanRPM < e.config.FanRPMBelow && snap.FanRPM > 0 {
		e.sendAlert(Alert{
			Type:      AlertFanLow,
			MinerIP:   snap.MinerIP,
			MinerName: snap.Hostname,
			Message:   fmt.Sprintf("Fan RPM is %d (threshold: %d)", snap.FanRPM, e.config.FanRPMBelow),
			Value:     float64(snap.FanRPM),
			Timestamp: time.Now(),
		})
	}

	// Check WiFi signal
	if e.config.WifiSignalBelow < 0 && snap.WifiRSSI < e.config.WifiSignalBelow {
		e.sendAlert(Alert{
			Type:      AlertWifiWeak,
			MinerIP:   snap.MinerIP,
			MinerName: snap.Hostname,
			Message:   fmt.Sprintf("WiFi signal is %d dBm (threshold: %d dBm)", snap.WifiRSSI, e.config.WifiSignalBelow),
			Value:     float64(snap.WifiRSSI),
			Timestamp: time.Now(),
		})
	}

	// Check pool connection
	if e.config.OnPoolDisconnected && !snap.PoolConnected {
		e.sendAlert(Alert{
			Type:      AlertPoolDisconnected,
			MinerIP:   snap.MinerIP,
			MinerName: snap.Hostname,
			Message:   "Pool disconnected",
			Timestamp: time.Now(),
		})
	}

	// Check new best difficulty
	if e.config.OnNewBestDiff {
		if lastBest, ok := e.lastBestDiff[minerKey]; ok && snap.BestDiffSess > lastBest {
			e.sendAlert(Alert{
				Type:      AlertNewBestDiff,
				MinerIP:   snap.MinerIP,
				MinerName: snap.Hostname,
				Message:   fmt.Sprintf("New best difficulty: %s", collector.FormatDifficulty(snap.BestDiffSess)),
				Value:     snap.BestDiffSess,
				Timestamp: time.Now(),
			})
		}
	}
	e.lastBestDiff[minerKey] = snap.BestDiffSess
}

// CheckShare evaluates a share for rejected status
func (e *AlertEngine) CheckShare(share *storage.Share, rejected bool) {
	if !e.config.OnShareRejected || !rejected {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.sendAlert(Alert{
		Type:      AlertShareRejected,
		MinerIP:   share.MinerIP,
		MinerName: share.Hostname,
		Message:   fmt.Sprintf("Share rejected (diff: %s)", collector.FormatDifficulty(share.Difficulty)),
		Value:     share.Difficulty,
		Timestamp: time.Now(),
	})
}

// CheckBlock sends an alert when a block is found. No cooldown — blocks are rare events.
func (e *AlertEngine) CheckBlock(block *storage.Block) {
	e.mu.RLock()
	webhookURL := e.config.WebhookURL
	enabled := e.config.OnBlockFound
	e.mu.RUnlock()

	if !enabled {
		return
	}

	if webhookURL == "" {
		log.Printf("Alert [block_found] %s: Block found on %s!", block.Hostname, block.CoinSymbol)
		return
	}

	valueStr := fmt.Sprintf("$%.2f", block.ValueUSD)
	if block.ValueUSD == 0 {
		valueStr = "N/A"
	}

	alert := Alert{
		Type:      AlertBlockFound,
		MinerIP:   block.MinerIP,
		MinerName: block.Hostname,
		Message:   fmt.Sprintf("Block found mining %s!", block.CoinSymbol),
		Timestamp: block.Timestamp,
		Fields: []map[string]interface{}{
			{"name": "Miner", "value": block.Hostname, "inline": true},
			{"name": "Coin", "value": block.CoinSymbol, "inline": true},
			{"name": "Reward", "value": fmt.Sprintf("%.4f %s", block.BlockReward, block.CoinSymbol), "inline": true},
			{"name": "Value", "value": valueStr, "inline": true},
			{"name": "Difficulty", "value": collector.FormatDifficulty(block.Difficulty), "inline": true},
		},
	}

	body, err := buildDiscordPayload(alert)
	if err != nil {
		log.Printf("Failed to marshal Discord payload: %v", err)
		return
	}

	go e.postWebhook(webhookURL, body)
}

// CheckLeaderChange checks if a share makes a new weekly leader in the best-share competition.
func (e *AlertEngine) CheckLeaderChange(share *storage.Share) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.config.OnNewLeader {
		return
	}

	// Reset if the week has changed
	ws := currentWeekStart()
	if ws.After(e.weekStart) {
		e.weeklyBestDiff = 0
		e.weeklyLeader = ""
		e.weekStart = ws
	}

	if share.Difficulty <= e.weeklyBestDiff {
		return
	}

	previousLeader := e.weeklyLeader
	e.weeklyBestDiff = share.Difficulty
	e.weeklyLeader = share.Hostname

	// Only alert when a *different* miner takes the lead (and there was a previous leader)
	if previousLeader == "" || previousLeader == share.Hostname {
		return
	}

	if e.config.WebhookURL == "" {
		log.Printf("Alert [new_leader] %s took the lead from %s (diff: %.2f)", share.Hostname, previousLeader, share.Difficulty)
		return
	}

	alert := Alert{
		Type:      AlertNewLeader,
		MinerIP:   share.MinerIP,
		MinerName: share.Hostname,
		Message:   fmt.Sprintf("%s is the new weekly leader!", share.Hostname),
		Timestamp: share.Timestamp,
		Fields: []map[string]interface{}{
			{"name": "New Leader", "value": share.Hostname, "inline": true},
			{"name": "Share Difficulty", "value": collector.FormatDifficulty(share.Difficulty), "inline": true},
			{"name": "Previous Leader", "value": previousLeader, "inline": true},
		},
	}

	body, err := buildDiscordPayload(alert)
	if err != nil {
		log.Printf("Failed to marshal Discord payload: %v", err)
		return
	}

	go e.postWebhook(e.config.WebhookURL, body)
}

// CheckOffline checks for miners that haven't been seen recently
func (e *AlertEngine) CheckOffline(miners []*storage.Miner) {
	if e.config.MinerOfflineSeconds <= 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	threshold := time.Duration(e.config.MinerOfflineSeconds) * time.Second

	for _, miner := range miners {
		if !miner.Enabled {
			continue
		}

		lastSeen, ok := e.lastSeen[miner.IP]
		if !ok {
			continue
		}

		if time.Since(lastSeen) > threshold {
			e.sendAlert(Alert{
				Type:      AlertMinerOffline,
				MinerIP:   miner.IP,
				MinerName: miner.Hostname,
				Message:   fmt.Sprintf("Miner offline for %v", time.Since(lastSeen).Round(time.Second)),
				Timestamp: time.Now(),
			})
		}
	}
}

// SendTestAlert sends a test message to the configured Discord webhook.
// It bypasses cooldown and runs synchronously so the caller gets immediate feedback.
func (e *AlertEngine) SendTestAlert() error {
	e.mu.RLock()
	webhookURL := e.config.WebhookURL
	e.mu.RUnlock()

	if webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       "✅ Test Alert",
				"description": "This is a test alert from MinerHQ. If you see this message, your Discord webhook is configured correctly!",
				"color":       0x00FF88,
				"timestamp":   time.Now().Format(time.RFC3339),
				"footer": map[string]string{
					"text": "MinerHQ Alert System — Test",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// validAlertTypes is the set of all supported alert types for test alerts
var validAlertTypes = map[AlertType]bool{
	AlertMinerOffline:     true,
	AlertTempHigh:         true,
	AlertHashrateDrop:     true,
	AlertShareRejected:    true,
	AlertPoolDisconnected: true,
	AlertFanLow:           true,
	AlertWifiWeak:         true,
	AlertNewBestDiff:      true,
	AlertBlockFound:       true,
	AlertNewLeader:        true,
}

// SendTestAlertByType sends a sample alert for the given type.
// Bypasses cooldown and runs synchronously.
func (e *AlertEngine) SendTestAlertByType(alertType string) error {
	e.mu.RLock()
	webhookURL := e.config.WebhookURL
	e.mu.RUnlock()

	if webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}

	at := AlertType(alertType)
	if !validAlertTypes[at] {
		return fmt.Errorf("invalid alert type: %s", alertType)
	}

	alert := buildSampleAlert(at)

	body, err := buildDiscordPayload(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// buildSampleAlert creates a realistic sample alert for testing
func buildSampleAlert(t AlertType) Alert {
	base := Alert{
		Type:      t,
		MinerIP:   "192.168.1.42",
		MinerName: "BitAxe-Ultra",
		Timestamp: time.Now(),
	}

	switch t {
	case AlertMinerOffline:
		base.Message = "Miner offline for 5m30s"
	case AlertTempHigh:
		base.Message = "Temperature is 72.5°C (threshold: 65.0°C)"
		base.Value = 72.5
	case AlertHashrateDrop:
		base.Message = "Hashrate dropped 45.2% (580.00 GH/s -> 318.00 GH/s)"
		base.Value = 45.2
	case AlertShareRejected:
		base.Message = "Share rejected (diff: 1024.50)"
		base.Value = 1024.50
	case AlertPoolDisconnected:
		base.Message = "Pool disconnected"
	case AlertFanLow:
		base.Message = "Fan RPM is 1200 (threshold: 2000)"
		base.Value = 1200
	case AlertWifiWeak:
		base.Message = "WiFi signal is -78 dBm (threshold: -70 dBm)"
		base.Value = -78
	case AlertNewBestDiff:
		base.Message = "New best difficulty: 4.29B"
		base.Value = 4290000000
	case AlertBlockFound:
		base.Message = "Block found mining DGB!"
		base.Fields = []map[string]interface{}{
			{"name": "Miner", "value": "BitAxe-Ultra", "inline": true},
			{"name": "Coin", "value": "DGB", "inline": true},
			{"name": "Reward", "value": "274.2800 DGB", "inline": true},
			{"name": "Value", "value": "$2.74", "inline": true},
			{"name": "Difficulty", "value": "8.59G", "inline": true},
		}
	case AlertNewLeader:
		base.Message = "BitAxe-Ultra is the new weekly leader!"
		base.Fields = []map[string]interface{}{
			{"name": "New Leader", "value": "BitAxe-Ultra", "inline": true},
			{"name": "Share Difficulty", "value": "4.29G", "inline": true},
			{"name": "Previous Leader", "value": "BitAxe-Supra", "inline": true},
		}
	}

	return base
}

// buildDiscordPayload builds the JSON body for a Discord webhook embed.
func buildDiscordPayload(alert Alert) ([]byte, error) {
	d := getAlertDisplay(alert.Type)

	// Use custom fields if provided, otherwise default Miner + IP fields
	fields := alert.Fields
	if fields == nil {
		fields = []map[string]interface{}{
			{"name": "Miner", "value": alert.MinerName, "inline": true},
			{"name": "IP", "value": alert.MinerIP, "inline": true},
		}
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("%s %s", d.Emoji, d.Title),
				"description": alert.Message,
				"color":       d.Color,
				"fields":      fields,
				"timestamp":   alert.Timestamp.Format(time.RFC3339),
				"footer": map[string]string{
					"text": "MinerHQ Alert System",
				},
			},
		},
	}

	return json.Marshal(payload)
}

// sendAlert sends an alert via Discord webhook (with cooldown)
func (e *AlertEngine) sendAlert(alert Alert) {
	// Check cooldown (5 minute cooldown per alert type per miner)
	cooldownKey := fmt.Sprintf("%s:%s", alert.MinerIP, alert.Type)
	if lastAlert, ok := e.alertCooldown[cooldownKey]; ok {
		if time.Since(lastAlert) < 5*time.Minute {
			return
		}
	}
	e.alertCooldown[cooldownKey] = time.Now()

	if e.config.WebhookURL == "" {
		log.Printf("Alert [%s] %s: %s", alert.Type, alert.MinerName, alert.Message)
		return
	}

	body, err := buildDiscordPayload(alert)
	if err != nil {
		log.Printf("Failed to marshal Discord payload: %v", err)
		return
	}

	go e.postWebhook(e.config.WebhookURL, body)
}

// postWebhook posts a payload to the given webhook URL
func (e *AlertEngine) postWebhook(url string, body []byte) {
	resp, err := e.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send Discord webhook: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("Discord webhook returned status %d", resp.StatusCode)
	}
}
