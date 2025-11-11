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

// Response is a simple DTO returned to the API layer.
type Response struct {
	ConversationID string
	Message        string
	Timestamp      time.Time
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
