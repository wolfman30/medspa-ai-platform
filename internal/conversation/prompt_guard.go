package conversation

import (
	"regexp"
	"strings"
)

// PromptGuardResult contains the result of a prompt injection scan.
type PromptGuardResult struct {
	// Blocked is true if the message should NOT be sent to the LLM.
	Blocked bool
	// Score is a rough heuristic risk score (0.0 = safe, 1.0 = definitely injection).
	Score float64
	// Reasons lists the detection signals that fired.
	Reasons []string
	// Sanitized is the cleaned message (if not blocked).
	Sanitized string
}

// promptGuardPattern is a compiled regex with a reason label and weight.
type promptGuardPattern struct {
	re     *regexp.Regexp
	reason string
	weight float64
}

// blockThreshold: messages scoring above this are blocked outright.
const blockThreshold = 0.7

// warnThreshold: messages scoring above this get sanitized/flagged.
const warnThreshold = 0.3

// Direct injection patterns — attempts to override system instructions.
var directInjectionPatterns = []promptGuardPattern{
	{regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier|your)\s+(instructions?|rules?|prompts?|guidelines?|directives?|programming)`), "direct_injection:ignore_instructions", 0.9},
	{regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous|prior|above|earlier|your)\s+(instructions?|rules?|prompts?|guidelines?|directives?)`), "direct_injection:disregard_instructions", 0.9},
	{regexp.MustCompile(`(?i)forget\s+(all\s+)?(previous|prior|above|earlier|your)\s+(instructions?|rules?|prompts?|guidelines?|directives?)`), "direct_injection:forget_instructions", 0.9},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an|my)\s+`), "direct_injection:role_reassignment", 0.7},
	{regexp.MustCompile(`(?i)new\s+role\s*:|new\s+instructions?\s*:|system\s*prompt\s*:|<<\s*sys(tem)?\s*>>`), "direct_injection:new_role", 0.9},
	{regexp.MustCompile(`(?i)override\s+(your\s+)?(system|instructions?|rules?|safety|guidelines?)`), "direct_injection:override", 0.8},
	{regexp.MustCompile(`(?i)act\s+as\s+(if\s+)?(you\s+are\s+|you're\s+)?(a\s+|an\s+)?(?:different|new|unrestricted|unfiltered|jailbroken)`), "direct_injection:act_as", 0.8},
	{regexp.MustCompile(`(?i)(pretend|imagine|suppose|assume)\s+(that\s+)?(you\s+)?(are|have|were|don'?t\s+have)\s+(no\s+)?(rules?|restrictions?|limits?|boundaries|guidelines?|filters?|safety)`), "direct_injection:pretend_no_rules", 0.9},
	{regexp.MustCompile(`(?i)do\s+not\s+follow\s+(your|the|any)\s+(rules?|instructions?|guidelines?|safety)`), "direct_injection:do_not_follow", 0.9},
	{regexp.MustCompile(`(?i)bypass\s+(your\s+)?(safety|filters?|restrictions?|guidelines?|rules?|content\s+policy)`), "direct_injection:bypass", 0.8},
	{regexp.MustCompile(`(?i)jailbreak|DAN\s*mode|developer\s*mode|unrestricted\s*mode|god\s*mode`), "direct_injection:jailbreak_keyword", 0.9},
}

// Indirect injection — attempts to extract system prompt or internal data.
var exfiltrationPatterns = []promptGuardPattern{
	{regexp.MustCompile(`(?i)(reveal|show|display|print|output|repeat|tell\s+me|what\s+(is|are))\s+(your\s+)?(system\s+prompt|instructions?|rules?|initial\s+prompt|hidden\s+prompt|system\s+message|original\s+prompt)`), "exfiltration:system_prompt", 0.8},
	{regexp.MustCompile(`(?i)(what|list|show|give|tell)\s+(me\s+)?(all\s+)?(the\s+)?(other\s+)?patient('?s)?\s+(data|info|names?|numbers?|records?|details?|appointments?)`), "exfiltration:patient_data", 0.7},
	{regexp.MustCompile(`(?i)(what|list|show|give|tell)\s+(me\s+)?(the\s+)?(all\s+)?(api|secret|key|token|password|credential|database|env|config)\b`), "exfiltration:credentials", 0.8},
	{regexp.MustCompile(`(?i)\b(api|secret|stripe|telnyx|aws|database|db)\s*(key|token|secret|password|credential)s?\b`), "exfiltration:credentials_keyword", 0.8},
	{regexp.MustCompile(`(?i)repeat\s+(everything|all|the\s+text)\s+(above|before|from\s+the\s+start|from\s+the\s+beginning)`), "exfiltration:repeat_above", 0.7},
	{regexp.MustCompile(`(?i)what\s+(?:were\s+)?you\s+(?:were\s+)?told\s+(before|initially|at\s+the\s+(start|beginning))`), "exfiltration:what_told", 0.6},
}

// Encoding/obfuscation — attempts to bypass filters via encoding.
var obfuscationPatterns = []promptGuardPattern{
	{regexp.MustCompile(`(?i)base64\s*(encode|decode|:)|\\x[0-9a-fA-F]{2}`), "obfuscation:encoding", 0.5},
	{regexp.MustCompile(`(?i)(translate|convert|encode)\s+(this|the\s+following)\s+(to|into|as)\s+(base64|hex|rot13|binary|morse)`), "obfuscation:encoding_request", 0.4},
	// Markdown/HTML injection to try to render hidden content
	{regexp.MustCompile(`!\[.*\]\(https?://`), "obfuscation:markdown_image", 0.4},
	{regexp.MustCompile(`<\s*(script|img|iframe|object|embed|link|style|svg|form)\b`), "obfuscation:html_injection", 0.6},
}

// Context manipulation — attempts to change the conversation frame.
var contextManipulationPatterns = []promptGuardPattern{
	{regexp.MustCompile(`(?i)(end\s+of\s+)?(system|assistant)\s*(message|prompt|instructions?)\s*[\-=]{2,}`), "context_manipulation:fake_boundary", 0.8},
	{regexp.MustCompile(`(?i)\[/?INST\]|\[/?SYS\]|<\|im_start\|>|<\|im_end\|>|<\|system\|>|<\|user\|>|<\|assistant\|>`), "context_manipulation:special_tokens", 0.9},
	{regexp.MustCompile(`(?i)###\s*(system|instruction|human|assistant|user)\s*:`), "context_manipulation:role_markers", 0.7},
	{regexp.MustCompile(`(?i)(previous|above)\s+conversation\s+(is|was)\s+(just\s+)?(a\s+)?(test|example|fake|simulation)`), "context_manipulation:dismiss_context", 0.7},
	{regexp.MustCompile(`(?i)the\s+real\s+(instructions?|task|prompt|conversation)\s+(is|starts?|begins?)`), "context_manipulation:real_instructions", 0.8},
}

// allPatterns combines all pattern groups.
var allPromptGuardPatterns []promptGuardPattern

func init() {
	allPromptGuardPatterns = make([]promptGuardPattern, 0, len(directInjectionPatterns)+len(exfiltrationPatterns)+len(obfuscationPatterns)+len(contextManipulationPatterns))
	allPromptGuardPatterns = append(allPromptGuardPatterns, directInjectionPatterns...)
	allPromptGuardPatterns = append(allPromptGuardPatterns, exfiltrationPatterns...)
	allPromptGuardPatterns = append(allPromptGuardPatterns, obfuscationPatterns...)
	allPromptGuardPatterns = append(allPromptGuardPatterns, contextManipulationPatterns...)
}

// ScanForPromptInjection analyzes inbound user text for prompt injection attempts.
// It returns a result with scoring, reasons, and optionally a sanitized version.
func ScanForPromptInjection(message string) PromptGuardResult {
	if strings.TrimSpace(message) == "" {
		return PromptGuardResult{Sanitized: message}
	}

	var reasons []string
	var totalWeight float64
	maxWeight := 0.0

	for _, p := range allPromptGuardPatterns {
		if p.re.MatchString(message) {
			reasons = append(reasons, p.reason)
			totalWeight += p.weight
			if p.weight > maxWeight {
				maxWeight = p.weight
			}
		}
	}

	// Score: use the max individual pattern weight, boosted if multiple patterns fire.
	score := maxWeight
	if len(reasons) > 1 {
		// Multiple signals compound: add 0.1 per additional signal (capped at 1.0).
		score = maxWeight + float64(len(reasons)-1)*0.1
		if score > 1.0 {
			score = 1.0
		}
	}

	result := PromptGuardResult{
		Score:     score,
		Reasons:   reasons,
		Sanitized: message,
	}

	if score >= blockThreshold {
		result.Blocked = true
	}

	return result
}

// blockedReply is the generic response when a message is blocked.
const blockedReply = "I'm here to help you with appointment scheduling and questions about our services. How can I assist you today?"

// SanitizeForLLM strips known injection markers from a message while preserving
// legitimate content. Used for messages that score between warn and block thresholds.
func SanitizeForLLM(message string) string {
	// Strip special token markers
	cleaned := regexp.MustCompile(`(?i)\[/?INST\]|\[/?SYS\]|<\|im_start\|>|<\|im_end\|>|<\|system\|>|<\|user\|>|<\|assistant\|>`).ReplaceAllString(message, "")

	// Strip fake role/boundary markers
	cleaned = regexp.MustCompile(`(?i)###\s*(system|instruction|human|assistant|user)\s*:`).ReplaceAllString(cleaned, "")

	// Strip HTML tags that could be used for injection
	cleaned = regexp.MustCompile(`<\s*(script|img|iframe|object|embed|link|style|svg|form)\b[^>]*>`).ReplaceAllString(cleaned, "")

	// Strip markdown image injection
	cleaned = regexp.MustCompile(`!\[.*?\]\(https?://[^)]+\)`).ReplaceAllString(cleaned, "")

	return strings.TrimSpace(cleaned)
}
