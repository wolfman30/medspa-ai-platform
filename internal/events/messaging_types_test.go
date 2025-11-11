package events

import "testing"

func TestMessagingEventTypes(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"received", MessageReceivedV1{}.EventType(), "messaging.message.received.v1"},
		{"sent", MessageSentV1{}.EventType(), "messaging.message.sent.v1"},
		{"hosted", HostedOrderActivatedV1{}.EventType(), "messaging.hosted_order.activated.v1"},
		{"brand", BrandCreatedV1{}.EventType(), "messaging.ten_dlc.brand.created.v1"},
		{"campaign", CampaignApprovedV1{}.EventType(), "messaging.ten_dlc.campaign.approved.v1"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s event type mismatch: got %s want %s", tt.name, tt.got, tt.want)
		}
	}
}
