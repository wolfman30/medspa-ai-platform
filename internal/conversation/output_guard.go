package conversation

import (
	"regexp"
	"strings"
)

// OutputGuardResult contains the result of scanning an outbound AI reply.
type OutputGuardResult struct {
	// Leaked is true if the reply contains sensitive information that should not be sent.
	Leaked bool
	// Reasons lists the detection signals that fired.
	Reasons []string
	// Sanitized is the cleaned reply (if fixable) or empty string (if should be blocked).
	Sanitized string
}

// outputLeakPattern is a compiled regex for detecting sensitive output leaks.
type outputLeakPattern struct {
	re     *regexp.Regexp
	reason string
	block  bool // if true, block entirely; if false, can try to sanitize
}

var outputLeakPatterns = []outputLeakPattern{
	// System prompt / instruction leaks
	{regexp.MustCompile(`(?i)my (system\s+)?prompt\s+(is|says|tells|instructs)`), "leak:system_prompt_disclosure", true},
	{regexp.MustCompile(`(?i)my instructions?\s+(are|say|tell|include|require)`), "leak:instructions_disclosure", true},
	{regexp.MustCompile(`(?i)i('m| am) (programmed|instructed|told|designed|configured) to`), "leak:programming_disclosure", true},
	{regexp.MustCompile(`(?i)(here are|these are|the following are)\s+(my )?(system )?(instructions|rules|guidelines|prompts)`), "leak:rules_listing", true},

	// AI identity leaks
	{regexp.MustCompile(`(?i)i('m| am) (a|an) (AI|artificial intelligence|language model|LLM|GPT|Claude|chatbot|chat bot)\b`), "leak:ai_identity", false},
	{regexp.MustCompile(`(?i)(powered by|built on|running on|using)\s+(Claude|GPT|OpenAI|Anthropic|Bedrock|AWS)`), "leak:tech_stack", true},

	// Credential / infrastructure leaks
	{regexp.MustCompile(`(?i)(api[_\s]?key|secret[_\s]?key|access[_\s]?token|bearer\s+token)\s*[:=]\s*\S+`), "leak:credential", true},
	{regexp.MustCompile(`(?i)(sk|pk)[-_](live|test)[-_][a-zA-Z0-9]{20,}`), "leak:stripe_key", true},
	{regexp.MustCompile(`(?i)AKIA[A-Z0-9]{16}`), "leak:aws_key", true},
	{regexp.MustCompile(`(?i)(postgres|mysql|redis|mongodb)://\S+`), "leak:database_url", true},
	{regexp.MustCompile(`(?i)\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d{2,5}`), "leak:ip_port", true},

	// Internal URL / endpoint leaks
	{regexp.MustCompile(`(?i)(api-dev|portal-dev|staging|internal)\.[a-z]+\.(com|io|net)`), "leak:internal_url", true},
	{regexp.MustCompile(`(?i)/admin/|/webhooks/|/internal/|/debug/`), "leak:internal_path", true},

	// Other patient data patterns (should never appear in a reply)
	{regexp.MustCompile(`(?i)other patient'?s?\s+(name|phone|email|appointment|record)`), "leak:other_patient_ref", true},
}

// ScanOutputForLeaks checks an outbound AI reply for sensitive information leaks.
func ScanOutputForLeaks(reply string) OutputGuardResult {
	if strings.TrimSpace(reply) == "" {
		return OutputGuardResult{Sanitized: reply}
	}

	var reasons []string
	shouldBlock := false

	for _, p := range outputLeakPatterns {
		if p.re.MatchString(reply) {
			reasons = append(reasons, p.reason)
			if p.block {
				shouldBlock = true
			}
		}
	}

	if len(reasons) == 0 {
		return OutputGuardResult{Sanitized: reply}
	}

	result := OutputGuardResult{
		Leaked:  true,
		Reasons: reasons,
	}

	if shouldBlock {
		// Can't salvage — replace entirely
		result.Sanitized = ""
	} else {
		// Try to sanitize (e.g., remove "I am an AI" disclosure)
		result.Sanitized = sanitizeOutput(reply)
	}

	return result
}

// sanitizeOutput removes AI identity disclosures from replies while preserving useful content.
func sanitizeOutput(reply string) string {
	// Remove "I am an AI/chatbot" sentences
	cleaned := regexp.MustCompile(`(?i)[^.!?]*\bi('m| am) (a|an) (AI|artificial intelligence|language model|LLM|GPT|Claude|chatbot)\b[^.!?]*[.!?]?\s*`).ReplaceAllString(reply, "")
	return strings.TrimSpace(cleaned)
}
