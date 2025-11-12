package handlers

import "context"

import "github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"

type telnyxClient interface {
	CheckHostedEligibility(ctx context.Context, number string) (*telnyxclient.HostedEligibilityResponse, error)
	CreateHostedOrder(ctx context.Context, req telnyxclient.HostedOrderRequest) (*telnyxclient.HostedOrder, error)
	CreateBrand(ctx context.Context, req telnyxclient.BrandRequest) (*telnyxclient.Brand, error)
	CreateCampaign(ctx context.Context, req telnyxclient.CampaignRequest) (*telnyxclient.Campaign, error)
	SendMessage(ctx context.Context, req telnyxclient.SendMessageRequest) (*telnyxclient.MessageResponse, error)
	VerifyWebhookSignature(timestamp, signature string, payload []byte) error
	GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error)
}
