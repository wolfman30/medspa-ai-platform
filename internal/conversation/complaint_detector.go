package conversation

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var complaintTracer = otel.Tracer("medspa/complaint-detector")

// ComplaintType represents the type of billing complaint detected.
type ComplaintType string

const (
	ComplaintNone         ComplaintType = ""
	ComplaintOvercharge   ComplaintType = "OVERCHARGE"
	ComplaintUnauthorized ComplaintType = "UNAUTHORIZED"
	ComplaintRefundReq    ComplaintType = "REFUND_REQUEST"
	ComplaintDoubleCharge ComplaintType = "DOUBLE_CHARGE"
	ComplaintGeneral      ComplaintType = "GENERAL_BILLING"
)

// ComplaintResult contains the result of complaint detection.
type ComplaintResult struct {
	Detected       bool
	Type           ComplaintType
	Confidence     float64
	MatchedKeyword string
	SuggestedReply string
}

// ComplaintDetector detects billing complaints in customer messages.
type ComplaintDetector struct {
	logger   *logging.Logger
	patterns map[ComplaintType][]*complaintPattern
}

type complaintPattern struct {
	regex   *regexp.Regexp
	weight  float64
	keyword string
}

// NewComplaintDetector creates a new complaint detector.
func NewComplaintDetector(logger *logging.Logger) *ComplaintDetector {
	if logger == nil {
		logger = logging.Default()
	}

	d := &ComplaintDetector{
		logger:   logger,
		patterns: make(map[ComplaintType][]*complaintPattern),
	}

	// Overcharge patterns
	d.patterns[ComplaintOvercharge] = []*complaintPattern{
		{regex: regexp.MustCompile(`(?i)\b(overcharged?|over\s*charged?)\b`), weight: 0.9, keyword: "overcharged"},
		{regex: regexp.MustCompile(`(?i)\bcharged?\s+(me\s+)?(too\s+much|more\s+than)\b`), weight: 0.85, keyword: "charged too much"},
		{regex: regexp.MustCompile(`(?i)\bwrong\s+(amount|charge|price)\b`), weight: 0.8, keyword: "wrong amount"},
		{regex: regexp.MustCompile(`(?i)\bincorrect\s+(charge|amount|bill)\b`), weight: 0.8, keyword: "incorrect charge"},
		{regex: regexp.MustCompile(`(?i)\bshould(n't|nt| not)\s+(be|have been)\s+\$?\d+\b`), weight: 0.7, keyword: "amount dispute"},
		{regex: regexp.MustCompile(`(?i)\bonly\s+(supposed|meant)\s+to\s+(be|pay)\b`), weight: 0.7, keyword: "expected amount"},
	}

	// Unauthorized charge patterns
	d.patterns[ComplaintUnauthorized] = []*complaintPattern{
		{regex: regexp.MustCompile(`(?i)\b(didn'?t|did\s*not|never)\s+(authorize|approve|agree)\b`), weight: 0.95, keyword: "didn't authorize"},
		{regex: regexp.MustCompile(`(?i)\bunauthorized\s+(charge|transaction|payment)\b`), weight: 0.95, keyword: "unauthorized charge"},
		{regex: regexp.MustCompile(`(?i)\b(fraud|fraudulent)\b`), weight: 0.9, keyword: "fraud"},
		{regex: regexp.MustCompile(`(?i)\bstole\s+(my\s+)?(money|card)\b`), weight: 0.9, keyword: "stolen"},
		{regex: regexp.MustCompile(`(?i)\bwithout\s+(my\s+)?(permission|consent)\b`), weight: 0.85, keyword: "without permission"},
		{regex: regexp.MustCompile(`(?i)\b(scam|scammed)\b`), weight: 0.85, keyword: "scam"},
	}

	// Refund request patterns
	d.patterns[ComplaintRefundReq] = []*complaintPattern{
		{regex: regexp.MustCompile(`(?i)\b(want|need|get)\s+(a\s+|my\s+)?refund\b`), weight: 0.9, keyword: "want refund"},
		{regex: regexp.MustCompile(`(?i)\b(money|deposit)\s+back\b`), weight: 0.85, keyword: "money back"},
		{regex: regexp.MustCompile(`(?i)\bcancel\s+(the\s+)?(charge|payment|deposit)\b`), weight: 0.8, keyword: "cancel charge"},
		{regex: regexp.MustCompile(`(?i)\breverse\s+(the\s+)?(charge|payment|transaction)\b`), weight: 0.85, keyword: "reverse charge"},
		{regex: regexp.MustCompile(`(?i)\bgive\s+(me\s+)?my\s+money\b`), weight: 0.8, keyword: "give money back"},
		{regex: regexp.MustCompile(`(?i)\brefund\s+(me|my|the)\b`), weight: 0.85, keyword: "refund request"},
	}

	// Double charge patterns
	d.patterns[ComplaintDoubleCharge] = []*complaintPattern{
		{regex: regexp.MustCompile(`(?i)\bcharged?\s+(me\s+)?(twice|two\s*times|2\s*times)\b`), weight: 0.95, keyword: "charged twice"},
		{regex: regexp.MustCompile(`(?i)\b(duplicate|double)\s+(charge|payment|transaction)\b`), weight: 0.95, keyword: "duplicate charge"},
		{regex: regexp.MustCompile(`(?i)\btwo\s+(charges?|payments?)\b`), weight: 0.8, keyword: "two charges"},
		{regex: regexp.MustCompile(`(?i)\bsee\s+(it\s+)?twice\b`), weight: 0.7, keyword: "see twice"},
		{regex: regexp.MustCompile(`(?i)\bmultiple\s+(charges?|times)\b`), weight: 0.75, keyword: "multiple charges"},
	}

	// General billing complaint patterns
	d.patterns[ComplaintGeneral] = []*complaintPattern{
		{regex: regexp.MustCompile(`(?i)\b(billing|payment)\s+(issue|problem|question)\b`), weight: 0.6, keyword: "billing issue"},
		{regex: regexp.MustCompile(`(?i)\bcharge\s+(doesn'?t|does\s*not)\s+(look|seem)\s+right\b`), weight: 0.7, keyword: "charge looks wrong"},
		{regex: regexp.MustCompile(`(?i)\bwhat('?s| is)\s+this\s+charge\b`), weight: 0.5, keyword: "what is this charge"},
		{regex: regexp.MustCompile(`(?i)\bexplain\s+(the\s+|this\s+)?charge\b`), weight: 0.4, keyword: "explain charge"},
		{regex: regexp.MustCompile(`(?i)\b(upset|angry|frustrated)\s+(about|with)\s+(the\s+)?(charge|payment|bill)\b`), weight: 0.7, keyword: "upset about charge"},
	}

	return d
}

// DetectComplaint analyzes a message for billing complaints.
func (d *ComplaintDetector) DetectComplaint(ctx context.Context, message string) *ComplaintResult {
	ctx, span := complaintTracer.Start(ctx, "complaint.detect")
	defer span.End()

	message = strings.TrimSpace(message)
	if message == "" {
		return &ComplaintResult{Detected: false}
	}

	var bestResult *ComplaintResult

	// Check each complaint type
	for complaintType, patterns := range d.patterns {
		for _, p := range patterns {
			if p.regex.MatchString(message) {
				if bestResult == nil || p.weight > bestResult.Confidence {
					bestResult = &ComplaintResult{
						Detected:       true,
						Type:           complaintType,
						Confidence:     p.weight,
						MatchedKeyword: p.keyword,
					}
				}
			}
		}
	}

	if bestResult == nil {
		return &ComplaintResult{Detected: false}
	}

	// Set suggested reply based on complaint type
	bestResult.SuggestedReply = d.getSuggestedReply(bestResult.Type)

	span.SetAttributes(
		attribute.Bool("complaint.detected", true),
		attribute.String("complaint.type", string(bestResult.Type)),
		attribute.Float64("complaint.confidence", bestResult.Confidence),
		attribute.String("complaint.keyword", bestResult.MatchedKeyword),
	)

	d.logger.Info("billing complaint detected",
		"type", bestResult.Type,
		"confidence", bestResult.Confidence,
		"keyword", bestResult.MatchedKeyword,
	)

	return bestResult
}

// getSuggestedReply returns an empathetic response template for the complaint type.
func (d *ComplaintDetector) getSuggestedReply(complaintType ComplaintType) string {
	switch complaintType {
	case ComplaintOvercharge:
		return "I understand your concern about the charge amount. I've flagged this for our billing team who will review it and contact you shortly. They'll make sure any overcharge is corrected. Is there anything else I can help you with?"

	case ComplaintUnauthorized:
		return "I take this very seriously. I've immediately escalated this to our billing team for urgent review. They will investigate and contact you as soon as possible. Please don't hesitate to contact your bank if you have additional concerns."

	case ComplaintRefundReq:
		return "I understand you'd like a refund. I've forwarded your request to our billing team who will review it according to our refund policy and get back to you shortly. Is there anything else I can help you with?"

	case ComplaintDoubleCharge:
		return "I apologize for any duplicate charge. I've flagged this for immediate review by our billing team. They will verify the charges and ensure any duplicate is corrected. You should hear back from them shortly."

	case ComplaintGeneral:
		return "I understand you have a question about your billing. I've forwarded this to our billing team who can provide you with detailed information. They'll be in touch shortly. Is there anything else I can help you with?"

	default:
		return "I understand your concern about the charge. I've flagged this for our billing team who will review and contact you shortly. Is there anything else I can help you with?"
	}
}

// IsHighPriority returns true if the complaint should be escalated immediately.
func (d *ComplaintDetector) IsHighPriority(result *ComplaintResult) bool {
	if !result.Detected {
		return false
	}

	// Unauthorized charges and high-confidence complaints are high priority
	switch result.Type {
	case ComplaintUnauthorized:
		return true
	case ComplaintDoubleCharge:
		return result.Confidence >= 0.8
	case ComplaintOvercharge:
		return result.Confidence >= 0.85
	default:
		return result.Confidence >= 0.9
	}
}

// GetEscalationPriority returns the escalation priority level.
func (d *ComplaintDetector) GetEscalationPriority(result *ComplaintResult) string {
	if !result.Detected {
		return "NONE"
	}

	switch result.Type {
	case ComplaintUnauthorized:
		return "HIGH"
	case ComplaintDoubleCharge, ComplaintOvercharge:
		if result.Confidence >= 0.8 {
			return "HIGH"
		}
		return "MEDIUM"
	case ComplaintRefundReq:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
