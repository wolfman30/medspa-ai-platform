package conversation

import "context"

// ReplyMessenger delivers AI replies back to the end user (e.g. via SMS).
type ReplyMessenger interface {
	SendReply(ctx context.Context, reply OutboundReply) error
}

// OutboundReply carries the data required to push a message to the user.
type OutboundReply struct {
	OrgID          string
	LeadID         string
	ConversationID string
	To             string
	From           string
	Body           string
	Metadata       map[string]string
}

