package scanner

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/camarigor/miner-hq/internal/collector"
	"github.com/camarigor/miner-hq/internal/storage"
)

func TestExpandSubnet(t *testing.T) {
	s := NewScanner()

	tests := []struct {
		name      string
		subnet    string
		wantCount int
		wantFirst string
		wantLast  string
		wantErr   bool
	}{
		{
			name:      "standard /24 network",
			subnet:    "192.168.1.0/24",
			wantCount: 254,
			wantFirst: "192.168.1.1",
			wantLast:  "192.168.1.254",
			wantErr:   false,
		},
		{
			name:      "10.x.x.x /24 network",
			subnet:    "10.7.7.0/24",
			wantCount: 254,
			wantFirst: "10.7.7.1",
			wantLast:  "10.7.7.254",
			wantErr:   false,
		},
		{
			name:      "172.16 /24 network",
			subnet:    "172.16.0.0/24",
			wantCount: 254,
			wantFirst: "172.16.0.1",
			wantLast:  "172.16.0.254",
			wantErr:   false,
		},
		{
			name:      "smaller /28 network",
			subnet:    "192.168.1.0/28",
			wantCount: 14, // 16 - 2 (network and broadcast)
			wantFirst: "192.168.1.1",
			wantLast:  "192.168.1.14",
			wantErr:   false,
		},
		{
			name:    "invalid CIDR",
			subnet:  "invalid",
			wantErr: true,
		},
		{
			name:    "missing mask",
			subnet:  "192.168.1.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, err := s.expandSubnet(tt.subnet)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expandSubnet(%q) expected error, got nil", tt.subnet)
				}
				return
			}

			if err != nil {
				t.Errorf("expandSubnet(%q) unexpected error: %v", tt.subnet, err)
				return
			}

			if len(ips) != tt.wantCount {
				t.Errorf("expandSubnet(%q) got %d IPs, want %d", tt.subnet, len(ips), tt.wantCount)
			}

			if len(ips) > 0 {
				if ips[0] != tt.wantFirst {
					t.Errorf("expandSubnet(%q) first IP = %q, want %q", tt.subnet, ips[0], tt.wantFirst)
				}
				if ips[len(ips)-1] != tt.wantLast {
					t.Errorf("expandSubnet(%q) last IP = %q, want %q", tt.subnet, ips[len(ips)-1], tt.wantLast)
				}
			}
		})
	}
}

func TestExpandSubnetExcludesNetworkAndBroadcast(t *testing.T) {
	s := NewScanner()

	ips, err := s.expandSubnet("192.168.1.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .0 is not in the list
	for _, ip := range ips {
		if strings.HasSuffix(ip, ".0") {
			t.Errorf("network address found in list: %s", ip)
		}
		if strings.HasSuffix(ip, ".255") {
			t.Errorf("broadcast address found in list: %s", ip)
		}
	}
}

func TestDetectSubnet(t *testing.T) {
	s := NewScanner()

	subnet, err := s.DetectSubnet()

	// In a container or CI environment, this might fail
	// which is acceptable - we just verify the format if it succeeds
	if err != nil {
		t.Skipf("DetectSubnet failed (may be expected in container): %v", err)
		return
	}

	// Verify it returns a /24
	if !strings.HasSuffix(subnet, "/24") {
		t.Errorf("DetectSubnet() = %q, want suffix /24", subnet)
	}

	// Verify it's a valid CIDR
	_, err = s.expandSubnet(subnet)
	if err != nil {
		t.Errorf("DetectSubnet() returned invalid CIDR %q: %v", subnet, err)
	}

	t.Logf("Detected subnet: %s", subnet)
}

func TestIsNerdQAxe(t *testing.T) {
	s := NewScanner()

	tests := []struct {
		name        string
		deviceModel string
		asicModel   string
		want        bool
	}{
		{
			name:        "NerdQAxe++",
			deviceModel: "NerdQAxe++",
			asicModel:   "BM1370",
			want:        true,
		},
		{
			name:        "NerdQAxe+ with BM1368",
			deviceModel: "NerdQAxe+",
			asicModel:   "BM1368",
			want:        true,
		},
		{
			name:        "NerdAxe",
			deviceModel: "NerdAxe",
			asicModel:   "",
			want:        true,
		},
		{
			name:        "NerdOctaxe",
			deviceModel: "NerdOctaxe",
			asicModel:   "",
			want:        true,
		},
		{
			name:        "case insensitive match",
			deviceModel: "nerdqaxe++",
			asicModel:   "",
			want:        true,
		},
		{
			name:        "ASIC model only BM1370",
			deviceModel: "Unknown",
			asicModel:   "BM1370",
			want:        true,
		},
		{
			name:        "ASIC model only BM1368",
			deviceModel: "Unknown",
			asicModel:   "BM1368",
			want:        true,
		},
		{
			name:        "generic device with Nerd in name",
			deviceModel: "NerdMiner v2",
			asicModel:   "",
			want:        true,
		},
		{
			name:        "unknown device",
			deviceModel: "AntMiner S19",
			asicModel:   "Unknown",
			want:        false,
		},
		{
			name:        "empty models",
			deviceModel: "",
			asicModel:   "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &collector.MinerAPIResponse{
				DeviceModel: tt.deviceModel,
				ASICModel:   tt.asicModel,
			}

			got := s.isNerdQAxe(info)
			if got != tt.want {
				t.Errorf("isNerdQAxe(%q, %q) = %v, want %v", tt.deviceModel, tt.asicModel, got, tt.want)
			}
		})
	}
}

func TestNewScanner(t *testing.T) {
	s := NewScanner()

	if s == nil {
		t.Fatal("NewScanner() returned nil")
	}

	if s.concurrency != 50 {
		t.Errorf("NewScanner() concurrency = %d, want 50", s.concurrency)
	}

	if s.timeout != 2*time.Second {
		t.Errorf("NewScanner() timeout = %v, want %v", s.timeout, 2*time.Second)
	}

	if s.client == nil {
		t.Error("NewScanner() client is nil")
	}
}

func TestNewScannerWithOptions(t *testing.T) {
	s := NewScannerWithOptions(100, 5*time.Second)

	if s.concurrency != 100 {
		t.Errorf("NewScannerWithOptions() concurrency = %d, want 100", s.concurrency)
	}

	if s.timeout != 5*time.Second {
		t.Errorf("NewScannerWithOptions() timeout = %v, want %v", s.timeout, 5*time.Second)
	}
}

func TestIncIP(t *testing.T) {
	s := NewScanner()

	tests := []struct {
		name     string
		startIP  string
		expected string
	}{
		{
			name:     "simple increment",
			startIP:  "192.168.1.1",
			expected: "192.168.1.2",
		},
		{
			name:     "octet rollover",
			startIP:  "192.168.1.255",
			expected: "192.168.2.0",
		},
		{
			name:     "multiple octet rollover",
			startIP:  "192.168.255.255",
			expected: "192.169.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse IP
			ip := net.ParseIP(tt.startIP).To4()
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.startIP)
			}

			s.incIP(ip)

			got := ip.String()
			if got != tt.expected {
				t.Errorf("incIP(%s) = %s, want %s", tt.startIP, got, tt.expected)
			}
		})
	}
}

func TestScanContextCancellation(t *testing.T) {
	s := NewScanner()

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Scan should return early with context error
	results, err := s.Scan(ctx, "192.168.1.0/24")

	// Either no results or context error is acceptable
	if err != nil && err != context.Canceled {
		t.Errorf("Scan with cancelled context unexpected error: %v", err)
	}

	// Should have very few or no results since context was cancelled
	if len(results) > 10 {
		t.Errorf("Scan with cancelled context returned too many results: %d", len(results))
	}
}

func TestScanResultStructure(t *testing.T) {
	// Test that ScanResult can hold the expected types
	result := ScanResult{
		Miner: &storage.Miner{
			IP:          "192.168.1.100",
			Hostname:    "test-miner",
			DeviceModel: "NerdQAxe++",
			ASICModel:   "BM1370",
			Enabled:     true,
			Online:      true,
		},
		Info: &collector.MinerAPIResponse{
			DeviceModel: "NerdQAxe++",
			ASICModel:   "BM1370",
			Hostname:    "test-miner",
			HashRate:    1234.56,
		},
	}

	if result.Miner == nil {
		t.Error("ScanResult.Miner should not be nil")
	}
	if result.Info == nil {
		t.Error("ScanResult.Info should not be nil")
	}
	if result.Miner.IP != "192.168.1.100" {
		t.Errorf("ScanResult.Miner.IP = %q, want %q", result.Miner.IP, "192.168.1.100")
	}
}

// Ensure unused imports are used
var _ = fmt.Sprintf
