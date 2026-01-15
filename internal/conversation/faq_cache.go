package conversation

import (
	"regexp"
	"strings"
)

// FAQEntry represents a cached FAQ response
type FAQEntry struct {
	Pattern  *regexp.Regexp
	Keywords []string // Alternative matching keywords
	Response string
}

// faqCache contains pre-computed responses for common medspa questions.
// These bypass the LLM for instant responses.
var faqCache = []FAQEntry{
	// HydraFacial vs DiamondGlow comparison
	{
		Pattern:  regexp.MustCompile(`(?i)(hydrafacial|hydra[\s-]?facial).*(diamond\s*glow|diamondglow)|(diamond\s*glow|diamondglow).*(hydrafacial|hydra[\s-]?facial)`),
		Keywords: []string{"hydrafacial", "diamondglow", "difference", "vs", "versus", "compare", "better"},
		Response: `Great question! Both are excellent exfoliating facials, but they work a bit differently:

HydraFacial uses vortex technology with water and serums to deeply cleanse and hydrate. It's very gentle and great for all skin types, especially sensitive skin.

DiamondGlow uses a diamond-tip wand for exfoliation while infusing serums. It's slightly more intense and may be better for those wanting deeper exfoliation.

For sensitive skin, I'd typically recommend starting with HydraFacial since it's the gentler option. Would you like to schedule a consultation to discuss which would be best for your skin goals?`,
	},
	// Hylenex/Filler dissolving vs Fillers comparison (must come before Botox vs Fillers)
	{
		Pattern:  regexp.MustCompile(`(?i)(dissolv|hylenex|hyaluronidase).*(filler)|(filler).*(dissolv|hylenex|hyaluronidase)`),
		Keywords: []string{}, // No keyword fallback - pattern only
		Response: `Great question! These are very different:

Dermal fillers (like Juvederm and Restylane) ADD volume to areas like lips, cheeks, and smile lines. They contain hyaluronic acid and results last 6-18 months.

Hylenex (hyaluronidase) is an enzyme that DISSOLVES hyaluronic acid fillers. It's used to reverse unwanted filler results, correct asymmetry, or treat complications. It's not a filler itself.

Would you like to schedule a consultation to discuss which service is right for you?`,
	},
	// Botox vs Fillers comparison
	{
		Pattern:  regexp.MustCompile(`(?i)(botox|dysport|xeomin).*(filler|juvederm|restylane)|(filler|juvederm|restylane).*(botox|dysport|xeomin)`),
		Keywords: []string{}, // Removed keyword fallback - pattern only to avoid false matches
		Response: `Great question! They work differently:

Botox relaxes muscles to smooth dynamic wrinkles (like forehead lines and crow's feet). Results last 3-4 months.

Fillers add volume and plump areas like lips, cheeks, and smile lines. Results typically last 6-18 months depending on the type.

Many patients actually use both for a complete rejuvenation! Would you like to schedule a consultation to see which would best address your concerns?`,
	},
	// Chemical peel vs Microneedling
	{
		Pattern:  regexp.MustCompile(`(?i)(chemical\s*peel|peel).*(microneedling|micro[\s-]?needling)|(microneedling|micro[\s-]?needling).*(chemical\s*peel|peel)`),
		Keywords: []string{"peel", "microneedling", "difference", "vs", "versus", "compare"},
		Response: `Both are great for skin rejuvenation but work differently:

Chemical peels use acids to exfoliate and improve texture, tone, and fine lines. Downtime varies from none to a week depending on depth.

Microneedling creates tiny punctures to stimulate collagen production. It's excellent for scars, pores, and overall skin texture with 1-3 days of redness.

Your practitioner can recommend the best option based on your skin type and goals. Would you like to schedule a consultation?`,
	},
	// Laser hair removal questions
	{
		Pattern:  regexp.MustCompile(`(?i)how many.*(session|treatment|appointment).*(laser|hair|removal)`),
		Keywords: []string{"laser", "hair", "sessions", "how many", "treatments"},
		Response: `Most people need 6-8 laser hair removal sessions spaced 4-6 weeks apart for optimal results. The exact number depends on the treatment area, hair color, and skin type. After completing the initial series, occasional maintenance sessions may be needed. Would you like to schedule a consultation to get a personalized treatment plan?`,
	},
	// How long does Botox last
	{
		Pattern:  regexp.MustCompile(`(?i)how long.*(botox|dysport|xeomin).*(last|work|effect)`),
		Keywords: []string{"botox", "long", "last", "duration"},
		Response: `Botox typically lasts 3-4 months. You'll start seeing results within 3-7 days, with full effects visible at 2 weeks. Many patients schedule maintenance appointments every 3-4 months to maintain their results. Would you like to book an appointment?`,
	},
	// How long do fillers last
	{
		Pattern:  regexp.MustCompile(`(?i)how long.*(filler|juvederm|restylane).*(last|work|effect)`),
		Keywords: []string{"filler", "long", "last", "duration"},
		Response: `Dermal fillers typically last 6-18 months depending on the type and treatment area. Lip fillers usually last 6-12 months, while cheek fillers can last 12-18 months. Results vary by individual metabolism. Would you like to schedule a consultation?`,
	},
}

// CheckFAQCache looks for a matching FAQ response.
// Returns the response and true if found, or empty string and false if not.
func CheckFAQCache(message string) (string, bool) {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return "", false
	}

	for _, faq := range faqCache {
		// First try pattern match
		if faq.Pattern != nil && faq.Pattern.MatchString(message) {
			return faq.Response, true
		}

		// Fall back to keyword matching (need at least 2 keywords to match)
		if len(faq.Keywords) > 0 {
			matchCount := 0
			for _, kw := range faq.Keywords {
				if strings.Contains(message, kw) {
					matchCount++
				}
			}
			// Require at least 2 keyword matches to trigger FAQ
			if matchCount >= 2 {
				return faq.Response, true
			}
		}
	}

	return "", false
}

// IsServiceComparisonQuestion checks if the message is asking about comparing services
func IsServiceComparisonQuestion(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	comparisonIndicators := []string{
		"difference between",
		"vs",
		"versus",
		"compared to",
		"or",
		"which is better",
		"which one",
		"what's the difference",
		"whats the difference",
	}
	for _, indicator := range comparisonIndicators {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	return false
}
