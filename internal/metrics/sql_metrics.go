package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DBWriteDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "sql",
		Name:      "write_duration_seconds",
		Help:      "Latency of SQLite writes",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
	})
)
