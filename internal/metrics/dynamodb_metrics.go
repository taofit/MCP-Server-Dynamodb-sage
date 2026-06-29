package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DynamoDBOperationDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "dynamodb",
		Name:      "operation_duration_seconds",
		Help:      "Latency of DynamoDB SDK calls",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
	}, []string{"operation", "table", "status"})

	DynamoDBOperationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "dynamodb",
		Name:      "operation_total",
		Help:      "Total DynamoDB calls by status",
	}, []string{"operation", "table", "status"})

	DynamoDBConsumedCapacityTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "dynamodb",
		Name:      "consumed_capacity_total",
		Help:      "Rolling RCU/WCU consumption",
	}, []string{"operation", "table", "capacity_type"})
)
