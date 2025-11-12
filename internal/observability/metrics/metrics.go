package metrics

import "github.com/prometheus/client_golang/prometheus"

// MessagingMetrics exposes counters/histograms for messaging flows.
type MessagingMetrics struct {
	inboundTotal   *prometheus.CounterVec
	outboundTotal  *prometheus.CounterVec
	webhookLatency *prometheus.HistogramVec
}

func NewMessagingMetrics(reg prometheus.Registerer) *MessagingMetrics {
	m := &MessagingMetrics{
		inboundTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "medspa",
			Subsystem: "messaging",
			Name:      "inbound_webhook_total",
			Help:      "Total inbound Telnyx webhooks",
		}, []string{"event_type", "status"}),
		outboundTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "medspa",
			Subsystem: "messaging",
			Name:      "outbound_total",
			Help:      "Total outbound Telnyx sends",
		}, []string{"status", "suppressed"}),
		webhookLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "medspa",
			Subsystem: "messaging",
			Name:      "webhook_latency_seconds",
			Help:      "Latency of Telnyx webhook processing",
			Buckets:   prometheus.DefBuckets,
		}, []string{"event_type"}),
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	reg.MustRegister(m.inboundTotal, m.outboundTotal, m.webhookLatency)
	return m
}

func (m *MessagingMetrics) ObserveInbound(eventType, status string) {
	if m == nil {
		return
	}
	m.inboundTotal.WithLabelValues(eventType, status).Inc()
}

func (m *MessagingMetrics) ObserveOutbound(status string, suppressed bool) {
	if m == nil {
		return
	}
	label := "false"
	if suppressed {
		label = "true"
	}
	m.outboundTotal.WithLabelValues(status, label).Inc()
}

func (m *MessagingMetrics) ObserveWebhookLatency(eventType string, seconds float64) {
	if m == nil {
		return
	}
	m.webhookLatency.WithLabelValues(eventType).Observe(seconds)
}
