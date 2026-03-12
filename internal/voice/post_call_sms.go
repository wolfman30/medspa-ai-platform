package voice

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
)

// PostCallSMSService sends a follow-up SMS after a voice AI call ends.
// Since Nova Sonic tools are disabled (AWS limitation), Lauren tells callers
// "I'll text you a deposit link" during the call but can't actually send it.
// This service fulfills that promise by sending the SMS on hangup.
type PostCallSMSService struct {
	logger      *slog.Logger
	messenger   conversation.ReplyMessenger
	orgResolver messaging.OrgResolver
	clinicStore *clinic.Store
}

// PostCallSMSConfig configures the service.
type PostCallSMSConfig struct {
	Logger      *slog.Logger
	Messenger   conversation.ReplyMessenger
	OrgResolver messaging.OrgResolver
	ClinicStore *clinic.Store
}

// NewPostCallSMSService creates a new post-call SMS service.
func NewPostCallSMSService(cfg PostCallSMSConfig) *PostCallSMSService {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &PostCallSMSService{
		logger:      cfg.Logger,
		messenger:   cfg.Messenger,
		orgResolver: cfg.OrgResolver,
		clinicStore: cfg.ClinicStore,
	}
}

// SendPostCallSMS sends a deposit/booking follow-up SMS to the caller.
func (s *PostCallSMSService) SendPostCallSMS(ctx context.Context, callerPhone, clinicPhone string) error {
	if s.messenger == nil {
		s.logger.Warn("post-call-sms: no messenger configured")
		return nil
	}

	// Resolve org from clinic phone
	orgID := ""
	if s.orgResolver != nil {
		resolved, err := s.orgResolver.ResolveOrgID(ctx, clinicPhone)
		if err != nil {
			s.logger.Warn("post-call-sms: could not resolve org", "clinic_phone", clinicPhone, "error", err)
		} else {
			orgID = resolved
		}
	}

	// Get clinic config for name and deposit info
	clinicName := "our office"
	depositAmount := 50 // default
	smsFrom := clinicPhone

	if s.clinicStore != nil && orgID != "" {
		cfg, err := s.clinicStore.Get(ctx, orgID)
		if err == nil && cfg != nil {
			if cfg.Name != "" {
				clinicName = cfg.Name
			}
			if cfg.DepositAmountCents > 0 {
				depositAmount = cfg.DepositAmountCents / 100
			}
			if smsFrom == "" {
				if cfg.SMSPhoneNumber != "" {
					smsFrom = cfg.SMSPhoneNumber
				} else if cfg.Phone != "" {
					smsFrom = cfg.Phone
				}
			}
		}
	}

	// Build the follow-up message
	msg := fmt.Sprintf(
		"Hi! This is Lauren from %s 😊 Thanks for calling! "+
			"To confirm your appointment, please complete the $%d deposit here: "+
			"https://app.aiwolfsolutions.com/deposit/%s "+
			"If you have any questions, just reply to this text!",
		clinicName, depositAmount, orgID,
	)

	err := s.messenger.SendReply(ctx, conversation.OutboundReply{
		OrgID: orgID,
		To:    callerPhone,
		From:  smsFrom,
		Body:  msg,
		Metadata: map[string]string{
			"source": "voice_ai_post_call",
		},
	})
	if err != nil {
		return fmt.Errorf("send post-call SMS: %w", err)
	}

	s.logger.Info("post-call-sms: sent",
		"caller", callerPhone,
		"clinic", clinicName,
		"org_id", orgID,
	)
	return nil
}
