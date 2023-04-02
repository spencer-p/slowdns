package main

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	requestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      "request_latency",
			Subsystem: "slowdns",
			Help:      "Latency of DNS requests in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.1, 0.2, 0.4, 0.8, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0},
		},
		[]string{
			"is_error",
			"block_level",
		},
	)
	requestOverheadLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      "request_overhead_latency",
			Subsystem: "slowdns",
			Help:      "Overhead latency imposed by routing system",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.1, 0.2, 0.4, 0.8, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0},
		},
		[]string{
			"is_error",
			"block_level",
		},
	)
	healthCheckCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "health_requests",
			Subsystem: "slowdns",
			Help:      "Results of requests to /health",
		},
		[]string{
			"ok",
		},
	)
)

func init() {
	prometheus.MustRegister(
		requestLatency,
		requestOverheadLatency,
		healthCheckCounter,
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

func ObserveLatencyOverhead(block_level string, is_error bool, latency time.Duration) {
	is_error_label := "false"
	if is_error {
		is_error_label = "true"
	}
	requestOverheadLatency.With(prometheus.Labels{
		"is_error":    is_error_label,
		"block_level": block_level,
	}).Observe(latency.Seconds())
}

func ObserveHealthCheck(success bool) {
	healthCheckCounter.With(prometheus.Labels{
		"ok": fmt.Sprintf("%t", success),
	}).Inc()
}
