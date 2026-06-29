package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AsyncJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "async",
		Name:      "jobs_total",
		Help:      "Total async jobs by final status",
	}, []string{"operation", "status"})

	AsyncJobDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "async",
		Name:      "job_duration_seconds",
		Help:      "End-to-end async job processing time",
		Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"operation", "status"})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sage",
		Subsystem: "queue",
		Name:      "depth",
		Help:      "Current depth of the in-process fallback queue",
	})

	JobStoragePending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sage",
		Subsystem: "job_storage",
		Name:      "pending",
		Help:      "Number of pending JobResult entries not yet polled by client",
	})
)
