package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RiskAnalysisTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "risk",
		Name:      "analysis_total",
		Help:      "Total operations analyzed by the risk interceptor",
	}, []string{"operation", "table", "risk_level"})

	RiskAnalysisBlockedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "risk",
		Name:      "analysis_blocked_total",
		Help:      "Operations blocked by the risk interceptor before reaching DynamoDB",
	}, []string{"operation", "table", "risk_level"})

	RiskAnalysisConfirmedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "risk",
		Name:      "analysis_confirmed_total",
		Help:      "Operations that were initially blocked but user provided confirmation",
	}, []string{"operation"})

	RiskAnalysisDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "risk",
		Name:      "analysis_duration_seconds",
		Help:      "Latency of the risk analysis call including describe-table",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"operation"})

	RiskPIIDetectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "risk",
		Name:      "pii_detected_total",
		Help:      "Count of requests where PII was detected in payloads",
	}, []string{"operation", "table"})
)
