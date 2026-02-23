package conversation

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
)

var llmTracer = otel.Tracer("medspa.internal.conversation.llm")

var llmLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "llm_latency_seconds",
		Help:      "Latency of LLM completions",
		// Focus on sub-10s buckets with a few higher ones for visibility.
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 15, 20, 30},
	},
	[]string{"model", "status"},
)

var llmTokensTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "llm_tokens_total",
		Help:      "Tokens used by the LLM",
	},
	[]string{"model", "type"}, // type: input, output, total
)

var depositDecisionTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "deposit_decision_total",
		Help:      "Counts LLM-based deposit decisions by outcome",
	},
	[]string{"model", "outcome"}, // outcome: collect, skip, error
)

func init() {
	prometheus.MustRegister(llmLatency)
	prometheus.MustRegister(llmTokensTotal)
	prometheus.MustRegister(depositDecisionTotal)
}

// RegisterMetrics registers conversation metrics with a custom registry.
// Use this when exposing a non-default registry (e.g., HTTP handlers with a private registry).
func RegisterMetrics(reg prometheus.Registerer) {
	if reg == nil || reg == prometheus.DefaultRegisterer {
		return
	}
	reg.MustRegister(llmLatency, llmTokensTotal, depositDecisionTotal)
}
