package conversation

import (
	"regexp"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

var (
	priceInquiryRE = regexp.MustCompile(`(?i)\b(?:how much|price|pricing|cost|rate|rates|charge)\b`)
	phiPrefaceRE   = regexp.MustCompile(`(?i)\b(?:diagnosed|diagnosis|my condition|my symptoms|i have|i've had|i am|i'm)\b`)
	// PHI keywords with word boundaries to avoid false positives (e.g., "sti" matching in "existing")
	phiKeywordsRE = regexp.MustCompile(`(?i)\b(?:diabetes|hiv|aids|cancer|hepatitis|pregnant|pregnancy|depression|anxiety|bipolar|schizophrenia|asthma|hypertension|blood pressure|infection|herpes|std|sti)\b`)
	// Strong medical advice cues — always trigger with any medical/service context
	strongMedicalCueRE = regexp.MustCompile(`(?i)\b(?:is it safe|safe to|ok to|okay to|contraindications?|side effects?|dosage|dose|mg|milligram|interactions?|mix with|stop taking)\b`)
	// Weak medical advice cues — only trigger with medical-specific context (not service names alone)
	weakMedicalCueRE = regexp.MustCompile(`(?i)\b(?:should i|can i)\b`)
	// Full medical context (services + medical terms) — used with strong cues
	medicalContextRE = regexp.MustCompile(`(?i)\b(?:botox|filler|laser|microneedling|facial|peel|dermaplaning|prp|injectable|medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
	// Medical-specific context (conditions/medications only, no service names) — used with weak cues
	medicalSpecificContextRE = regexp.MustCompile(`(?i)\b(?:medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
)

// isPriceInquiry returns true if the message asks about pricing or costs.
func isPriceInquiry(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	return priceInquiryRE.MatchString(message) || strings.Contains(message, "$")
}

// isAmbiguousHelp returns true if the message is a vague help/question request
// without specific booking or service context.
func isAmbiguousHelp(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !(strings.Contains(message, "help") || strings.Contains(message, "question") || strings.Contains(message, "info")) {
		return false
	}
	// If the user already mentioned booking or a service, let the LLM handle it.
	// "available" indicates booking intent (e.g., "do you have anything available Thursday?")
	for _, kw := range []string{"book", "appointment", "schedule", "available", "opening", "botox", "filler", "facial", "laser", "peel", "microneedling", "hydrafacial"} {
		if strings.Contains(message, kw) {
			return false
		}
	}
	return true
}

// isQuestionSelection returns true if the message is a simple statement indicating
// the user has a question (e.g., "I have a question") without actual content.
func isQuestionSelection(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	message = strings.Trim(message, ".!?")
	message = strings.Join(strings.Fields(message), " ")
	if strings.Contains(message, "?") {
		return false
	}

	for _, kw := range []string{"book", "appointment", "schedule", "botox", "filler", "facial", "laser", "peel", "microneedling"} {
		if strings.Contains(message, kw) {
			return false
		}
	}

	switch message {
	case "question",
		"quick question",
		"a question",
		"a quick question",
		"just a question",
		"just a quick question",
		"i had a question",
		"i had a quick question",
		"i just had a question",
		"i just had a quick question",
		"i have a question",
		"i have a quick question",
		"i just have a question",
		"i just have a quick question",
		"i got a question",
		"i got a quick question",
		"i've got a question",
		"i've got a quick question",
		"had a question",
		"had a quick question",
		"have a question",
		"have a quick question",
		"got a question",
		"got a quick question",
		"question please",
		"quick question please",
		"quick question for you",
		"i have a quick question for you",
		"i had a quick question for you",
		"i just had a quick question for you",
		"just a question please",
		"just a quick question please":
		return true
	default:
		return false
	}
}

// detectServiceKey scans the message for a known service name or alias from
// the clinic config and returns the canonical (lowercased) service key.
func detectServiceKey(message string, cfg *clinic.Config) string {
	message = strings.ToLower(message)
	if strings.TrimSpace(message) == "" {
		return ""
	}
	candidates := make([]string, 0, 16)
	if cfg != nil {
		for key := range cfg.ServicePriceText {
			candidates = append(candidates, key)
		}
		for key := range cfg.ServiceDepositAmountCents {
			candidates = append(candidates, key)
		}
		for _, svc := range cfg.Services {
			candidates = append(candidates, svc)
		}
	}
	candidates = append(candidates, "botox", "filler", "dermal filler", "consultation", "laser", "facial", "peel", "microneedling")

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		if strings.Contains(message, key) {
			// Resolve through aliases to canonical service name for price lookup.
			if cfg != nil {
				if resolved, ok := cfg.ServiceAliases[key]; ok {
					return strings.ToLower(resolved)
				}
			}
			return key
		}
	}
	return ""
}

// detectPHI returns true if the message appears to contain protected health
// information (PHI) such as diagnoses or medical conditions.
func detectPHI(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !phiPrefaceRE.MatchString(message) {
		return false
	}
	// Use regex with word boundaries to avoid false positives
	// (e.g., "sti" matching inside "existing")
	return phiKeywordsRE.MatchString(message)
}

// detectMedicalAdvice returns a list of medical keywords found in the message
// when it appears to be requesting medical advice. Returns nil if no medical
// advice request is detected.
func detectMedicalAdvice(message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}
	hasStrongCue := strongMedicalCueRE.MatchString(message)
	hasWeakCue := weakMedicalCueRE.MatchString(message)
	if !hasStrongCue && !hasWeakCue {
		return nil
	}
	// Strong cues ("is it safe", "side effects", etc.) trigger with any medical context
	// Weak cues ("can i", "should i") only trigger with medical-specific context
	// (medications, conditions) — not just service names like "botox" which indicate booking intent
	if hasStrongCue {
		if !medicalContextRE.MatchString(message) {
			return nil
		}
	} else {
		if !medicalSpecificContextRE.MatchString(message) {
			return nil
		}
	}
	keywords := []string{}
	for _, kw := range []string{
		"botox", "filler", "laser", "microneedling", "facial", "peel", "dermaplaning", "prp", "injectable",
		"medication", "medicine", "meds", "prescription", "ibuprofen", "tylenol", "acetaminophen", "antibiotic", "antibiotics",
		"painkiller", "painkillers", "blood pressure", "pregnant", "pregnancy", "breastfeeding", "allergy", "allergic",
		"contraindication", "contraindications", "side effects", "dosage", "dose", "interaction", "interactions", "mix with",
	} {
		if strings.Contains(message, kw) {
			keywords = append(keywords, kw)
		}
	}
	if len(keywords) == 0 {
		keywords = append(keywords, "medical_advice_request")
	}
	return keywords
}

// containsBookingIntent checks if the user message suggests they want to book.
func containsBookingIntent(msg string) bool {
	msg = strings.ToLower(msg)
	keywords := []string{"book", "appointment", "schedule", "available", "availability", "when can", "open slot", "time slot"}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
