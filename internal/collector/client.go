package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/camarigor/miner-hq/internal/storage"
)

// MinerAPIResponse matches the /api/system/info response from NerdQAxe and AxeOS/Zyber firmware.
// AxeOS-specific fields are zero-valued when not present in the JSON response.
type MinerAPIResponse struct {
	DeviceModel     string  `json:"deviceModel"`
	ASICModel       string  `json:"ASICModel"`
	Hostname        string  `json:"hostname"`
	HostIP          string  `json:"hostip"`
	MacAddr         string  `json:"macAddr"`
	Version         string  `json:"version"`
	HashRate        float64 `json:"hashRate"`
	HashRate1m      float64 `json:"hashRate_1m"`
	HashRate10m     float64 `json:"hashRate_10m"`
	HashRate1h      float64 `json:"hashRate_1h"`
	HashRate1d      float64 `json:"hashRate_1d"`
	Temp            float64 `json:"temp"`
	VRTemp          float64 `json:"vrTemp"`
	Power           float64 `json:"power"`
	Voltage         float64 `json:"voltage"`
	CoreVoltage     int     `json:"coreVoltage"`
	Frequency       int     `json:"frequency"`
	FanRPM          int     `json:"fanrpm"`
	FanSpeed        int     `json:"fanspeed"`
	SharesAccepted   int64   `json:"sharesAccepted"`
	SharesRejected   int64   `json:"sharesRejected"`
	BestDiff         float64 `json:"bestDiff"`
	BestSessionDiff  float64 `json:"bestSessionDiff"`
	FoundBlocks      int     `json:"foundBlocks"`
	TotalFoundBlocks int     `json:"totalFoundBlocks"`
	PoolDifficulty  float64 `json:"poolDifficulty"`
	UptimeSeconds   int64   `json:"uptimeSeconds"`
	WifiRSSI        int     `json:"wifiRSSI"`
	ASICCount       int     `json:"asicCount"`
	SmallCoreCount  int     `json:"smallCoreCount"`
	Stratum         struct {
		Pools []struct {
			Connected      bool    `json:"connected"`
			PoolDifficulty float64 `json:"poolDifficulty"`
			Accepted       int64   `json:"accepted"`
			Rejected       int64   `json:"rejected"`
			BestDiff       float64 `json:"bestDiff"`
		} `json:"pools"`
	} `json:"stratum"`

	// AxeOS/Zyber-specific fields (zero-value when not present)
	AxeOSVersion    string  `json:"axeOSVersion"`
	BoardVersion    string  `json:"boardVersion"`
	StratumURL      string  `json:"stratumURL"`
	StratumPort     int     `json:"stratumPort"`
	StratumUser     string  `json:"stratumUser"`
	IsUsingFallback int     `json:"isUsingFallbackStratum"`
	BlockFound      int     `json:"blockFound"`
	BlockHeight     int64   `json:"blockHeight"`
	NetworkDiff     float64 `json:"networkDifficulty"`
	Temp2           float64 `json:"temp2"`
	Fan2RPM         int     `json:"fan2rpm"`
}

// MinerClient handles communication with NerdQAxe miners
type MinerClient struct {
	httpClient *http.Client
}

// NewMinerClient creates a new MinerClient with default timeout
func NewMinerClient() *MinerClient {
	return &MinerClient{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// FetchInfo fetches miner info from the REST API
func (c *MinerClient) FetchInfo(ip string) (*MinerAPIResponse, error) {
	url := fmt.Sprintf("http://%s/api/system/info", ip)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch miner info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var info MinerAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &info, nil
}

// ToSnapshot converts API response to storage.MinerSnapshot
func (c *MinerClient) ToSnapshot(ip string, info *MinerAPIResponse) *storage.MinerSnapshot {
	isAxeOS := info.AxeOSVersion != ""

	// Pool connection: AxeOS has no stratum.pools[].connected field,
	// so we infer from accepted shares + configured stratum URL.
	poolConnected := false
	if isAxeOS {
		poolConnected = info.SharesAccepted > 0 && info.StratumURL != ""
	} else if len(info.Stratum.Pools) > 0 {
		poolConnected = info.Stratum.Pools[0].Connected
	}

	// Found blocks: AxeOS uses "blockFound" instead of "foundBlocks"
	foundBlocks := info.FoundBlocks
	if isAxeOS && info.BlockFound > 0 {
		foundBlocks = info.BlockFound
	}

	// Device model: AxeOS doesn't send "deviceModel", build from BoardVersion + ASICModel
	deviceModel := info.DeviceModel
	if deviceModel == "" && isAxeOS {
		deviceModel = fmt.Sprintf("AxeOS (%s)", info.ASICModel)
	}

	return &storage.MinerSnapshot{
		MinerIP:       ip,
		Timestamp:     time.Now(),
		Hostname:      info.Hostname,
		DeviceModel:   deviceModel,
		HashRate:      info.HashRate,
		HashRate1m:    info.HashRate1m,
		HashRate10m:   info.HashRate10m,
		HashRate1h:    info.HashRate1h,
		HashRate1d:    info.HashRate1d,
		Temperature:   info.Temp,
		VRTemp:        info.VRTemp,
		Power:         info.Power,
		Voltage:       info.Voltage,
		FanRPM:        info.FanRPM,
		FanPercent:    info.FanSpeed,
		SharesAccept:  info.SharesAccepted,
		SharesReject:  info.SharesRejected,
		BestDiff:      info.BestDiff,
		BestDiffSess:  info.BestSessionDiff,
		PoolDiff:      info.PoolDifficulty,
		PoolConnected:    poolConnected,
		UptimeSecs:       info.UptimeSeconds,
		WifiRSSI:         info.WifiRSSI,
		FoundBlocks:      foundBlocks,
		TotalFoundBlocks: info.TotalFoundBlocks,
	}
}

// ToMiner converts API response to storage.Miner
func (c *MinerClient) ToMiner(ip string, info *MinerAPIResponse) *storage.Miner {
	deviceModel := info.DeviceModel
	if deviceModel == "" && info.AxeOSVersion != "" {
		deviceModel = fmt.Sprintf("AxeOS (%s)", info.ASICModel)
	}

	return &storage.Miner{
		IP:          ip,
		Hostname:    info.Hostname,
		DeviceModel: deviceModel,
		ASICModel:   info.ASICModel,
		Enabled:     true,
		LastSeen:    time.Now(),
		Online:      true,
	}
}
