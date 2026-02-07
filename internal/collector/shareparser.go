package collector

import (
	"regexp"
	"strconv"
	"time"

	"github.com/camarigor/miner-hq/internal/storage"
)

// Example log line:
// asic_result: (Pri) Job ID: 18 AsicNr: 3 Ver: 23B82202 Nonce F854197E; Extranonce2 001c0041 diff 5894.3/18304/3.70G

var shareRegex = regexp.MustCompile(
	`asic_result:.*Job ID:\s*(\d+)\s+AsicNr:\s*(\d+).*diff\s+([\d.]+)`,
)

type ShareParser struct{}

func NewShareParser() *ShareParser {
	return &ShareParser{}
}

// Parse attempts to parse a share from a log line
// Returns nil if the line is not a share result
func (p *ShareParser) Parse(minerIP string, line string) *storage.Share {
	matches := shareRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	jobID := matches[1]
	asicNum, _ := strconv.Atoi(matches[2])
	difficulty, _ := strconv.ParseFloat(matches[3], 64)

	return &storage.Share{
		MinerIP:    minerIP,
		Timestamp:  time.Now(),
		AsicNum:    asicNum,
		Difficulty: difficulty,
		JobID:      jobID,
	}
}

// FormatDifficulty formats difficulty as human-readable (K, M, G)
func FormatDifficulty(diff float64) string {
	switch {
	case diff >= 1e9:
		return strconv.FormatFloat(diff/1e9, 'f', 2, 64) + "G"
	case diff >= 1e6:
		return strconv.FormatFloat(diff/1e6, 'f', 2, 64) + "M"
	case diff >= 1e3:
		return strconv.FormatFloat(diff/1e3, 'f', 2, 64) + "K"
	default:
		return strconv.FormatFloat(diff, 'f', 1, 64)
	}
}
