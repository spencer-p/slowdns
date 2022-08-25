package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	requestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      "request_latency",
			Subsystem: "slowdns",
			Help:      "Latency of DNS requests in seconds",
			Buckets:   []float64{0.001, 0.01, 0.1, 0.2, 0.4, 0.8, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0},
		},
		[]string{
			"is_error",
			"block_level",
		},
	)
)

func init() {
	prometheus.MustRegister(
		requestLatency,
	)
}

func ObserveRequestLatency(block_level string, is_error bool, latency time.Duration) {
	is_error_label := "false"
	if is_error {
		is_error_label = "true"
	}
	requestLatency.With(prometheus.Labels{
		"is_error":    is_error_label,
		"block_level": block_level,
	}).Observe(latency.Seconds())
}
