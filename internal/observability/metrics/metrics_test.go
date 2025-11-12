package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMessagingMetricsObserve(t *testing.T) {
	m := NewMessagingMetrics(nil)
	m.ObserveInbound("message.received", "delivered")
	m.ObserveOutbound("queued", false)
	m.ObserveWebhookLatency("message.received", 0.5)
}

func TestMessagingMetricsCustomRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMessagingMetrics(reg)
	m.ObserveOutbound("queued", true)
}

func TestMessagingMetricsNilSafe(t *testing.T) {
	var m *MessagingMetrics
	m.ObserveInbound("event", "status")
	m.ObserveOutbound("queued", false)
	m.ObserveWebhookLatency("event", 0.1)
}
