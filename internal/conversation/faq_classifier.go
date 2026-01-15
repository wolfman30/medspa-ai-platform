package conversation

import (
	"context"
	"encoding/json"
	"strings"
)

// FAQCategory represents a category of frequently asked questions
type FAQCategory string

const (
	FAQCategoryHydraFacialVsDiamondGlow FAQCategory = "hydrafacial_vs_diamondglow"
	FAQCategoryHylenexVsFillers         FAQCategory = "hylenex_vs_fillers"
	FAQCategoryBotoxVsFillers           FAQCategory = "botox_vs_fillers"
	FAQCategoryPeelVsMicroneedling      FAQCategory = "peel_vs_microneedling"
	FAQCategoryLaserHairSessions        FAQCategory = "laser_hair_sessions"
	FAQCategoryBotoxDuration            FAQCategory = "botox_duration"
	FAQCategoryFillerDuration           FAQCategory = "filler_duration"
	FAQCategoryOther                    FAQCategory = "other"
)

// faqResponses maps categories to their cached responses
var faqResponses = map[FAQCategory]string{
	FAQCategoryHydraFacialVsDiamondGlow: `Great question! Both are excellent exfoliating facials, but they work a bit differently:

HydraFacial uses vortex technology with water and serums to deeply cleanse and hydrate. It's very gentle and great for all skin types, especially sensitive skin.

DiamondGlow uses a diamond-tip wand for exfoliation while infusing serums. It's slightly more intense and may be better for those wanting deeper exfoliation.

For sensitive skin, I'd typically recommend starting with HydraFacial since it's the gentler option. Would you like to schedule a consultation to discuss which would be best for your skin goals?`,

	FAQCategoryHylenexVsFillers: `Great question! These are very different:

Dermal fillers (like Juvederm and Restylane) ADD volume to areas like lips, cheeks, and smile lines. They contain hyaluronic acid and results last 6-18 months.

Hylenex (hyaluronidase) is an enzyme that DISSOLVES hyaluronic acid fillers. It's used to reverse unwanted filler results, correct asymmetry, or treat complications. It's not a filler itself.

Would you like to schedule a consultation to discuss which service is right for you?`,

	FAQCategoryBotoxVsFillers: `Great question! They work differently:

Botox relaxes muscles to smooth dynamic wrinkles (like forehead lines and crow's feet). Results last 3-4 months.

Fillers add volume and plump areas like lips, cheeks, and smile lines. Results typically last 6-18 months depending on the type.

Many patients actually use both for a complete rejuvenation! Would you like to schedule a consultation to see which would best address your concerns?`,

	FAQCategoryPeelVsMicroneedling: `Both are great for skin rejuvenation but work differently:

Chemical peels use acids to exfoliate and improve texture, tone, and fine lines. Downtime varies from none to a week depending on depth.

Microneedling creates tiny punctures to stimulate collagen production. It's excellent for scars, pores, and overall skin texture with 1-3 days of redness.

Your practitioner can recommend the best option based on your skin type and goals. Would you like to schedule a consultation?`,

	FAQCategoryLaserHairSessions: `Most people need 6-8 laser hair removal sessions spaced 4-6 weeks apart for optimal results. The exact number depends on the treatment area, hair color, and skin type. After completing the initial series, occasional maintenance sessions may be needed. Would you like to schedule a consultation to get a personalized treatment plan?`,

	FAQCategoryBotoxDuration: `Botox typically lasts 3-4 months. You'll start seeing results within 3-7 days, with full effects visible at 2 weeks. Many patients schedule maintenance appointments every 3-4 months to maintain their results. Would you like to book an appointment?`,

	FAQCategoryFillerDuration: `Dermal fillers typically last 6-18 months depending on the type and treatment area. Lip fillers usually last 6-12 months, while cheek fillers can last 12-18 months. Results vary by individual metabolism. Would you like to schedule a consultation?`,
}

const faqClassifierPrompt = `Classify this medspa question into ONE category. Respond with JSON only.

Categories:
- hydrafacial_vs_diamondglow: Comparing HydraFacial and DiamondGlow facials
- hylenex_vs_fillers: Comparing Hylenex/filler dissolving with dermal fillers (NOT about Botox)
- botox_vs_fillers: Comparing Botox/neurotoxins with dermal fillers (NOT about dissolving)
- peel_vs_microneedling: Comparing chemical peels with microneedling
- laser_hair_sessions: Questions about how many laser hair removal sessions needed
- botox_duration: Questions about how long Botox lasts
- filler_duration: Questions about how long fillers last
- other: Anything else (booking, pricing, specific treatments, general questions)

IMPORTANT:
- "Hylenex", "filler dissolve", or "dissolving fillers" = hylenex_vs_fillers (NOT botox_vs_fillers)
- Only use botox_vs_fillers if the question explicitly mentions Botox/Dysport/Xeomin

Question: %s

Respond with: {"category": "<category_name>"}`

// FAQClassifier uses LLM to classify questions for cached FAQ responses
type FAQClassifier struct {
	client LLMClient
}

// NewFAQClassifier creates a new LLM-based FAQ classifier
func NewFAQClassifier(client LLMClient) *FAQClassifier {
	return &FAQClassifier{client: client}
}

// ClassifyQuestion uses LLM to determine the FAQ category for a question
func (c *FAQClassifier) ClassifyQuestion(ctx context.Context, question string) (FAQCategory, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return FAQCategoryOther, nil
	}

	prompt := strings.Replace(faqClassifierPrompt, "%s", question, 1)

	resp, err := c.client.Complete(ctx, LLMRequest{
		Messages:  []ChatMessage{{Role: ChatRoleUser, Content: prompt}},
		MaxTokens: 50,
	})
	if err != nil {
		return FAQCategoryOther, err
	}

	// Parse JSON response
	var result struct {
		Category string `json:"category"`
	}

	// Try to extract JSON from response (LLM might add extra text)
	content := strings.TrimSpace(resp.Text)
	startIdx := strings.Index(content, "{")
	endIdx := strings.LastIndex(content, "}")
	if startIdx >= 0 && endIdx > startIdx {
		content = content[startIdx : endIdx+1]
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return FAQCategoryOther, nil // Fall through to full LLM on parse error
	}

	category := FAQCategory(result.Category)
	if _, exists := faqResponses[category]; exists {
		return category, nil
	}
	return FAQCategoryOther, nil
}

// GetFAQResponse returns the cached response for a category, or empty if not found
func GetFAQResponse(category FAQCategory) string {
	return faqResponses[category]
}

// ClassifyAndRespond classifies a question and returns a cached response if applicable
// Returns empty string if the question should go to the full LLM
func (c *FAQClassifier) ClassifyAndRespond(ctx context.Context, question string) (string, error) {
	category, err := c.ClassifyQuestion(ctx, question)
	if err != nil {
		return "", err
	}

	if category == FAQCategoryOther {
		return "", nil
	}

	return GetFAQResponse(category), nil
}
