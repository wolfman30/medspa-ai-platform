package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
)

type testTelnyxClient struct {
	sendResp     *telnyxclient.MessageResponse
	lastSendReq  *telnyxclient.SendMessageRequest
	verifyCalled bool
	sendErr      error
	checkErr     error
	orderErr     error
	brandErr     error
	campaignErr  error
	verifyErr    error
}

func (s *testTelnyxClient) CheckHostedEligibility(ctx context.Context, number string) (*telnyxclient.HostedEligibilityResponse, error) {
	if s.checkErr != nil {
		return nil, s.checkErr
	}
	return &telnyxclient.HostedEligibilityResponse{PhoneNumber: number, Eligible: true}, nil
}

func (s *testTelnyxClient) CreateHostedOrder(ctx context.Context, req telnyxclient.HostedOrderRequest) (*telnyxclient.HostedOrder, error) {
	if s.orderErr != nil {
		return nil, s.orderErr
	}
	return &telnyxclient.HostedOrder{
		ID:          "hno_test",
		Status:      "pending",
		ClinicID:    req.ClinicID,
		PhoneNumber: req.PhoneNumber,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *testTelnyxClient) CreateBrand(ctx context.Context, req telnyxclient.BrandRequest) (*telnyxclient.Brand, error) {
	if s.brandErr != nil {
		return nil, s.brandErr
	}
	return &telnyxclient.Brand{ID: "brand_internal", BrandID: "B123", Status: "approved", ClinicID: req.ClinicID}, nil
}

func (s *testTelnyxClient) CreateCampaign(ctx context.Context, req telnyxclient.CampaignRequest) (*telnyxclient.Campaign, error) {
	if s.campaignErr != nil {
		return nil, s.campaignErr
	}
	return &telnyxclient.Campaign{ID: "campaign_internal", CampaignID: "C123", BrandID: req.BrandID, Status: "active", UseCase: req.UseCase}, nil
}

func (s *testTelnyxClient) SendMessage(ctx context.Context, req telnyxclient.SendMessageRequest) (*telnyxclient.MessageResponse, error) {
	s.lastSendReq = &req
	if s.sendErr != nil {
		return nil, s.sendErr
	}
	if s.sendResp != nil {
		return s.sendResp, nil
	}
	return &telnyxclient.MessageResponse{ID: "msg_test", Status: "queued", From: req.From, To: req.To}, nil
}

func (s *testTelnyxClient) VerifyWebhookSignature(timestamp, signature string, payload []byte) error {
	s.verifyCalled = true
	if s.verifyErr != nil {
		return s.verifyErr
	}
	return nil
}

func (s *testTelnyxClient) GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error) {
	return &telnyxclient.HostedOrder{ID: orderID, Status: "pending", PhoneNumber: "+1555"}, nil
}

type stubProcessedTracker struct {
	seen map[string]bool
}

func (s *stubProcessedTracker) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	return s.seen[eventID], nil
}

func (s *stubProcessedTracker) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[eventID] = true
	return true, nil
}

type recordedMessage struct {
	ClinicID uuid.UUID
	From     string
	To       string
}

type stubConversationPublisher struct {
	calls   int
	lastJob string
	last    conversation.MessageRequest
}

func (s *stubConversationPublisher) EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error {
	s.calls++
	s.lastJob = jobID
	s.last = req
	return nil
}

type stubLeadsRepo struct {
	called bool
	lead   *leads.Lead
	err    error
}

func (s *stubLeadsRepo) Create(context.Context, *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadsRepo) GetByID(context.Context, string, string) (*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadsRepo) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*leads.Lead, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	if s.lead != nil {
		return s.lead, nil
	}
	return &leads.Lead{ID: "lead-stub", OrgID: orgID, Phone: phone, Source: source}, nil
}
