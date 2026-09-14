package moderation

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	CheckDuration         = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "moderation_check_duration_seconds", Help: "Moderation check latency"}, []string{"action"})
	RedisDuration         = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "moderation_redis_duration_seconds", Help: "Moderation Redis latency"})
	NormalizationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "moderation_normalization_duration_seconds", Help: "Moderation normalization latency"})
	RulesDuration         = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "moderation_rules_duration_seconds", Help: "Moderation rules latency"})
)

func Collectors() []prometheus.Collector {
	return []prometheus.Collector{CheckDuration, RedisDuration, NormalizationDuration, RulesDuration}
}

func observeCheck(duration time.Duration, action Action) {
	CheckDuration.WithLabelValues(string(action)).Observe(duration.Seconds())
}
func observeRedis(duration time.Duration)         { RedisDuration.Observe(duration.Seconds()) }
func observeNormalization(duration time.Duration) { NormalizationDuration.Observe(duration.Seconds()) }
func observeRules(duration time.Duration)         { RulesDuration.Observe(duration.Seconds()) }
