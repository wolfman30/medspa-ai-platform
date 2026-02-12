package archive

import "time"

// ConversationRecord is the top-level structure archived to S3 for LLM training.
type ConversationRecord struct {
	Version         string              `json:"version"` // "1.0"
	ConversationID  string              `json:"conversation_id"`
	OrgID           string              `json:"org_id"`
	PhoneHash       string              `json:"phone_hash"` // sha256 of phone
	ArchivedAt      time.Time           `json:"archived_at"`
	DurationSeconds int                 `json:"duration_seconds"`
	MessageCount    int                 `json:"message_count"`
	Outcome         string              `json:"outcome"`
	Labels          Labels              `json:"labels"`
	Context         ConversationContext `json:"context"`
	Messages        []Message           `json:"messages"`
}

// Labels holds auto-classification results for training data curation.
type Labels struct {
	MedicalLiabilityRisk    string `json:"medical_liability_risk"` // none|low|medium|high
	PromptInjectionDetected bool   `json:"prompt_injection_detected"`
	PromptInjectionType     string `json:"prompt_injection_type"` // none|jailbreak|data_exfil|role_override|social_engineering
	ConversationCategory    string `json:"conversation_category"` // normal_booking|medical_inquiry|off_label_request|prompt_injection|social_engineering|abusive_hostile|abandoned|unqualified|escalation|test_internal
	Sentiment               string `json:"sentiment"`             // positive|neutral|negative|hostile
	ContainsPHI             bool   `json:"contains_phi"`
	AutoLabeled             bool   `json:"auto_labeled"`
	LabelModel              string `json:"label_model"`
	HumanReviewed           bool   `json:"human_reviewed"`
}

// ConversationContext captures booking/payment context for training.
type ConversationContext struct {
	ServiceRequested   string `json:"service_requested,omitempty"`
	PatientType        string `json:"patient_type,omitempty"`
	BookingCompleted   bool   `json:"booking_completed"`
	PaymentCompleted   bool   `json:"payment_completed"`
	DepositAmountCents int    `json:"deposit_amount_cents,omitempty"`
}

// Message is a single conversation turn.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ManifestEntry is one JSONL line in the monthly manifest file.
type ManifestEntry struct {
	ConversationID    string `json:"conversation_id"`
	S3Key             string `json:"s3_key"`
	Category          string `json:"category"`
	MedicalRisk       string `json:"medical_risk"`
	InjectionDetected bool   `json:"injection_detected"`
	ArchivedAt        string `json:"archived_at"`
	MessageCount      int    `json:"message_count"`
	Outcome           string `json:"outcome"`
}
