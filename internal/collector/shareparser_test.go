package collector

import (
	"testing"
)

func TestShareParser_ParseValidShareLine(t *testing.T) {
	parser := NewShareParser()
	minerIP := "192.168.1.100"
	line := "I (858876424) asic_result: (Pri) Job ID: 18 AsicNr: 3 Ver: 23B82202 Nonce F854197E; Extranonce2 001c0041 diff 5894.3/18304/3.70G"

	share := parser.Parse(minerIP, line)

	if share == nil {
		t.Fatal("expected share to be parsed, got nil")
	}

	if share.MinerIP != minerIP {
		t.Errorf("expected MinerIP %q, got %q", minerIP, share.MinerIP)
	}

	if share.JobID != "18" {
		t.Errorf("expected JobID %q, got %q", "18", share.JobID)
	}

	if share.AsicNum != 3 {
		t.Errorf("expected AsicNum %d, got %d", 3, share.AsicNum)
	}

	if share.Difficulty != 5894.3 {
		t.Errorf("expected Difficulty %f, got %f", 5894.3, share.Difficulty)
	}

	if share.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set, got zero time")
	}
}

func TestShareParser_ParseHighDifficultyShare(t *testing.T) {
	parser := NewShareParser()
	minerIP := "10.0.0.50"
	line := "I (999999999) asic_result: (Pri) Job ID: 42 AsicNr: 7 Ver: ABCDEF12 Nonce 12345678; Extranonce2 00FF00FF diff 1234567.89/50000/10.5G"

	share := parser.Parse(minerIP, line)

	if share == nil {
		t.Fatal("expected share to be parsed, got nil")
	}

	if share.JobID != "42" {
		t.Errorf("expected JobID %q, got %q", "42", share.JobID)
	}

	if share.AsicNum != 7 {
		t.Errorf("expected AsicNum %d, got %d", 7, share.AsicNum)
	}

	if share.Difficulty != 1234567.89 {
		t.Errorf("expected Difficulty %f, got %f", 1234567.89, share.Difficulty)
	}
}

func TestShareParser_ParseAxeOSShareLine(t *testing.T) {
	parser := NewShareParser()
	minerIP := "192.168.1.23"
	line := `I (16860088) asic_result: ID: 69868e2b00000b0b, ASIC nr: 0, ver: 24564000 Nonce C27001F0 diff 432.8 of 2048.`

	share := parser.Parse(minerIP, line)

	if share == nil {
		t.Fatal("expected AxeOS share to be parsed, got nil")
	}

	if share.MinerIP != minerIP {
		t.Errorf("expected MinerIP %q, got %q", minerIP, share.MinerIP)
	}

	if share.JobID != "69868e2b00000b0b" {
		t.Errorf("expected JobID %q, got %q", "69868e2b00000b0b", share.JobID)
	}

	if share.AsicNum != 0 {
		t.Errorf("expected AsicNum %d, got %d", 0, share.AsicNum)
	}

	if share.Difficulty != 432.8 {
		t.Errorf("expected Difficulty %f, got %f", 432.8, share.Difficulty)
	}
}

func TestShareParser_ParseAxeOSHighDiffShare(t *testing.T) {
	parser := NewShareParser()
	line := `I (16861575) asic_result: ID: 69868e2b00000b0b, ASIC nr: 0, ver: 242A8000 Nonce 74CE0428 diff 6880.0 of 2048.`

	share := parser.Parse("10.0.0.1", line)

	if share == nil {
		t.Fatal("expected AxeOS high-diff share to be parsed, got nil")
	}

	if share.Difficulty != 6880.0 {
		t.Errorf("expected Difficulty %f, got %f", 6880.0, share.Difficulty)
	}
}

func TestShareParser_NonShareLineReturnsNil(t *testing.T) {
	parser := NewShareParser()

	testCases := []struct {
		name string
		line string
	}{
		{
			name: "info log line",
			line: "I (12345) system: Starting up miner...",
		},
		{
			name: "stratum message",
			line: "I (12345) stratum: Connected to pool",
		},
		{
			name: "temperature log",
			line: "I (12345) temp: Current temperature: 65C",
		},
		{
			name: "partial asic_result without diff",
			line: "I (12345) asic_result: initializing",
		},
		{
			name: "asic_result without job ID",
			line: "I (12345) asic_result: AsicNr: 3 diff 1000.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			share := parser.Parse("192.168.1.1", tc.line)
			if share != nil {
				t.Errorf("expected nil for non-share line, got %+v", share)
			}
		})
	}
}

func TestShareParser_EmptyLineReturnsNil(t *testing.T) {
	parser := NewShareParser()

	share := parser.Parse("192.168.1.1", "")
	if share != nil {
		t.Errorf("expected nil for empty line, got %+v", share)
	}
}

func TestShareParser_WhitespaceOnlyLineReturnsNil(t *testing.T) {
	parser := NewShareParser()

	share := parser.Parse("192.168.1.1", "   \t\n  ")
	if share != nil {
		t.Errorf("expected nil for whitespace-only line, got %+v", share)
	}
}

func TestFormatDifficulty(t *testing.T) {
	testCases := []struct {
		name     string
		diff     float64
		expected string
	}{
		{
			name:     "giga difficulty",
			diff:     5.5e9,
			expected: "5.50G",
		},
		{
			name:     "mega difficulty",
			diff:     2.34e6,
			expected: "2.34M",
		},
		{
			name:     "kilo difficulty",
			diff:     5894.3,
			expected: "5.89K",
		},
		{
			name:     "sub-kilo difficulty",
			diff:     123.456,
			expected: "123.5",
		},
		{
			name:     "small difficulty",
			diff:     1.5,
			expected: "1.5",
		},
		{
			name:     "exact giga boundary",
			diff:     1e9,
			expected: "1.00G",
		},
		{
			name:     "exact mega boundary",
			diff:     1e6,
			expected: "1.00M",
		},
		{
			name:     "exact kilo boundary",
			diff:     1e3,
			expected: "1.00K",
		},
		{
			name:     "zero difficulty",
			diff:     0,
			expected: "0.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatDifficulty(tc.diff)
			if result != tc.expected {
				t.Errorf("FormatDifficulty(%f): expected %q, got %q", tc.diff, tc.expected, result)
			}
		})
	}
}

func TestNewShareParser(t *testing.T) {
	parser := NewShareParser()
	if parser == nil {
		t.Error("expected NewShareParser to return non-nil parser")
	}
}
