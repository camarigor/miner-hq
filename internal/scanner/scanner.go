package scanner

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/camarigor/miner-hq/internal/collector"
	"github.com/camarigor/miner-hq/internal/storage"
)

// Known NerdQAxe device models
var knownDeviceModels = []string{
	"NerdQAxe++",
	"NerdQAxe+",
	"NerdQAxePlus",
	"NerdAxe",
	"NerdOctaxe",
	"NerdAxe+",
	"NerdAxe++",
}

// Known ASIC models used by NerdQAxe devices
var knownASICModels = []string{
	"BM1370",
	"BM1368",
	"BM1366",
}

// ScanResult represents a discovered miner
type ScanResult struct {
	Miner *storage.Miner
	Info  *collector.MinerAPIResponse
}

// Scanner scans networks for supported miners (NerdQAxe, AxeOS/Zyber)
type Scanner struct {
	client      *collector.MinerClient
	concurrency int
	timeout     time.Duration
}

// NewScanner creates a new Scanner with default settings
func NewScanner() *Scanner {
	return &Scanner{
		client:      collector.NewMinerClient(),
		concurrency: 50,
		timeout:     2 * time.Second,
	}
}

// NewScannerWithOptions creates a new Scanner with custom settings
func NewScannerWithOptions(concurrency int, timeout time.Duration) *Scanner {
	return &Scanner{
		client:      collector.NewMinerClient(),
		concurrency: concurrency,
		timeout:     timeout,
	}
}

// DetectSubnet attempts to detect the local subnet (returns e.g., "10.7.7.0/24")
// Deprecated: Use DetectAllSubnets instead for multi-interface support
func (s *Scanner) DetectSubnet() (string, error) {
	subnets := s.DetectAllSubnets()
	if len(subnets) == 0 {
		return "", fmt.Errorf("no suitable network interface found")
	}
	return subnets[0], nil
}

// DetectAllSubnets returns all local subnets from all network interfaces
func (s *Scanner) DetectAllSubnets() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var subnets []string

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Skip IPv6 and loopback
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Skip link-local addresses (169.254.x.x)
			if ip[0] == 169 && ip[1] == 254 {
				continue
			}

			// Skip Docker bridge networks (172.17.x.x typically)
			if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
				continue
			}

			// Calculate network address with /24 mask
			mask := net.CIDRMask(24, 32)
			network := ip.Mask(mask)
			subnet := fmt.Sprintf("%s/24", network.String())

			// Avoid duplicates
			if !seen[subnet] {
				seen[subnet] = true
				subnets = append(subnets, subnet)
			}
		}
	}

	return subnets
}

// Scan scans the given subnet for supported miners
func (s *Scanner) Scan(ctx context.Context, subnet string) ([]ScanResult, error) {
	ips, err := s.expandSubnet(subnet)
	if err != nil {
		return nil, fmt.Errorf("failed to expand subnet: %w", err)
	}

	var (
		results []ScanResult
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	// Semaphore for concurrency control
	sem := make(chan struct{}, s.concurrency)

	for _, ip := range ips {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			result, err := s.ScanSingle(ip)
			if err == nil && result != nil {
				mu.Lock()
				results = append(results, *result)
				mu.Unlock()
			}
		}(ip)
	}

	wg.Wait()

	return results, nil
}

// ScanSingle checks a single IP for a supported miner (NerdQAxe or AxeOS/Zyber)
func (s *Scanner) ScanSingle(ip string) (*ScanResult, error) {
	info, err := s.client.FetchInfo(ip)
	if err != nil {
		return nil, err
	}

	if !s.isSupportedMiner(info) {
		return nil, fmt.Errorf("device at %s is not a supported miner", ip)
	}

	miner := s.client.ToMiner(ip, info)

	return &ScanResult{
		Miner: miner,
		Info:  info,
	}, nil
}

// isSupportedMiner checks if the device is a known NerdQAxe or AxeOS/Zyber miner
func (s *Scanner) isSupportedMiner(info *collector.MinerAPIResponse) bool {
	// AxeOS/Zyber firmware detection
	if info.AxeOSVersion != "" {
		return true
	}

	// Check deviceModel against known models
	for _, model := range knownDeviceModels {
		if strings.EqualFold(info.DeviceModel, model) {
			return true
		}
		// Also check for partial match (case insensitive)
		if strings.Contains(strings.ToLower(info.DeviceModel), strings.ToLower(model)) {
			return true
		}
	}

	// Check ASICModel against known models
	for _, model := range knownASICModels {
		if strings.EqualFold(info.ASICModel, model) {
			return true
		}
	}

	// If deviceModel contains "Nerd" or "Axe", it's likely a NerdQAxe variant
	lowerModel := strings.ToLower(info.DeviceModel)
	if strings.Contains(lowerModel, "nerd") || strings.Contains(lowerModel, "axe") {
		return true
	}

	return false
}

// expandSubnet converts CIDR to list of IPs (excluding network and broadcast addresses)
func (s *Scanner) expandSubnet(subnet string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR: %w", err)
	}

	var ips []string

	// Get the first IP in the range (network address)
	ip := ipNet.IP.To4()
	if ip == nil {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}

	// Calculate network and broadcast addresses
	networkAddr := make(net.IP, len(ip))
	copy(networkAddr, ip)

	// Calculate broadcast address by inverting the mask
	mask := ipNet.Mask
	broadcastAddr := make(net.IP, len(ip))
	for i := 0; i < len(ip); i++ {
		broadcastAddr[i] = ip[i] | ^mask[i]
	}

	// Create a copy of the IP for iteration (start at network + 1)
	currentIP := make(net.IP, len(ip))
	copy(currentIP, ip)
	s.incIP(currentIP) // Skip network address

	for ipNet.Contains(currentIP) {
		// Skip broadcast address
		if currentIP.Equal(broadcastAddr) {
			break
		}

		ips = append(ips, currentIP.String())
		s.incIP(currentIP)
	}

	return ips, nil
}

// incIP increments an IP address by 1
func (s *Scanner) incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] > 0 {
			break
		}
	}
}
