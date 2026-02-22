package instagram

import "time"

// WebhookEvent is the top-level structure received from Meta's webhook.
type WebhookEvent struct {
	Object string  `json:"object"`
	Entry  []Entry `json:"entry"`
}

// Entry represents a single entry in the webhook payload.
type Entry struct {
	ID        string      `json:"id"`
	Time      int64       `json:"time"`
	Messaging []Messaging `json:"messaging"`
}

// Messaging represents a single messaging event.
type Messaging struct {
	Sender    Sender    `json:"sender"`
	Recipient Recipient `json:"recipient"`
	Timestamp int64     `json:"timestamp"`
	Message   *Message  `json:"message,omitempty"`
	Postback  *Postback `json:"postback,omitempty"`
}

// Sender identifies who sent the message.
type Sender struct {
	ID string `json:"id"`
}

// Recipient identifies the recipient.
type Recipient struct {
	ID string `json:"id"`
}

// Message contains the message content.
type Message struct {
	MID  string `json:"mid"`
	Text string `json:"text"`
}

// Postback represents a postback event (button tap).
type Postback struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

// SendRequest is the payload sent to the Graph API to send a message.
type SendRequest struct {
	Recipient SendRecipient `json:"recipient"`
	Message   SendMessage   `json:"message"`
}

// SendRecipient identifies who to send the message to.
type SendRecipient struct {
	ID string `json:"id"`
}

// SendMessage is the message content for outbound messages.
type SendMessage struct {
	Text       string      `json:"text,omitempty"`
	Attachment *Attachment `json:"attachment,omitempty"`
}

// Attachment represents a structured message attachment (e.g., button template).
type Attachment struct {
	Type    string  `json:"type"`
	Payload Payload `json:"payload"`
}

// Payload is the attachment payload.
type Payload struct {
	TemplateType string   `json:"template_type"`
	Text         string   `json:"text"`
	Buttons      []Button `json:"buttons"`
}

// Button represents a button in a button template.
type Button struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Payload string `json:"payload,omitempty"`
}

// SendResponse is the response from the Graph API after sending a message.
type SendResponse struct {
	RecipientID string     `json:"recipient_id"`
	MessageID   string     `json:"message_id"`
	Error       *SendError `json:"error,omitempty"`
}

// SendError represents an error returned by the Graph API.
type SendError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      int    `json:"code"`
	FBTraceID string `json:"fbtrace_id"`
}

// ParsedInboundMessage is the normalized result of parsing a webhook event.
type ParsedInboundMessage struct {
	SenderID        string
	RecipientID     string
	Text            string
	Timestamp       time.Time
	IsPostback      bool
	PostbackPayload string
	MessageID       string
}
