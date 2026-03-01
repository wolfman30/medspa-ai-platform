package conversation

// FilterResult captures inbound message safety filtering decisions.
type FilterResult struct {
	Blocked       bool
	RedactedMsg   string
	DeflectionMsg string
	SawPHI        bool
	MedicalKW     []string
	Sanitized     string
}

// FilterInbound evaluates an inbound message for PHI, prompt injection,
// and medical-advice requests.
//
// This function is pure and has no side effects.
func FilterInbound(raw string) FilterResult {
	result := FilterResult{
		RedactedMsg: raw,
		Sanitized:   raw,
	}

	redacted, sawPHI := RedactPHI(raw)
	result.RedactedMsg = redacted
	result.SawPHI = sawPHI

	if !sawPHI {
		result.MedicalKW = detectMedicalAdvice(raw)
		if len(result.MedicalKW) > 0 {
			result.RedactedMsg = "[REDACTED]"
		}
	}

	injectionResult := ScanForPromptInjection(raw)
	if injectionResult.Blocked {
		result.Blocked = true
		result.DeflectionMsg = blockedReply
		return result
	}
	if injectionResult.Score >= warnThreshold {
		result.Sanitized = SanitizeForLLM(raw)
	}

	if result.SawPHI {
		result.Blocked = true
		result.DeflectionMsg = phiDeflectionReply
		return result
	}
	if len(result.MedicalKW) > 0 {
		result.Blocked = true
		result.DeflectionMsg = medicalAdviceDeflectionReply
		return result
	}

	return result
}
