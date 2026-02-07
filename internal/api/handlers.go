package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/camarigor/miner-hq/internal/alerts"
	"github.com/camarigor/miner-hq/internal/pricing"
	"github.com/camarigor/miner-hq/internal/storage"
)

// MinerWithSnapshot combines miner info with latest snapshot
type MinerWithSnapshot struct {
	IP          string                 `json:"ip"`
	Hostname    string                 `json:"hostname"`
	DeviceModel string                 `json:"deviceModel"`
	ASICModel   string                 `json:"asicModel"`
	Enabled     bool                   `json:"enabled"`
	Online      bool                   `json:"online"`
	CoinID      string                 `json:"coinId"`
	Snapshot    *storage.MinerSnapshot `json:"snapshot,omitempty"`
}

// handleGetMiners returns all miners with online status and latest snapshot
// GET /api/miners
func (s *Server) handleGetMiners(w http.ResponseWriter, r *http.Request) {
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get current online status from collector
	status := s.collector.GetMinerStatus()

	// Build response with snapshots
	result := make([]MinerWithSnapshot, 0, len(miners))
	for _, m := range miners {
		mws := MinerWithSnapshot{
			IP:          m.IP,
			Hostname:    m.Hostname,
			DeviceModel: m.DeviceModel,
			ASICModel:   m.ASICModel,
			Enabled:     m.Enabled,
			Online:      false,
			CoinID:      m.CoinID,
		}

		if online, ok := status[m.IP]; ok {
			mws.Online = online
		}

		// Get latest snapshot for this miner
		snapshots, err := s.storage.GetSnapshots(m.IP, time.Now().Add(-5*time.Minute), 1)
		if err == nil && len(snapshots) > 0 {
			mws.Snapshot = snapshots[0]
		}

		result = append(result, mws)
	}

	s.jsonResponse(w, result)
}

// handleGetMiner returns a single miner by IP
// GET /api/miners/{ip}
func (s *Server) handleGetMiner(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")

	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, m := range miners {
		if m.IP == ip {
			// Get current online status
			status := s.collector.GetMinerStatus()
			if online, ok := status[m.IP]; ok {
				m.Online = online
			}
			s.jsonResponse(w, m)
			return
		}
	}

	http.Error(w, "miner not found", http.StatusNotFound)
}

// handleGetMinerHistory returns miner snapshots history
// GET /api/miners/{ip}/history
// Query params: hours (default 24), limit (default 1000)
func (s *Server) handleGetMinerHistory(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")

	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	limit := 1000
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	snapshots, err := s.storage.GetSnapshots(ip, since, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, snapshots)
}

// handleRemoveMiner removes a miner by IP
// DELETE /api/miners/{ip}
func (s *Server) handleRemoveMiner(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")

	// Stop collecting from this miner
	s.collector.RemoveMiner(ip)

	// Mark as disabled in storage
	if err := s.storage.RemoveMiner(ip); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]bool{"success": true})
}

// handleSetMinerCoin sets the coin for a specific miner
// PUT /api/miners/{ip}/coin
func (s *Server) handleSetMinerCoin(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")

	var req struct {
		Coin string `json:"coin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Allow empty string to reset to global default
	if req.Coin != "" {
		valid := false
		for _, c := range pricing.GetSupportedCoins() {
			if c.ID == req.Coin {
				valid = true
				break
			}
		}
		if !valid {
			http.Error(w, "invalid coin", http.StatusBadRequest)
			return
		}
	}

	if err := s.storage.SetMinerCoin(ip, req.Coin); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"status": "ok",
		"ip":     ip,
		"coin":   req.Coin,
	})
}

// FleetStats represents aggregate fleet statistics
type FleetStats struct {
	TotalHashrate   float64 `json:"totalHashrate"`   // GH/s
	TotalPower      float64 `json:"totalPower"`      // Watts
	Efficiency      float64 `json:"efficiency"`      // J/TH
	OnlineMiners    int     `json:"onlineMiners"`
	TotalMiners     int     `json:"totalMiners"`
	EnergyCostPerDay float64 `json:"energyCostPerDay"` // Currency per day
}

// handleGetStats returns fleet aggregate stats
// GET /api/stats
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status := s.collector.GetMinerStatus()

	var stats FleetStats
	stats.TotalMiners = len(miners)

	// Get latest snapshot for each miner to calculate totals
	for _, m := range miners {
		if online, ok := status[m.IP]; ok && online {
			stats.OnlineMiners++

			// Get latest snapshot for this miner
			snapshots, err := s.storage.GetSnapshots(m.IP, time.Now().Add(-5*time.Minute), 1)
			if err == nil && len(snapshots) > 0 {
				snap := snapshots[0]
				stats.TotalHashrate += snap.HashRate
				stats.TotalPower += snap.Power
			}
		}
	}

	// Calculate efficiency (J/TH)
	// Power is in Watts, HashRate is in GH/s
	// J/TH = Watts / (GH/s / 1000) = Watts * 1000 / GH/s
	if stats.TotalHashrate > 0 {
		stats.Efficiency = (stats.TotalPower * 1000) / stats.TotalHashrate
	}

	// Calculate energy cost per day
	// (totalPower / 1000) * 24 * costPerKwh
	stats.EnergyCostPerDay = (stats.TotalPower / 1000) * 24 * s.cfg.Energy.CostPerKWh

	s.jsonResponse(w, stats)
}

// handleGetShares returns recent shares
// GET /api/shares
// Query params: hours (default 24), limit (default 100)
func (s *Server) handleGetShares(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	shares, err := s.storage.GetShares(since, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, shares)
}

// handleGetBlocks returns found blocks
// GET /api/blocks
// Query params: days (default 365), limit (default 100)
func (s *Server) handleGetBlocks(w http.ResponseWriter, r *http.Request) {
	days := 365
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	since := time.Now().AddDate(0, 0, -days)
	blocks, err := s.storage.GetBlocks(since, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, blocks)
}

// handleGetBlockCount returns the total count of found blocks
// GET /api/blocks/count
// Returns only blocks we've captured via WebSocket (reliable data)
func (s *Server) handleGetBlockCount(w http.ResponseWriter, r *http.Request) {
	// Get count from our database (blocks we've captured via WebSocket)
	dbCount, _ := s.storage.GetBlockCount()

	s.jsonResponse(w, map[string]int64{
		"count": dbCount,
	})
}

// WeeklyCompetitor represents a miner in the weekly competition
type WeeklyCompetitor struct {
	MinerIP            string  `json:"minerIp"`
	Hostname           string  `json:"hostname"`
	BestDiff           float64 `json:"bestDiff"`
	ShareCount         int     `json:"shareCount"`
	Rank               int     `json:"rank"`
	PercentOfTop       float64 `json:"percentOfTop"`       // Percentage relative to leader
	PersonalBest       float64 `json:"personalBest"`       // All-time best
	IsNewRecord        bool    `json:"isNewRecord"`        // Beat personal best this week
	WeeksInTop3        int     `json:"weeksInTop3"`        // Streak counter
	RankChange         int     `json:"rankChange"`         // +1 moved up, -1 moved down, 0 same
	FoundBlockThisWeek bool    `json:"foundBlockThisWeek"` // Miner Legend status
	BlocksThisWeek     int     `json:"blocksThisWeek"`     // Number of blocks found this week
}

// WeeklyCompetition represents the weekly competition state
type WeeklyCompetition struct {
	Competitors      []WeeklyCompetitor      `json:"competitors"`
	BlockCompetitors []WeeklyBlockCompetitor `json:"blockCompetitors"`
	WeekStart        time.Time               `json:"weekStart"`
	WeekEnd          time.Time               `json:"weekEnd"`
	TimeRemaining    string                  `json:"timeRemaining"`
	SecondsLeft      int64                   `json:"secondsLeft"`
}

// WeeklyBlockCompetitor represents a miner in the weekly block competition
type WeeklyBlockCompetitor struct {
	MinerIP         string `json:"minerIp"`
	Hostname        string `json:"hostname"`
	BlocksThisWeek  int    `json:"blocksThisWeek"`
	BlocksAllTime   int    `json:"blocksAllTime"`
	Title           string `json:"title"`
	TitleIcon       string `json:"titleIcon"`
	Streak          int    `json:"streak"` // Consecutive weeks with at least 1 block
	Rank            int    `json:"rank"`
}

// getBlockTitle returns the title and icon based on weekly block count
func getBlockTitle(blocksThisWeek int) (string, string) {
	switch {
	case blocksThisWeek >= 8:
		return "Block God", "🌟"
	case blocksThisWeek >= 6:
		return "Block King", "👑"
	case blocksThisWeek >= 4:
		return "Block Champion", "🏆"
	case blocksThisWeek >= 3:
		return "Block Master", "💎"
	case blocksThisWeek >= 2:
		return "Block Hunter", "⛏️"
	case blocksThisWeek >= 1:
		return "Block Finder", "🔨"
	default:
		return "", ""
	}
}

// handleGetWeeklyCompetition returns the weekly best share competition
// GET /api/competition/weekly
func (s *Server) handleGetWeeklyCompetition(w http.ResponseWriter, r *http.Request) {
	// Calculate week boundaries (Sunday to Saturday, resets Sunday at midnight)
	now := time.Now()
	weekday := int(now.Weekday()) // Sunday = 0, Monday = 1, ..., Saturday = 6
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Get all miners
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// For each miner, get their best share this week and all-time
	var competitors []WeeklyCompetitor
	for _, m := range miners {
		// Get best share this week
		weeklyBest, _ := s.storage.GetBestShareInRange(m.IP, weekStart, now)

		// Get all-time best
		allTimeBest, _ := s.storage.GetBestShare(m.IP, false)

		// Get share count this week
		shareCount, _ := s.storage.GetShareCountInRange(m.IP, weekStart, now)

		var bestDiff, personalBest float64
		if weeklyBest != nil {
			bestDiff = weeklyBest.Difficulty
		}
		if allTimeBest != nil {
			personalBest = allTimeBest.Difficulty
		}

		// Get blocks found this week
		blocksThisWeek, _ := s.storage.GetBlockCountInRange(m.IP, weekStart, now)

		// Only include miners with shares this week
		if bestDiff > 0 {
			competitors = append(competitors, WeeklyCompetitor{
				MinerIP:            m.IP,
				Hostname:           m.Hostname,
				BestDiff:           bestDiff,
				ShareCount:         shareCount,
				PersonalBest:       personalBest,
				IsNewRecord:        bestDiff > personalBest && personalBest > 0, // Strictly greater = new record
				FoundBlockThisWeek: blocksThisWeek > 0,
				BlocksThisWeek:     blocksThisWeek,
			})
		}
	}

	// Sort by best difficulty (descending)
	for i := 0; i < len(competitors)-1; i++ {
		for j := i + 1; j < len(competitors); j++ {
			if competitors[j].BestDiff > competitors[i].BestDiff {
				competitors[i], competitors[j] = competitors[j], competitors[i]
			}
		}
	}

	// Calculate ranks and percentages
	var topDiff float64
	if len(competitors) > 0 {
		topDiff = competitors[0].BestDiff
	}
	for i := range competitors {
		competitors[i].Rank = i + 1
		if topDiff > 0 {
			competitors[i].PercentOfTop = (competitors[i].BestDiff / topDiff) * 100
		}
	}

	// Calculate time remaining
	secondsLeft := int64(weekEnd.Sub(now).Seconds())
	days := secondsLeft / 86400
	hours := (secondsLeft % 86400) / 3600
	minutes := (secondsLeft % 3600) / 60

	var timeRemaining string
	if days > 0 {
		timeRemaining = fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		timeRemaining = fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		timeRemaining = fmt.Sprintf("%dm", minutes)
	}

	// Build block competition data
	var blockCompetitors []WeeklyBlockCompetitor
	for _, m := range miners {
		blocksThisWeek, _ := s.storage.GetBlockCountInRange(m.IP, weekStart, now)
		blocksAllTime, _ := s.storage.GetBlockCountAllTime(m.IP)
		streak, _ := s.storage.GetBlockStreak(m.IP)

		// Only include miners with at least 1 block ever
		if blocksAllTime > 0 {
			title, titleIcon := getBlockTitle(blocksAllTime) // Use all-time for permanent titles
			blockCompetitors = append(blockCompetitors, WeeklyBlockCompetitor{
				MinerIP:        m.IP,
				Hostname:       m.Hostname,
				BlocksThisWeek: blocksThisWeek,
				BlocksAllTime:  blocksAllTime,
				Title:          title,
				TitleIcon:      titleIcon,
				Streak:         streak,
			})
		}
	}

	// Sort block competitors by blocks this week (descending), then all-time (descending)
	for i := 0; i < len(blockCompetitors)-1; i++ {
		for j := i + 1; j < len(blockCompetitors); j++ {
			if blockCompetitors[j].BlocksThisWeek > blockCompetitors[i].BlocksThisWeek ||
				(blockCompetitors[j].BlocksThisWeek == blockCompetitors[i].BlocksThisWeek &&
					blockCompetitors[j].BlocksAllTime > blockCompetitors[i].BlocksAllTime) {
				blockCompetitors[i], blockCompetitors[j] = blockCompetitors[j], blockCompetitors[i]
			}
		}
	}

	// Assign ranks to block competitors
	for i := range blockCompetitors {
		blockCompetitors[i].Rank = i + 1
	}

	s.jsonResponse(w, WeeklyCompetition{
		Competitors:      competitors,
		BlockCompetitors: blockCompetitors,
		WeekStart:        weekStart,
		WeekEnd:          weekEnd,
		TimeRemaining:    timeRemaining,
		SecondsLeft:      secondsLeft,
	})
}

// MoneyMakerCompetitor represents a miner in the money makers competition
type MoneyMakerCompetitor struct {
	MinerIP          string  `json:"minerIp"`
	Hostname         string  `json:"hostname"`
	TotalUSD         float64 `json:"totalUsd"`         // Historical value (when mined)
	CurrentUSD       float64 `json:"currentUsd"`       // Current value (today's prices)
	BlockCount       int     `json:"blockCount"`
	WeeklyUSD        float64 `json:"weeklyUsd"`        // Historical weekly value
	WeeklyCurrentUSD float64 `json:"weeklyCurrentUsd"` // Current weekly value
	WeeklyBlocks     int     `json:"weeklyBlocks"`
	Title            string  `json:"title"`
	TitleIcon        string  `json:"titleIcon"`
	Rank             int     `json:"rank"`
}

// MoneyMakersResponse represents the money makers leaderboard
type MoneyMakersResponse struct {
	Competitors []MoneyMakerCompetitor `json:"competitors"`
	WeekStart   time.Time              `json:"weekStart"`
	WeekEnd     time.Time              `json:"weekEnd"`
}

// getMoneyTitle returns the title and icon based on total USD earned
func getMoneyTitle(totalUSD float64) (string, string) {
	switch {
	case totalUSD >= 10000:
		return "Crypto Mogul", "💎"
	case totalUSD >= 5000:
		return "Mining Tycoon", "🏆"
	case totalUSD >= 1000:
		return "Profit King", "👑"
	case totalUSD >= 500:
		return "Cash Master", "💰"
	case totalUSD >= 100:
		return "Money Maker", "💵"
	case totalUSD >= 10:
		return "Coin Collector", "🪙"
	case totalUSD > 0:
		return "First Dollar", "💲"
	default:
		return "", ""
	}
}

// handleGetMoneyMakers returns the money makers leaderboard
// GET /api/competition/moneymakers
func (s *Server) handleGetMoneyMakers(w http.ResponseWriter, r *http.Request) {
	// Calculate week boundaries
	now := time.Now()
	weekday := int(now.Weekday())
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Get all money makers (historical values)
	makers, err := s.storage.GetMoneyMakers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all coin holdings to calculate current values
	allHoldings, err := s.storage.GetMinerCoinHoldings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Group holdings by miner
	holdingsByMiner := make(map[string][]*storage.CoinHolding)
	for _, h := range allHoldings {
		holdingsByMiner[h.MinerIP] = append(holdingsByMiner[h.MinerIP], h)
	}

	// Calculate current values using current prices
	currentValueByMiner := make(map[string]float64)
	for minerIP, holdings := range holdingsByMiner {
		var currentTotal float64
		for _, h := range holdings {
			currentPrice := s.pricing.GetPriceForCoin(h.CoinID)
			currentTotal += h.TotalCoins * currentPrice
		}
		currentValueByMiner[minerIP] = currentTotal
	}

	var competitors []MoneyMakerCompetitor
	for i, m := range makers {
		// Get weekly earnings (historical)
		weeklyUSD, weeklyBlocks, _ := s.storage.GetWeeklyEarnings(m.MinerIP, weekStart)

		// Get weekly coin holdings for current value calculation
		weeklyHoldings, _ := s.storage.GetWeeklyCoinHoldings(m.MinerIP, weekStart)
		var weeklyCurrentUSD float64
		for _, h := range weeklyHoldings {
			currentPrice := s.pricing.GetPriceForCoin(h.CoinID)
			weeklyCurrentUSD += h.TotalCoins * currentPrice
		}

		title, titleIcon := getMoneyTitle(m.TotalUSD)
		competitors = append(competitors, MoneyMakerCompetitor{
			MinerIP:          m.MinerIP,
			Hostname:         m.Hostname,
			TotalUSD:         m.TotalUSD,
			CurrentUSD:       currentValueByMiner[m.MinerIP],
			BlockCount:       m.BlockCount,
			WeeklyUSD:        weeklyUSD,
			WeeklyCurrentUSD: weeklyCurrentUSD,
			WeeklyBlocks:     weeklyBlocks,
			Title:            title,
			TitleIcon:        titleIcon,
			Rank:             i + 1,
		})
	}

	s.jsonResponse(w, MoneyMakersResponse{
		Competitors: competitors,
		WeekStart:   weekStart,
		WeekEnd:     weekEnd,
	})
}

// handleGetSettings returns the current configuration
// GET /api/settings
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, s.cfg)
}

// handleSaveSettings saves the configuration
// POST /api/settings
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, s.cfg); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Save to file
	if err := s.cfg.Save("/data/config.json"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Propagate alert config to the running engine
	if s.alerts != nil {
		s.alerts.UpdateConfig(&alerts.AlertConfig{
			WebhookURL:          s.cfg.Alerts.WebhookURL,
			MinerOfflineSeconds: s.cfg.Alerts.OfflineMinutes * 60,
			TempAbove:           s.cfg.Alerts.TempThresholdC,
			HashrateDropPercent: s.cfg.Alerts.HashrateDropPct,
			FanRPMBelow:         s.cfg.Alerts.FanRPMBelow,
			WifiSignalBelow:     s.cfg.Alerts.WifiSignalBelow,
			OnShareRejected:     s.cfg.Alerts.OnShareRejected,
			OnPoolDisconnected:  s.cfg.Alerts.OnPoolDisconnected,
			OnNewBestDiff:       s.cfg.Alerts.OnNewBestDiff,
			OnBlockFound:        s.cfg.Alerts.OnBlockFound,
			OnNewLeader:         s.cfg.Alerts.OnNewLeader,
		})
	}

	s.jsonResponse(w, map[string]bool{"success": true})
}

// ScanResponse represents the scan results
type ScanResponse struct {
	Subnets []string         `json:"subnets"`
	Results []*storage.Miner `json:"results"`
}

// handleScan starts a network scan
// POST /api/scan
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	// Detect all available subnets
	subnets := s.scanner.DetectAllSubnets()
	if len(subnets) == 0 {
		http.Error(w, "no network interfaces found", http.StatusInternalServerError)
		return
	}

	log.Printf("Scanning subnets: %v", subnets)

	// Run scan with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Scan all subnets
	var allMiners []*storage.Miner
	seen := make(map[string]bool)

	for _, subnet := range subnets {
		results, err := s.scanner.Scan(ctx, subnet)
		if err != nil {
			log.Printf("Error scanning subnet %s: %v", subnet, err)
			continue
		}

		for _, result := range results {
			// Avoid duplicates (in case same miner appears on multiple interfaces)
			if !seen[result.Miner.IP] {
				seen[result.Miner.IP] = true
				allMiners = append(allMiners, result.Miner)
			}
		}
	}

	log.Printf("Scan complete: found %d miners", len(allMiners))

	s.jsonResponse(w, ScanResponse{
		Subnets: subnets,
		Results: allMiners,
	})
}

// AddMinerRequest represents a request to add a miner
type AddMinerRequest struct {
	IP string `json:"ip"`
}

// handleAddMiner adds a miner by IP
// POST /api/miners
func (s *Server) handleAddMiner(w http.ResponseWriter, r *http.Request) {
	var req AddMinerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.IP == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	// Try to scan this single IP to verify it's a miner
	result, err := s.scanner.ScanSingle(req.IP)
	if err != nil {
		http.Error(w, "failed to connect to miner: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Save miner to storage
	if err := s.storage.UpsertMiner(result.Miner); err != nil {
		http.Error(w, "failed to save miner: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Start collecting from this miner
	s.collector.AddMiner(req.IP)

	s.jsonResponse(w, result.Miner)
}

// handleStatic serves static files
// GET /*
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Serve index.html for root
	if path == "/" || path == "" {
		filePath := "web/templates/index.html"
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, filePath)
		return
	}

	// Serve other static files
	filePath := filepath.Join("web", path)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// If file doesn't exist, serve index.html for SPA routing
		indexPath := "web/templates/index.html"
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, indexPath)
		return
	}

	// Disable cache for JS files during development
	if strings.HasSuffix(path, ".js") {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}

	http.ServeFile(w, r, filePath)
}

// HistoryPoint represents a point in time series data
type HistoryPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	Hashrate    float64   `json:"hashrate"`    // GH/s - current/1min
	Hashrate10m float64   `json:"hashrate10m"` // GH/s - 10min average
	Hashrate1h  float64   `json:"hashrate1h"`  // GH/s - 1h average
	TempASIC    float64   `json:"tempAsic"`    // °C
	TempVReg    float64   `json:"tempVreg"`    // °C
	Power       float64   `json:"power"`       // Watts
}

// handleGetHistory returns aggregated hashrate history for the last hour
// GET /api/history
func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fixed 1 hour timeframe with 5 second sampling for detailed oscillations
	since := time.Now().Add(-1 * time.Hour)
	sampleInterval := 5 * time.Second

	// For each time bucket, store snapshot data per miner
	type minerData struct {
		hashrate1m  float64 // 1min hashrate from miner
		hashrate10m float64 // 10min average from miner
		hashrate1h  float64 // 1h average from miner
		tempASIC    float64
		tempVReg    float64
		power       float64
	}
	buckets := make(map[time.Time]map[string]minerData)

	for _, m := range miners {
		snapshots, err := s.storage.GetSnapshots(m.IP, since, 20000)
		if err != nil {
			continue
		}

		for _, snap := range snapshots {
			rounded := snap.Timestamp.Truncate(sampleInterval)

			if buckets[rounded] == nil {
				buckets[rounded] = make(map[string]minerData)
			}

			// Always update with latest snapshot for this bucket
			buckets[rounded][m.IP] = minerData{
				hashrate1m:  snap.HashRate1m,  // Use miner's 1m average
				hashrate10m: snap.HashRate10m, // Use miner's 10m average
				hashrate1h:  snap.HashRate1h,  // Use miner's 1h average
				tempASIC:    snap.Temperature,
				tempVReg:    snap.VRTemp,
				power:       snap.Power,
			}
		}
	}

	// Aggregate across miners for each time bucket
	var history []HistoryPoint
	for ts, minerMap := range buckets {
		var totalHash1m, totalHash10m, totalHash1h, totalPower float64
		var avgTempASIC, avgTempVReg float64
		count := 0
		for _, data := range minerMap {
			totalHash1m += data.hashrate1m
			totalHash10m += data.hashrate10m
			totalHash1h += data.hashrate1h
			totalPower += data.power
			avgTempASIC += data.tempASIC
			avgTempVReg += data.tempVReg
			count++
		}
		if count > 0 {
			avgTempASIC /= float64(count)
			avgTempVReg /= float64(count)
		}
		history = append(history, HistoryPoint{
			Timestamp:   ts,
			Hashrate:    totalHash1m,  // 1min average shows oscillations
			Hashrate10m: totalHash10m, // 10min average from miner
			Hashrate1h:  totalHash1h,  // 1h average from miner
			TempASIC:    avgTempASIC,
			TempVReg:    avgTempVReg,
			Power:       totalPower,
		})
	}

	// Sort by timestamp
	for i := 0; i < len(history)-1; i++ {
		for j := i + 1; j < len(history); j++ {
			if history[i].Timestamp.After(history[j].Timestamp) {
				history[i], history[j] = history[j], history[i]
			}
		}
	}

	s.jsonResponse(w, history)
}

// BestShareInfo contains best share data
type BestShareInfo struct {
	Difficulty float64 `json:"difficulty"`
	Hostname   string  `json:"hostname"`
	MinerIP    string  `json:"minerIp"`
}

// BestSharesResponse contains best shares info
type BestSharesResponse struct {
	AllTime *BestShareInfo `json:"allTime,omitempty"`
	Session *BestShareInfo `json:"session,omitempty"`
}

// handleGetBestShares returns the best shares across all miners
// GET /api/shares/best
func (s *Server) handleGetBestShares(w http.ResponseWriter, r *http.Request) {
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var bestAllTime, bestSession *BestShareInfo

	for _, m := range miners {
		// Get latest snapshot for this miner to get bestDiff values
		snapshots, err := s.storage.GetSnapshots(m.IP, time.Now().Add(-5*time.Minute), 1)
		if err != nil || len(snapshots) == 0 {
			continue
		}
		snap := snapshots[0]

		// All time best (from miner's bestDiff)
		if snap.BestDiff > 0 {
			if bestAllTime == nil || snap.BestDiff > bestAllTime.Difficulty {
				bestAllTime = &BestShareInfo{
					Difficulty: snap.BestDiff,
					Hostname:   m.Hostname,
					MinerIP:    m.IP,
				}
			}
		}

		// Session best (from miner's bestSessionDiff - since last boot)
		if snap.BestDiffSess > 0 {
			if bestSession == nil || snap.BestDiffSess > bestSession.Difficulty {
				bestSession = &BestShareInfo{
					Difficulty: snap.BestDiffSess,
					Hostname:   m.Hostname,
					MinerIP:    m.IP,
				}
			}
		}
	}

	s.jsonResponse(w, BestSharesResponse{
		AllTime: bestAllTime,
		Session: bestSession,
	})
}

// handlePurge purges old data
// POST /api/purge
func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	if err := s.storage.PurgeOldData(days); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]bool{"success": true})
}

// handleGetDBSize returns the database file size
// GET /api/dbsize
func (s *Server) handleGetDBSize(w http.ResponseWriter, r *http.Request) {
	info, err := os.Stat(s.cfg.DBPath)
	if err != nil {
		s.jsonResponse(w, map[string]interface{}{
			"size":      0,
			"sizeHuman": "Unknown",
		})
		return
	}

	size := info.Size()
	var sizeHuman string
	switch {
	case size >= 1<<30:
		sizeHuman = fmt.Sprintf("%.2f GB", float64(size)/(1<<30))
	case size >= 1<<20:
		sizeHuman = fmt.Sprintf("%.2f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		sizeHuman = fmt.Sprintf("%.2f KB", float64(size)/(1<<10))
	default:
		sizeHuman = fmt.Sprintf("%d B", size)
	}

	s.jsonResponse(w, map[string]interface{}{
		"size":      size,
		"sizeHuman": sizeHuman,
	})
}

// handleGetCoins returns the list of supported coins
// GET /api/coins
func (s *Server) handleGetCoins(w http.ResponseWriter, r *http.Request) {
	coins := pricing.GetSupportedCoins()
	s.jsonResponse(w, coins)
}

// CoinEarningsDetail contains earnings for a specific coin
type CoinEarningsDetail struct {
	CoinID        string  `json:"coinId"`
	CoinSymbol    string  `json:"coinSymbol"`
	CoinIcon      string  `json:"coinIcon"`
	TotalCoins    float64 `json:"totalCoins"`
	BlockCount    int     `json:"blockCount"`
	HistoricalUSD float64 `json:"historicalUsd"` // Value when mined
	CurrentPrice  float64 `json:"currentPrice"`
	CurrentUSD    float64 `json:"currentUsd"` // Value at current price
}

// EarningsResponse contains earnings calculation
type EarningsResponse struct {
	Coins         []CoinEarningsDetail `json:"coins"`
	TotalBlocks   int                  `json:"totalBlocks"`
	TotalEarnedUSD float64             `json:"totalEarnedUsd"`   // Historical total
	TotalCurrentUSD float64            `json:"totalCurrentUsd"`  // Current total
}

// handleGetEarnings returns earnings for all coins being mined
// GET /api/earnings
// Includes coins configured on miners even if no blocks found yet
func (s *Server) handleGetEarnings(w http.ResponseWriter, r *http.Request) {
	// 1. Collect all unique coins being mined (from miner configs)
	miners, err := s.storage.GetMiners()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	activeCoinIDs := make(map[string]bool)
	for _, m := range miners {
		coinID := m.CoinID
		if coinID == "" {
			coinID = "dgb" // default fallback for miners without a coin set
		}
		activeCoinIDs[coinID] = true
	}

	// 2. Get actual earnings (coins with blocks)
	allEarnings, err := s.storage.GetTotalEarnings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	earningsByCoin := make(map[string]*storage.CoinEarnings)
	for _, e := range allEarnings {
		earningsByCoin[e.CoinID] = e
		// Also include coins with blocks even if no miner is currently set to them
		activeCoinIDs[e.CoinID] = true
	}

	// 3. Build response for all active coins
	var response EarningsResponse
	for coinID := range activeCoinIDs {
		currentPrice := s.pricing.GetPriceForCoin(coinID)
		coinInfo := s.pricing.GetCoinInfoByID(coinID)

		coinIcon := ""
		coinSymbol := strings.ToUpper(coinID)
		if coinInfo != nil {
			coinIcon = coinInfo.Icon
			coinSymbol = coinInfo.Symbol
		}

		detail := CoinEarningsDetail{
			CoinID:       coinID,
			CoinSymbol:   coinSymbol,
			CoinIcon:     coinIcon,
			CurrentPrice: currentPrice,
		}

		if e, ok := earningsByCoin[coinID]; ok {
			detail.TotalCoins = e.TotalCoins
			detail.BlockCount = e.BlockCount
			detail.HistoricalUSD = e.HistoricalUSD
			detail.CurrentUSD = e.TotalCoins * currentPrice

			response.TotalBlocks += e.BlockCount
			response.TotalEarnedUSD += e.HistoricalUSD
			response.TotalCurrentUSD += detail.CurrentUSD
		}

		response.Coins = append(response.Coins, detail)
	}

	if len(response.Coins) == 0 {
		response.Coins = []CoinEarningsDetail{}
	}

	s.jsonResponse(w, response)
}

// handleTestAlert sends a test alert to the configured Discord webhook.
// POST /api/alerts/test
// Body (optional): {"type": "block_found"} — sends a sample alert for that type.
// Empty body or no type — sends the generic connectivity test.
func (s *Server) handleTestAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string `json:"type"`
	}
	// Best-effort decode; empty body is fine
	_ = json.NewDecoder(r.Body).Decode(&req)

	var err error
	if req.Type != "" {
		err = s.alerts.SendTestAlertByType(req.Type)
	} else {
		err = s.alerts.SendTestAlert()
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.jsonResponse(w, map[string]bool{"success": true})
}

// jsonResponse sends a JSON response
func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}
