package conversation

import (
	"context"
	"fmt"
	"time"
)

// Service describes how the conversation engine should behave.
type Service interface {
	StartConversation(ctx context.Context, req StartRequest) (*Response, error)
	ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error)
	GetHistory(ctx context.Context, conversationID string) ([]Message, error)
}

// Message represents a single message in a conversation transcript.
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Channel identifies which transport the conversation is happening on.
type Channel string

const (
	ChannelUnknown Channel = ""
	ChannelSMS     Channel = "sms"
)

// StartRequest represents the minimal data we need to open a conversation.
type StartRequest struct {
	OrgID          string
	LeadID         string
	Intro          string
	Source         string
	ClinicID       string
	Channel        Channel
	From           string
	To             string
	ConversationID string
	Metadata       map[string]string
	Silent         bool
	AckMessage     string // The ack message sent to the user (for Silent=true flows)
}

// MessageRequest represents a single turn in the conversation.
type MessageRequest struct {
	OrgID          string
	LeadID         string
	ConversationID string
	Message        string
	ClinicID       string
	Channel        Channel
	From           string
	To             string
	Metadata       map[string]string
}

// Deposit intent for payment processing.
type DepositIntent struct {
	AmountCents  int32
	SuccessURL   string
	CancelURL    string
	Description  string
	ScheduledFor *time.Time
	// Preloaded checkout info (set by deposit preloader for parallel generation)
	PreloadedURL       string // Pre-generated Square checkout URL
	PreloadedPaymentID string // Pre-generated payment ID to use for intent (UUID string)
}

// Response is a simple DTO returned to the API layer.
type Response struct {
	ConversationID string
	Message        string
	Timestamp      time.Time
	DepositIntent  *DepositIntent
}

// StubService is a placeholder implementation used until the real engine is ready.
type StubService struct{}

// NewStubService returns the stub implementation.
func NewStubService() *StubService {
	return &StubService{}
}

// StartConversation returns a canned greeting plus a generated conversation ID.
func (s *StubService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	id := req.ConversationID
	if id == "" {
		id = fmt.Sprintf("conv_%s_%d", req.LeadID, time.Now().UnixNano())
	}
	if req.Silent {
		return &Response{
			ConversationID: id,
			Message:        "",
			Timestamp:      time.Now().UTC(),
		}, nil
	}
	return &Response{
		ConversationID: id,
		Message:        "dY`< Thanks for reaching out! I'm your MedSpa concierge. How can I help?",
		Timestamp:      time.Now().UTC(),
	}, nil
}

// ProcessMessage echoes back the user's message for now.
func (s *StubService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	return &Response{
		ConversationID: req.ConversationID,
		Message:        fmt.Sprintf("You said: %s. A full AI response will arrive soon.", req.Message),
		Timestamp:      time.Now().UTC(),
	}, nil
}

// GetHistory returns empty history for stub service.
func (s *StubService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	return []Message{}, nil
}
