package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AuditLogWriteDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "audit",
		Name:      "log_write_duration_seconds",
		Help:      "Latency of SQLite audit log writes",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
	}, []string{})

	AuditLogBufferDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sage",
		Subsystem: "audit",
		Name:      "buffer_depth",
		Help:      "Current depth of the buffered audit log channel",
	})
)
