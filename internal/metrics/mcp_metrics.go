// Package metrics provides Prometheus metrics for monitoring MCP (Model Context Protocol) tool operations.
// It includes counters for tracking tool invocations and errors, as well as histograms for measuring
// tool execution duration.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MCPToolInvocationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "mcp",
		Name:      "tool_invocations_total",
		Help:      "Total MCP tool invocations",
	}, []string{"tool", "transport"})

	MCPToolErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sage",
		Subsystem: "mcp",
		Name:      "tool_errors_total",
		Help:      "MCP tool errors by type",
	}, []string{"tool", "error_type"})

	MCPToolDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sage",
		Subsystem: "mcp",
		Name:      "tool_duration_seconds",
		Help:      "Total handler latency including risk analysis",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
	}, []string{"tool", "type"})
)
