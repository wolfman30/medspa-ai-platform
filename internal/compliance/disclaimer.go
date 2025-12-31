package compliance

import (
	"context"
	"fmt"
	"strings"
)

// DisclaimerLevel represents the verbosity of the disclaimer.
type DisclaimerLevel string

const (
	// DisclaimerShort is the shortest disclaimer.
	DisclaimerShort DisclaimerLevel = "short"
	// DisclaimerMedium is a moderate disclaimer.
	DisclaimerMedium DisclaimerLevel = "medium"
	// DisclaimerFull is the most comprehensive disclaimer.
	DisclaimerFull DisclaimerLevel = "full"
)

// Disclaimer templates
const (
	disclaimerShortText = "Auto-assistant. Not medical advice."

	disclaimerMediumText = "This is an automated assistant. For medical advice, please consult your provider."

	disclaimerFullText = "This is an automated scheduling assistant. The information provided is general in nature and not a substitute for professional medical advice. Please consult with a licensed healthcare provider for medical guidance."
)

// DisclaimerConfig configures the disclaimer service.
type DisclaimerConfig struct {
	// Level determines which disclaimer template to use.
	Level DisclaimerLevel
	// Enabled controls whether disclaimers are added.
	Enabled bool
	// FirstMessageOnly adds disclaimer only to first message in conversation.
	FirstMessageOnly bool
	// CustomText overrides the default template.
	CustomText string
}

// DefaultDisclaimerConfig returns sensible defaults.
func DefaultDisclaimerConfig() DisclaimerConfig {
	return DisclaimerConfig{
		Level:            DisclaimerMedium,
		Enabled:          true,
		FirstMessageOnly: false,
	}
}

// DisclaimerService handles adding legal disclaimers to messages.
type DisclaimerService struct {
	audit  *AuditService
	config DisclaimerConfig
}

// NewDisclaimerService creates a new disclaimer service.
func NewDisclaimerService(audit *AuditService, config DisclaimerConfig) *DisclaimerService {
	return &DisclaimerService{
		audit:  audit,
		config: config,
	}
}

// GetDisclaimerText returns the appropriate disclaimer text.
func (s *DisclaimerService) GetDisclaimerText() string {
	if s.config.CustomText != "" {
		return s.config.CustomText
	}

	switch s.config.Level {
	case DisclaimerShort:
		return disclaimerShortText
	case DisclaimerFull:
		return disclaimerFullText
	default:
		return disclaimerMediumText
	}
}

// AddDisclaimer adds a disclaimer to the message if configured.
func (s *DisclaimerService) AddDisclaimer(ctx context.Context, message string, opts DisclaimerOptions) (string, error) {
	if !s.config.Enabled {
		return message, nil
	}

	// Skip if first-message-only mode and this isn't the first message
	if s.config.FirstMessageOnly && !opts.IsFirstMessage {
		return message, nil
	}

	disclaimer := s.GetDisclaimerText()

	// Don't add if already contains disclaimer
	if strings.Contains(message, disclaimer) {
		return message, nil
	}

	// Add disclaimer at the end, separated by newlines
	result := fmt.Sprintf("%s\n\n%s", strings.TrimSpace(message), disclaimer)

	// Log to audit trail if audit service is available
	if s.audit != nil && opts.OrgID != "" {
		_ = s.audit.LogDisclaimerSent(ctx, opts.OrgID, opts.ConversationID, opts.LeadID, string(s.config.Level), disclaimer)
	}

	return result, nil
}

// DisclaimerOptions provides context for disclaimer addition.
type DisclaimerOptions struct {
	OrgID          string
	ConversationID string
	LeadID         string
	IsFirstMessage bool
}

// MustAddDisclaimer is like AddDisclaimer but panics on error.
func (s *DisclaimerService) MustAddDisclaimer(ctx context.Context, message string, opts DisclaimerOptions) string {
	result, err := s.AddDisclaimer(ctx, message, opts)
	if err != nil {
		panic(fmt.Sprintf("compliance: failed to add disclaimer: %v", err))
	}
	return result
}

// ShouldAddDisclaimer checks if a disclaimer should be added based on config.
func (s *DisclaimerService) ShouldAddDisclaimer(isFirstMessage bool) bool {
	if !s.config.Enabled {
		return false
	}
	if s.config.FirstMessageOnly && !isFirstMessage {
		return false
	}
	return true
}
