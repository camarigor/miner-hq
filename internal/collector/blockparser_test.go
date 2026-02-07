package collector

import (
	"testing"
)

func TestBlockParser_Parse(t *testing.T) {
	parser := NewBlockParser()

	tests := []struct {
		name       string
		line       string
		wantBlock  bool
		wantDiff   float64
		wantNetDiff float64
	}{
		{
			name:       "valid block found message",
			line:       "I (12345) STRATUM_MANAGER: FOUND BLOCK!!! 123456789.0 > 123000.0",
			wantBlock:  true,
			wantDiff:   123456789.0,
			wantNetDiff: 123000.0,
		},
		{
			name:       "block with integer diff",
			line:       "FOUND BLOCK!!! 5000000 > 4500000",
			wantBlock:  true,
			wantDiff:   5000000.0,
			wantNetDiff: 4500000.0,
		},
		{
			name:       "not a block message",
			line:       "asic_result: (Pri) Job ID: 18 AsicNr: 3 Ver: 23B82202 Nonce F854197E",
			wantBlock:  false,
		},
		{
			name:       "empty line",
			line:       "",
			wantBlock:  false,
		},
		{
			name:       "normal share message",
			line:       "I (12345) STRATUM_MANAGER: Share accepted",
			wantBlock:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := parser.Parse("192.168.1.100", tt.line)

			if tt.wantBlock {
				if block == nil {
					t.Error("expected block, got nil")
					return
				}
				if block.Difficulty != tt.wantDiff {
					t.Errorf("difficulty = %v, want %v", block.Difficulty, tt.wantDiff)
				}
				if block.NetworkDifficulty != tt.wantNetDiff {
					t.Errorf("networkDifficulty = %v, want %v", block.NetworkDifficulty, tt.wantNetDiff)
				}
				if block.MinerIP != "192.168.1.100" {
					t.Errorf("minerIP = %v, want 192.168.1.100", block.MinerIP)
				}
			} else {
				if block != nil {
					t.Errorf("expected nil block, got %+v", block)
				}
			}
		})
	}
}
