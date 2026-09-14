package moderation

import (
	"fmt"
	"sort"
)

func DefaultConfig() Config {
	return Config{
		Enabled: true, FailurePolicy: FailOpen,
		RateLimit:     RateLimitConfig{Messages10Sec: 8, Messages60Sec: 25, Messages5Min: 80},
		Duplicate:     DuplicateConfig{WindowSeconds: 300, WarningCount: 4, BlockCount: 6, Levels: []ScoreLevel{{Count: 4, Score: 15}, {Count: 6, Score: 40}, {Count: 10, Score: 80}}},
		Recipients:    RecipientConfig{WindowSeconds: 60, WarningCount: 10, BlockCount: 20, Levels: []ScoreLevel{{Count: 10, Score: 20}, {Count: 20, Score: 50}, {Count: 40, Score: 100}}},
		FuzzyMatching: FuzzyConfig{Enabled: true, MinWordLength: 5, MaxDistanceMedium: 1, MaxDistanceLong: 2},
		Score:         ScoreConfig{Flood10Sec: 25, Flood60Sec: 40, Flood5Min: 60, SuspiciousLink: 20, PhoneSolicitation: 25, DuplicateRecipients: 25},
		Thresholds:    ThresholdConfig{Log: 30, Block: 50, Mute5Minutes: 70, Mute1Hour: 90, Mute24Hours: 120, Review: 150, TempBanSeconds: 604800},
	}
}

func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()
	if c.FailurePolicy == "" {
		c.FailurePolicy = defaults.FailurePolicy
	}
	if c.Thresholds.TempBanSeconds == 0 {
		c.Thresholds.TempBanSeconds = defaults.Thresholds.TempBanSeconds
	}
	if c.Duplicate.WarningCount == 0 {
		c.Duplicate.WarningCount = defaults.Duplicate.WarningCount
	}
	if c.Duplicate.BlockCount == 0 {
		c.Duplicate.BlockCount = defaults.Duplicate.BlockCount
	}
	if len(c.Duplicate.Levels) == 0 {
		c.Duplicate.Levels = []ScoreLevel{{Count: c.Duplicate.WarningCount, Score: 15}, {Count: c.Duplicate.BlockCount, Score: 40}, {Count: c.Duplicate.BlockCount + 4, Score: 80}}
	}
	if c.Recipients.WarningCount == 0 {
		c.Recipients.WarningCount = defaults.Recipients.WarningCount
	}
	if c.Recipients.BlockCount == 0 {
		c.Recipients.BlockCount = defaults.Recipients.BlockCount
	}
	if len(c.Recipients.Levels) == 0 {
		c.Recipients.Levels = []ScoreLevel{{Count: c.Recipients.WarningCount, Score: 20}, {Count: c.Recipients.BlockCount, Score: 50}, {Count: c.Recipients.BlockCount * 2, Score: 100}}
	}
}

func (c *Config) Validate() error {
	c.ApplyDefaults()
	if c.FailurePolicy != FailOpen && c.FailurePolicy != FailClose {
		return fmt.Errorf("redisFailurePolicy non valida")
	}
	if c.RateLimit.Messages10Sec < 1 || c.RateLimit.Messages60Sec < 1 || c.RateLimit.Messages5Min < 1 {
		return fmt.Errorf("i limiti messaggi devono essere positivi")
	}
	if c.Duplicate.WindowSeconds < 1 || c.Recipients.WindowSeconds < 1 {
		return fmt.Errorf("le finestre temporali devono essere positive")
	}
	if c.Duplicate.WarningCount >= c.Duplicate.BlockCount || c.Recipients.WarningCount >= c.Recipients.BlockCount {
		return fmt.Errorf("warningCount deve essere minore di blockCount")
	}
	if c.FuzzyMatching.MinWordLength < 5 || c.FuzzyMatching.MaxDistanceMedium < 0 || c.FuzzyMatching.MaxDistanceLong < c.FuzzyMatching.MaxDistanceMedium {
		return fmt.Errorf("configurazione fuzzy non valida")
	}
	scores := []int{c.Score.Flood10Sec, c.Score.Flood60Sec, c.Score.Flood5Min, c.Score.SuspiciousLink, c.Score.PhoneSolicitation, c.Score.DuplicateRecipients}
	for _, score := range scores {
		if score < 0 {
			return fmt.Errorf("i pesi score non possono essere negativi")
		}
	}
	t := c.Thresholds
	if t.Log < 0 || !(t.Log < t.Block && t.Block < t.Mute5Minutes && t.Mute5Minutes < t.Mute1Hour && t.Mute1Hour < t.Mute24Hours && t.Mute24Hours < t.Review) {
		return fmt.Errorf("le soglie azione devono essere strettamente crescenti")
	}
	if t.TempBanSeconds < 60 {
		return fmt.Errorf("tempBanSeconds deve essere almeno 60")
	}
	for _, levels := range [][]ScoreLevel{c.Duplicate.Levels, c.Recipients.Levels} {
		previous := 0
		for _, level := range levels {
			if level.Count <= previous || level.Score < 0 {
				return fmt.Errorf("livelli score non validi")
			}
			previous = level.Count
		}
	}
	return nil
}

func scoreForCount(count int64, levels []ScoreLevel) int {
	ordered := append([]ScoreLevel(nil), levels...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Count < ordered[j].Count })
	score := 0
	for _, level := range ordered {
		if count >= int64(level.Count) {
			score = level.Score
		}
	}
	return score
}
