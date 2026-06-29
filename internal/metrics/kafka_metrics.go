package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	KafkaSendDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "kafka",
		Name:      "send_duration_seconds",
		Help:      "Latency of Kafka Send() calls",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	}, []string{"topic"})

	KafkaSendTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "kafka",
		Name:      "send_total",
		Help:      "Total Kafka sends by status",
	}, []string{"topic", "status"})

	KafkaSendBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "kafka",
		Name:      "send_bytes_total",
		Help:      "Total bytes written to Kafka",
	}, []string{"topic"})

	KafkaConsumerLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "sage",
		Subsystem: "kafka",
		Name:      "consumer_lag",
		Help:      "Consumer lag per partition",
	}, []string{"topic", "partition"})
)
