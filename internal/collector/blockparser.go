package collector

import (
	"regexp"
	"strconv"
	"time"

	"github.com/camarigor/miner-hq/internal/storage"
)

// Example log line from miner:
// I (12345) STRATUM_MANAGER: FOUND BLOCK!!! 123456789.0 > 123000.0

var blockRegex = regexp.MustCompile(
	`FOUND BLOCK!!!\s+([\d.]+)\s*>\s*([\d.]+)`,
)

type BlockParser struct{}

func NewBlockParser() *BlockParser {
	return &BlockParser{}
}

// Parse attempts to parse a block found event from a log line
// Returns nil if the line is not a block found event
func (p *BlockParser) Parse(minerIP string, line string) *storage.Block {
	matches := blockRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	difficulty, _ := strconv.ParseFloat(matches[1], 64)
	networkDiff, _ := strconv.ParseFloat(matches[2], 64)

	return &storage.Block{
		MinerIP:           minerIP,
		Timestamp:         time.Now(),
		Difficulty:        difficulty,
		NetworkDifficulty: networkDiff,
	}
}
