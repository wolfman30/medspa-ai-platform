package conversation

import (
	"context"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// DepositConfig allows callers to configure defaults used when the LLM signals a deposit.
type DepositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

type LLMOption func(*LLMService)

// WithDepositConfig sets the defaults applied to LLM-produced deposit intents.
func WithDepositConfig(cfg DepositConfig) LLMOption {
	return func(s *LLMService) {
		s.deposit = depositConfig(cfg)
	}
}

// WithEMR configures an EMR adapter for real-time availability lookup.
func WithEMR(emr *EMRAdapter) LLMOption {
	return func(s *LLMService) {
		s.emr = emr
	}
}

// WithMoxieClient configures the direct Moxie GraphQL API client for availability queries.
func WithMoxieClient(client *moxieclient.Client) LLMOption {
	return func(s *LLMService) {
		s.moxieClient = client
	}
}

// WithLeadsRepo configures the leads repository for saving scheduling preferences.
func WithLeadsRepo(repo leads.Repository) LLMOption {
	return func(s *LLMService) {
		s.leadsRepo = repo
	}
}

// WithClinicStore configures the clinic config store for business hours awareness.
func WithClinicStore(store *clinic.Store) LLMOption {
	return func(s *LLMService) {
		s.clinicStore = store
	}
}

// WithAuditService configures compliance audit logging.
func WithAuditService(audit *compliance.AuditService) LLMOption {
	return func(s *LLMService) {
		s.audit = audit
	}
}

// PaymentStatusChecker checks if a lead has an open or completed deposit.
type PaymentStatusChecker interface {
	HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error)
}

// WithPaymentChecker configures payment status checking for context injection.
func WithPaymentChecker(checker PaymentStatusChecker) LLMOption {
	return func(s *LLMService) {
		s.paymentChecker = checker
	}
}

// WithAPIBaseURL sets the public API base URL (used for building callback URLs).
func WithAPIBaseURL(url string) LLMOption {
	return func(s *LLMService) {
		s.apiBaseURL = url
	}
}

// WithVoiceModel sets a separate (faster) model for voice conversations.
func WithVoiceModel(model string) LLMOption {
	return func(s *LLMService) {
		s.voiceModel = model
	}
}

// WithAvailabilityPrefetcher enables background availability pre-fetching.
func WithAvailabilityPrefetcher(p *AvailabilityPrefetcher) LLMOption {
	return func(s *LLMService) {
		s.prefetcher = p
	}
}

type depositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}
