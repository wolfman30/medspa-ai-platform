package conversation

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// serviceAliasesFromConfig extracts the ServiceAliases map from a clinic config.
// Returns nil if cfg is nil.
func serviceAliasesFromConfig(cfg *clinic.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	return cfg.ServiceAliases
}

// ---------- package-level compiled regexes ----------

var (
	newPatientRE       = regexp.MustCompile(`(?i)\bnew patient\b|first time|\bi'm new\b|\bi am new\b|\bnever been\b|,\s*new\s*[,.]|\bnew here\b`)
	returningPatientRE = regexp.MustCompile(`(?i)\breturning\b|\bexisting patient\b|\bi've been\b|\bi have been\b|\bbeen there\b|\bbeen before\b|\bvisited before\b|\bcome before\b|\bcome here before\b|,\s*returning\s*[,.]|,\s*existing\s*[,.]`)
	timeRangeRE        = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(?:am|pm|a|p)?\s*[-–—]\s*(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)`)
	betweenRE          = regexp.MustCompile(`(?i)between\s+(\d{1,2})(?::(\d{2}))?\s*(?:am|pm|a|p)?\s+and\s+(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)`)
	specificTimeRE     = regexp.MustCompile(`(?i)(around |about |at |after |before )?(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)\b`)
	noonRE             = regexp.MustCompile(`(?i)\b(noon|midday)\b`)
)

// ---------- universal fallback service patterns ----------

// universalServicePatterns is a small set of patterns that apply regardless of clinic config.
// Ordered by specificity — check longer/specific terms first.
var universalServicePatterns = []struct {
	pattern string
	name    string
}{
	// Filler dissolve
	{"filler dissolve", "filler dissolve"},
	{"dissolve filler", "filler dissolve"},
	{"dissolve", "filler dissolve"},
	{"hylenex", "filler dissolve"},
	// Specific filler types
	{"mini lip filler", "mini lip filler"},
	{"mini lip", "mini lip filler"},
	{"half syringe lip", "mini lip filler"},
	{"dermal filler", "dermal filler"},
	{"lip filler", "lip filler"},
	{"lip injection", "lip filler"},
	{"lip augmentation", "lip filler"},
	{"cheek filler", "cheek filler"},
	{"jawline filler", "jawline filler"},
	{"chin filler", "chin filler"},
	{"under eye filler", "under eye filler"},
	{"tear trough", "tear trough filler"},
	{"radiesse", "radiesse"},
	{"skinvive", "skinvive"},
	// Peels
	{"perfect derma peel", "Perfect Derma Peel"},
	{"chemical peel", "chemical peel"},
	{"vi peel", "VI Peel"},
	// Weight loss
	{"weight loss consultation", "weight loss consultation"},
	{"semaglutide", "semaglutide"},
	{"weight loss", "weight loss"},
	{"lose weight", "weight loss"},
	{"losing weight", "weight loss"},
	{"tirzepatide", "tirzepatide"},
	{"ozempic", "weight loss"},
	{"wegovy", "weight loss"},
	{"mounjaro", "weight loss"},
	{"glp-1", "weight loss"},
	// Tixel
	{"tixel full face and neck", "tixel - full face & neck"},
	{"tixel face and neck", "tixel - full face & neck"},
	{"tixel full face", "tixel - full face"},
	{"tixel face", "tixel - full face"},
	{"tixel decollete", "tixel - decollete"},
	{"tixel chest", "tixel - decollete"},
	{"tixel neck", "tixel - neck"},
	{"tixel eye", "tixel - around the eyes"},
	{"tixel mouth", "tixel - around the mouth"},
	{"tixel arm", "tixel - upper arms"},
	{"tixel hand", "tixel - hands"},
	{"tixel", "tixel"},
	// Laser hair removal
	{"laser hair removal", "laser hair removal"},
	{"hair removal", "laser hair removal"},
	// IPL / photofacial
	{"ipl face", "ipl - full face"},
	{"ipl neck", "ipl - neck"},
	{"ipl chest", "ipl - chest"},
	{"photofacial", "ipl"},
	{"ipl", "ipl"},
	// Tattoo removal
	{"tattoo removal", "tattoo removal"},
	{"tattoo", "tattoo removal"},
	// Vascular / spider veins
	{"vascular lesion", "vascular lesion removal"},
	{"spider vein", "vascular lesion removal"},
	// Erbium laser resurfacing
	{"ablative erbium", "ablative erbium laser resurfacing"},
	{"fractional erbium", "fractional erbium laser resurfacing"},
	{"erbium", "erbium laser resurfacing"},
	{"laser resurfacing", "laser resurfacing"},
	// Under eye
	{"under eye treatment", "pbf under eye treatment"},
	{"pbf under eye", "pbf under eye treatment"},
	// Threads
	{"pdo thread", "PDO threads"},
	{"thread lift", "thread lift"},
	// Microneedling
	{"microneedling with prp", "microneedling with prp"},
	{"microneedling", "microneedling"},
	{"prp", "PRP"},
	{"vampire facial", "PRP facial"},
	// Other treatments
	{"hydrafacial", "HydraFacial"},
	{"salmon dna facial", "salmon dna facial"},
	{"salmon facial", "salmon dna facial"},
	{"laser treatment", "laser treatment"},
	{"laser hair", "laser hair removal"},
	// Neurotoxins
	{"jeuveau", "Jeuveau"},
	{"dysport", "Dysport"},
	{"xeomin", "Xeomin"},
	{"lip flip", "Botox"},
	{"fix my 11s", "Botox"},
	{"fix my elevens", "Botox"},
	{"my 11s", "Botox"},
	{"eleven lines", "Botox"},
	{"11 lines", "Botox"},
	{"frown lines", "Botox"},
	{"forehead lines", "Botox"},
	{"brow lift", "Botox"},
	{"bunny lines", "Botox"},
	{"crow's feet", "Botox"},
	{"crows feet", "Botox"},
	{"botox", "Botox"},
	// Kybella
	{"kybella", "kybella"},
	{"double chin", "kybella"},
	// Wellness
	{"b12 shot", "b12 shot"},
	{"b12", "b12 shot"},
	{"vitamin injection", "b12 shot"},
	{"nad+", "nad+"},
	{"nad", "nad+"},
	// Generic catch-alls (last)
	{"filler", "filler"},
	{"consultation", "consultation"},
	{"facial", "facial"},
	{"peel", "peel"},
	{"laser", "laser"},
	{"injectable", "injectables"},
	{"wrinkle", "wrinkle treatment"},
	{"anti-aging", "anti-aging treatment"},
}

// ---------- past service patterns ----------

var pastServicePatterns = []struct {
	pattern string
	name    string
}{
	{"had botox", "Botox"},
	{"got botox", "Botox"},
	{"did botox", "Botox"},
	{"had filler", "filler"},
	{"got filler", "filler"},
	{"did filler", "filler"},
	{"had lip", "lip filler"},
	{"got lip", "lip filler"},
	{"had hydrafacial", "HydraFacial"},
	{"got hydrafacial", "HydraFacial"},
	{"had facial", "facial"},
	{"got facial", "facial"},
	{"did facial", "facial"},
	{"had weight loss", "weight loss"},
	{"did weight loss", "weight loss"},
	{"had semaglutide", "semaglutide"},
	{"did semaglutide", "semaglutide"},
	{"had laser", "laser"},
	{"got laser", "laser"},
	{"had microneedling", "microneedling"},
	{"got microneedling", "microneedling"},
	{"had peel", "peel"},
	{"got peel", "peel"},
	{"had prp", "PRP"},
	{"got prp", "PRP"},
	{"had dysport", "Dysport"},
	{"got dysport", "Dysport"},
	{"had jeuveau", "Jeuveau"},
	{"got jeuveau", "Jeuveau"},
	{"had xeomin", "Xeomin"},
	{"got xeomin", "Xeomin"},
}

// ---------- name extraction ----------

const nameWordPattern = `[\p{L}][\p{L}\p{M}'-]*`

var namePhrasePattern = nameWordPattern + `(?:\s+` + nameWordPattern + `){0,2}`

var namePatterns = buildNamePatterns()

var nameTextNormalizer = strings.NewReplacer(
	"\u2019", "'", // right single quote
	"\u2018", "'", // left single quote
	"\u2032", "'", // prime symbol
)

func buildNamePatterns() []*regexp.Regexp {
	name := namePhrasePattern
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)my name is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)i'?m\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)i am\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)this is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)call me\s+(` + name + `)`),
		regexp.MustCompile(`(?i)it'?s\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)name'?s\s+(` + name + `)`),
	}
}

func normalizeNameText(text string) string {
	if text == "" {
		return ""
	}
	return nameTextNormalizer.Replace(text)
}

func collectUserMessages(history []ChatMessage) (lowercase string, original string) {
	var lowerBuilder strings.Builder
	var originalBuilder strings.Builder
	for _, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		lowerBuilder.WriteString(strings.ToLower(msg.Content))
		lowerBuilder.WriteString(" ")
		originalBuilder.WriteString(msg.Content)
		originalBuilder.WriteString(" ")
	}
	return lowerBuilder.String(), originalBuilder.String()
}

func findNameInUserMessages(userMessages string) (fullName, firstName string) {
	normalized := normalizeNameText(userMessages)
	for _, pattern := range namePatterns {
		matches := pattern.FindAllStringSubmatch(normalized, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			full, first := fullAndFirstNameFromParts(extractNameParts(match[1]))
			if full != "" {
				return full, ""
			}
			if firstName == "" && first != "" {
				firstName = first
			}
		}
	}
	return "", firstName
}

func nameFromReplyAfterNameQuestion(history []ChatMessage) (fullName, firstName string) {
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" || !assistantAskedForName(prev) {
			continue
		}
		full, first := fullAndFirstNameFromParts(extractNameParts(msg.Content))
		if full != "" || first != "" {
			return full, first
		}
	}
	return "", ""
}

func combineSplitNameReplies(history []ChatMessage, firstName string) string {
	first := strings.TrimSpace(firstName)
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" {
			continue
		}
		if first == "" && (assistantAskedForName(prev) || assistantAskedForFirstName(prev)) {
			full, firstOnly := fullAndFirstNameFromParts(extractNameParts(msg.Content))
			if full != "" {
				return full
			}
			if firstOnly != "" {
				first = firstOnly
			}
			continue
		}
		if first != "" && assistantAskedForLastName(prev) {
			parts := extractNameParts(msg.Content)
			if len(parts) == 0 {
				continue
			}
			if len(parts) >= 2 {
				return parts[0] + " " + parts[1]
			}
			return first + " " + parts[0]
		}
	}
	return ""
}

// lastAssistantAskedForName checks if the most recent assistant message in history
// already asked for the patient's name, to avoid duplicate name requests.
func lastAssistantAskedForName(history []ChatMessage) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleAssistant {
			return assistantAskedForName(history[i].Content)
		}
	}
	return false
}

func assistantAskedForName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if !strings.Contains(message, "name") {
		return false
	}
	if strings.Contains(message, "full name") || strings.Contains(message, "first and last") {
		return true
	}
	if strings.Contains(message, "first name") || strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "your name") {
		return true
	}
	if strings.Contains(message, "may i") || strings.Contains(message, "what") || strings.Contains(message, "can i") || strings.Contains(message, "could i") {
		return true
	}
	return false
}

func assistantAskedForFirstName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	return strings.Contains(message, "first name")
}

func assistantAskedForLastName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "surname") || strings.Contains(message, "family name") {
		return true
	}
	return false
}

func fullAndFirstNameFromParts(parts []string) (fullName, firstName string) {
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1], parts[0]
	}
	if len(parts) == 1 {
		return "", parts[0]
	}
	return "", ""
}

func extractNameParts(raw string) []string {
	raw = normalizeNameText(raw)
	words := strings.Fields(strings.TrimSpace(raw))
	nameWords := make([]string, 0, 2)
	for _, word := range words {
		cleaned := cleanNameToken(word)
		if cleaned == "" {
			continue
		}
		if !looksLikeNameWord(cleaned) {
			if len(nameWords) > 0 {
				break
			}
			continue
		}
		nameWords = append(nameWords, capitalizeNameWord(cleaned))
		if len(nameWords) == 2 {
			break
		}
	}
	return nameWords
}

func cleanNameToken(word string) string {
	word = strings.TrimSpace(word)
	if word == "" {
		return ""
	}
	word = strings.Trim(word, ".,!?\"()[]{}")
	word = strings.Trim(word, "'-")
	return word
}

func looksLikeNameWord(word string) bool {
	count := utf8.RuneCountInString(word)
	if count < 2 || count > 30 {
		return false
	}
	firstRune, _ := utf8.DecodeRuneInString(word)
	if !unicode.IsLetter(firstRune) {
		return false
	}
	if isCommonWord(strings.ToLower(word)) {
		return false
	}
	return true
}

func capitalizeNameWord(word string) string {
	if word == "" {
		return ""
	}
	firstRune, size := utf8.DecodeRuneInString(word)
	if firstRune == utf8.RuneError || size == 0 {
		return word
	}
	return strings.ToUpper(string(firstRune)) + strings.ToLower(word[size:])
}

// isCommonWord checks if a word is a common English word that shouldn't be treated as a name
func isCommonWord(word string) bool {
	common := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "her": true,
		"was": true, "one": true, "our": true, "out": true, "day": true,
		"had": true, "has": true, "his": true, "how": true, "its": true,
		"may": true, "new": true, "now": true, "old": true, "see": true,
		"way": true, "who": true, "boy": true, "did": true, "get": true,
		"let": true, "put": true, "say": true, "she": true, "too": true,
		"use": true, "yes": true, "no": true, "hi": true, "hey": true,
		"thanks": true, "thank": true, "please": true, "ok": true, "okay": true,
		"sure": true, "good": true, "great": true, "fine": true, "well": true,
		"just": true, "like": true, "want": true, "need": true, "have": true,
		"interested": true, "looking": true, "book": true, "booking": true, "appointment": true,
		"morning": true, "afternoon": true, "evening": true, "weekday": true,
		"weekend": true, "available": true, "schedule": true, "scheduling": true, "time": true,
		"botox": true, "filler": true, "facial": true, "laser": true,
		"consultation": true, "treatment": true, "service": true,
		"existing": true, "returning": true, "patient": true, "calling": true, "texting": true,
		"in": true, "on": true, "at": true, "to": true, "of": true, "is": true, "it": true,
		"an": true, "as": true, "be": true, "by": true, "do": true, "if": true, "or": true,
		"so": true, "up": true, "we": true, "me": true, "my": true, "he": true,
		"weight": true, "loss": true, "skin": true, "body": true, "face": true, "lip": true,
		"hair": true, "nail": true, "peel": true, "tox": true,
		"about": true, "with": true, "from": true, "this": true, "that": true, "what": true,
		"when": true, "your": true, "some": true, "been": true, "were": true, "them": true,
		"then": true, "than": true, "also": true, "very": true, "more": true, "much": true,
		"here": true, "there": true, "where": true, "which": true, "their": true,
		"would": true, "could": true, "should": true, "will": true,
		"inquiring": true, "writing": true, "reaching": true, "contacting": true,
		"wondering": true, "asking": true, "checking": true, "getting": true,
	}
	return common[strings.ToLower(word)]
}

// ---------- patient type detection ----------

// detectPatientType merges all patient-type detection approaches into one function.
// Priority: 1) regex scan of all user messages, 2) short-reply context check.
func detectPatientType(userMessages string, history []ChatMessage) string {
	// 1. Regex scan across all user messages
	if newPatientRE.MatchString(userMessages) {
		return "new"
	}
	if returningPatientRE.MatchString(userMessages) {
		return "existing"
	}

	// 2. Short reply after assistant asked about patient type
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != ChatRoleUser {
			continue
		}
		reply := normalizePatientTypeReply(history[i].Content)
		if reply == "" {
			continue
		}
		if !assistantAskedPatientType(history, i) {
			continue
		}
		return reply
	}
	return ""
}

func normalizePatientTypeReply(message string) string {
	cleaned := strings.ToLower(strings.TrimSpace(message))
	cleaned = strings.Trim(cleaned, ".,!?")
	switch cleaned {
	case "new", "new patient", "new here", "first time", "first-time", "never been", "never been before", "i'm new", "im new", "i am new":
		return "new"
	case "existing", "returning", "existing patient", "returning patient", "been before", "i've been before", "i have been before", "not new",
		"visited before", "i've visited before", "i have visited before", "come before", "i've come before", "been here before", "yes i have":
		return "existing"
	default:
		return ""
	}
}

func assistantAskedPatientType(history []ChatMessage, userIndex int) bool {
	prev := previousAssistantMessage(history, userIndex)
	if prev == "" {
		return false
	}
	content := strings.ToLower(prev)
	if strings.Contains(content, "new patient") || strings.Contains(content, "existing patient") || strings.Contains(content, "returning patient") {
		return true
	}
	if strings.Contains(content, "visited") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "been") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "new") && (strings.Contains(content, "existing") || strings.Contains(content, "returning")) {
		return true
	}
	if strings.Contains(content, "are you new") && (strings.Contains(content, "patient") || strings.Contains(content, "here") || strings.Contains(content, "before")) {
		return true
	}
	return false
}

// ---------- schedule detection ----------

// scheduleFromShortReply detects flexible schedule replies when the assistant
// just asked about preferred days/times.
func scheduleFromShortReply(history []ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != ChatRoleUser {
			continue
		}
		if !assistantAskedSchedule(history, i) {
			continue
		}
		reply := strings.ToLower(strings.TrimSpace(history[i].Content))
		reply = strings.Trim(reply, ".,!?")
		flexPatterns := []string{
			"whenever", "whenever works", "whenever is fine", "whenever works for me",
			"anything", "anything works", "anything is fine",
			"open", "i'm open", "im open", "i am open", "wide open",
			"no preference", "no pref", "doesn't matter", "dont matter",
			"don't care", "dont care", "whatever", "whatever works",
			"any", "any time", "anytime", "any day",
			"flexible", "i'm flexible", "im flexible", "pretty flexible",
			"free", "i'm free", "im free", "i am free",
			"works for me", "all good", "good with anything",
		}
		for _, pat := range flexPatterns {
			if reply == pat || strings.Contains(reply, pat) {
				return "flexible"
			}
		}
	}
	return ""
}

func assistantAskedSchedule(history []ChatMessage, userIndex int) bool {
	prev := previousAssistantMessage(history, userIndex)
	if prev == "" {
		return false
	}
	content := strings.ToLower(prev)
	scheduleIndicators := []string{
		"days and times",
		"day and time",
		"what days",
		"what times",
		"when works",
		"when would",
		"preferred time",
		"preferred day",
		"schedule",
		"availability",
		"work best for you",
		"work for you",
		"convenient for you",
	}
	for _, ind := range scheduleIndicators {
		if strings.Contains(content, ind) {
			return true
		}
	}
	return false
}

// ---------- provider preference ----------

// matchProviderNameInText scans user messages for known provider names from the
// conversation's system prompt (which lists available providers).
func matchProviderNameInText(userMessages string, history []ChatMessage) string {
	var providerNames []string
	for _, msg := range history {
		if msg.Role == ChatRoleSystem || msg.Role == ChatRoleAssistant {
			lower := strings.ToLower(msg.Content)
			if strings.Contains(lower, "provider") || strings.Contains(lower, "brandi") || strings.Contains(lower, "gale") {
				for _, pattern := range []string{"we have ", "available providers: ", "providers: "} {
					idx := strings.Index(lower, pattern)
					if idx >= 0 {
						segment := msg.Content[idx+len(pattern):]
						segment = strings.ReplaceAll(segment, " and ", ", ")
						if dashIdx := strings.Index(segment, " - "); dashIdx >= 0 {
							segment = segment[:dashIdx]
						}
						if dotIdx := strings.IndexAny(segment, ".!?\n"); dotIdx >= 0 {
							segment = segment[:dotIdx]
						}
						parts := strings.Split(segment, ", ")
						for _, p := range parts {
							name := strings.TrimSpace(p)
							if name != "" && looksLikePersonName(name) {
								providerNames = append(providerNames, name)
							}
						}
					}
				}
			}
		}
	}

	for _, fullName := range providerNames {
		firstName := strings.ToLower(strings.Fields(fullName)[0])
		if len(firstName) > 2 && strings.Contains(userMessages, firstName) {
			return fullName
		}
	}
	return ""
}

// looksLikePersonName filters out extracted "provider names" that are actually
// service names or other non-person text.
func looksLikePersonName(name string) bool {
	words := strings.Fields(name)
	if len(words) < 2 || len(words) > 4 {
		return false
	}
	serviceWords := map[string]bool{
		"lip": true, "filler": true, "fillers": true, "augmentation": true,
		"injection": true, "botox": true, "peel": true, "laser": true,
		"hair": true, "removal": true, "weight": true, "loss": true,
		"skin": true, "facial": true, "microneedling": true, "tixel": true,
		"chemical": true, "dermal": true, "consultation": true, "treatment": true,
		"prp": true, "b12": true, "vitamin": true, "iv": true, "therapy": true,
		"enhancement": true, "rejuvenation": true, "sculpting": true,
	}
	for _, w := range words {
		if serviceWords[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// providerPreferenceFromReply checks if the assistant asked about provider preference
// and the user replied with a name or "no preference".
func providerPreferenceFromReply(history []ChatMessage) string {
	providerQuestionPatterns := []string{
		"provider preference", "preferred provider", "specific provider",
		"particular provider", "who would you like", "do you have a preference for a provider",
	}
	for i := len(history) - 1; i >= 1; i-- {
		msg := history[i]
		if msg.Role != ChatRoleUser {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if history[j].Role == ChatRoleAssistant {
				assistantMsg := strings.ToLower(history[j].Content)
				askedAboutProvider := false
				for _, pat := range providerQuestionPatterns {
					if strings.Contains(assistantMsg, pat) {
						askedAboutProvider = true
						break
					}
				}
				if askedAboutProvider {
					reply := strings.ToLower(strings.TrimSpace(msg.Content))
					if reply == "no" || reply == "nope" || strings.Contains(reply, "no preference") ||
						strings.Contains(reply, "doesn't matter") || strings.Contains(reply, "don't care") ||
						strings.Contains(reply, "either") || strings.Contains(reply, "anyone") ||
						strings.Contains(reply, "whoever") {
						return "no preference"
					}
					if len(reply) > 1 && len(reply) < 50 {
						return msg.Content
					}
				}
				break
			}
		}
	}
	return ""
}

// ---------- helper ----------

func previousAssistantMessage(history []ChatMessage, start int) string {
	for i := start - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleSystem {
			continue
		}
		if history[i].Role != ChatRoleAssistant {
			return ""
		}
		return history[i].Content
	}
	return ""
}

// ---------- service matching ----------

// matchService finds the best service match using config-driven aliases first,
// then falling back to universal patterns. Returns the matched service name.
func matchService(text string, serviceAliases map[string]string) string {
	if len(serviceAliases) > 0 {
		// Build sorted alias list for deterministic, longest-match-first ordering
		type aliasPair struct {
			pattern string
			name    string
		}
		pairs := make([]aliasPair, 0, len(serviceAliases))
		for alias, service := range serviceAliases {
			pairs = append(pairs, aliasPair{strings.ToLower(alias), service})
		}
		// Sort by pattern length descending for longest-match-first
		for i := 0; i < len(pairs); i++ {
			for j := i + 1; j < len(pairs); j++ {
				if len(pairs[j].pattern) > len(pairs[i].pattern) {
					pairs[i], pairs[j] = pairs[j], pairs[i]
				}
			}
		}
		for _, p := range pairs {
			if strings.Contains(text, p.pattern) {
				return p.name
			}
		}
	}

	// Fall back to universal patterns
	for _, s := range universalServicePatterns {
		if strings.Contains(text, s.pattern) {
			return s.name
		}
	}
	return ""
}

// ---------- main extraction function ----------

// extractPreferences extracts scheduling preferences from conversation history.
// serviceAliases maps patient-facing terms to canonical service names (from clinic config).
// Pass nil if no config is available.
func extractPreferences(history []ChatMessage, serviceAliases map[string]string) (leads.SchedulingPreferences, bool) {
	prefs := leads.SchedulingPreferences{}
	hasPreferences := false

	userMessages, userMessagesOriginal := collectUserMessages(history)

	// --- Name extraction ---
	fullName, firstNameFallback := findNameInUserMessages(userMessagesOriginal)
	if fullName == "" {
		fullNameFromPrompt, firstFromPrompt := nameFromReplyAfterNameQuestion(history)
		if fullNameFromPrompt != "" {
			fullName = fullNameFromPrompt
		}
		if firstNameFallback == "" {
			firstNameFallback = firstFromPrompt
		}
	}
	if fullName == "" {
		fullName = combineSplitNameReplies(history, firstNameFallback)
	}
	if fullName != "" {
		prefs.Name = fullName
		hasPreferences = true
	} else if firstNameFallback != "" {
		prefs.Name = firstNameFallback
		hasPreferences = true
	}

	// --- Patient type (unified) ---
	if pt := detectPatientType(userMessages, history); pt != "" {
		prefs.PatientType = pt
		hasPreferences = true
	}

	// --- Past services ---
	if prefs.PatientType == "existing" || strings.Contains(userMessages, "before") || strings.Contains(userMessages, "previously") || strings.Contains(userMessages, "last time") {
		var pastServices []string
		for _, svc := range pastServicePatterns {
			if strings.Contains(userMessages, svc.pattern) {
				found := false
				for _, existing := range pastServices {
					if strings.EqualFold(existing, svc.name) {
						found = true
						break
					}
				}
				if !found {
					pastServices = append(pastServices, svc.name)
				}
			}
		}
		if len(pastServices) > 0 {
			prefs.PastServices = strings.Join(pastServices, ", ")
			hasPreferences = true
		}
	}

	// --- Service interest (config-driven + fallback) ---
	allMessages := userMessages
	for _, msg := range history {
		if msg.Role == ChatRoleAssistant {
			allMessages += strings.ToLower(msg.Content) + " "
		}
	}

	// First check user messages, then fall back to full conversation context
	if svc := matchService(userMessages, serviceAliases); svc != "" {
		prefs.ServiceInterest = svc
		hasPreferences = true
	} else if svc := matchService(allMessages, serviceAliases); svc != "" {
		prefs.ServiceInterest = svc
		hasPreferences = true
	}

	// --- Day preferences ---
	if strings.Contains(userMessages, "weekday") {
		prefs.PreferredDays = "weekdays"
		hasPreferences = true
	} else if strings.Contains(userMessages, "weekend") {
		prefs.PreferredDays = "weekends"
		hasPreferences = true
	} else if strings.Contains(userMessages, "any day") || strings.Contains(userMessages, "flexible") || strings.Contains(userMessages, "anytime") || strings.Contains(userMessages, "whenever") || strings.Contains(userMessages, "open schedule") {
		prefs.PreferredDays = "any"
		hasPreferences = true
	} else if strings.Contains(userMessages, "monday") || strings.Contains(userMessages, "tuesday") || strings.Contains(userMessages, "wednesday") || strings.Contains(userMessages, "thursday") || strings.Contains(userMessages, "friday") {
		days := []string{}
		for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			if strings.Contains(userMessages, day) {
				days = append(days, day)
			}
		}
		if len(days) > 0 {
			prefs.PreferredDays = strings.Join(days, ", ")
			hasPreferences = true
		}
	}

	// --- Time preferences ---
	rangeMatched := false
	for _, re := range []*regexp.Regexp{timeRangeRE, betweenRE} {
		if m := re.FindStringSubmatch(userMessages); len(m) >= 6 {
			endAMPM := strings.ToLower(strings.ReplaceAll(m[5], ".", ""))
			if endAMPM == "a" {
				endAMPM = "am"
			} else if endAMPM == "p" {
				endAMPM = "pm"
			}
			startHour := m[1]
			startMin := m[2]
			endHour := m[3]
			endMin := m[4]
			startStr := startHour
			if startMin != "" {
				startStr += ":" + startMin
			}
			startStr += endAMPM
			endStr := endHour
			if endMin != "" {
				endStr += ":" + endMin
			}
			endStr += endAMPM
			prefs.PreferredTimes = "after " + startStr + ", before " + endStr
			hasPreferences = true
			rangeMatched = true
			break
		}
	}

	if !rangeMatched {
		if matches := specificTimeRE.FindAllStringSubmatch(userMessages, -1); len(matches) > 0 {
			times := []string{}
			for _, match := range matches {
				qualifier := strings.TrimSpace(strings.ToLower(match[1]))
				hour := match[2]
				minutes := match[3]
				ampm := strings.ToLower(strings.ReplaceAll(match[4], ".", ""))
				if ampm == "a" {
					ampm = "am"
				} else if ampm == "p" {
					ampm = "pm"
				}
				timeStr := ""
				if qualifier == "after" || qualifier == "before" {
					timeStr = qualifier + " "
				}
				timeStr += hour
				if minutes != "" {
					timeStr += ":" + minutes
				}
				timeStr += ampm
				times = append(times, timeStr)
			}
			if len(times) > 0 {
				prefs.PreferredTimes = strings.Join(times, ", ")
				hasPreferences = true
			}
		}
	}

	// General time preferences fallback
	if prefs.PreferredTimes == "" {
		if noonRE.MatchString(userMessages) {
			prefs.PreferredTimes = "noon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "morning") {
			prefs.PreferredTimes = "morning"
			hasPreferences = true
		} else if strings.Contains(userMessages, "afternoon") {
			prefs.PreferredTimes = "afternoon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "evening") || strings.Contains(userMessages, "after work") || strings.Contains(userMessages, "late") {
			prefs.PreferredTimes = "evening"
			hasPreferences = true
		} else if strings.Contains(userMessages, "anytime") || strings.Contains(userMessages, "any time") || strings.Contains(userMessages, "flexible") || strings.Contains(userMessages, "whenever") || strings.Contains(userMessages, "doesn't matter") || strings.Contains(userMessages, "don't care") || strings.Contains(userMessages, "works for me") || strings.Contains(userMessages, "i'm free") || strings.Contains(userMessages, "i am free") || strings.Contains(userMessages, "open schedule") {
			prefs.PreferredTimes = "flexible"
			hasPreferences = true
		}
	}

	// Short-reply fallback for schedule
	if prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
		if schedPref := scheduleFromShortReply(history); schedPref != "" {
			prefs.PreferredDays = "any"
			prefs.PreferredTimes = schedPref
			hasPreferences = true
		}
	}

	// --- Provider preference ---
	noPreferencePatterns := []string{
		"no preference", "no provider preference", "don't care", "doesn't matter",
		"either is fine", "either one", "anyone", "any provider", "whoever",
		"whoever is available", "no pref", "don't have a preference",
	}
	for _, pat := range noPreferencePatterns {
		if strings.Contains(userMessages, pat) {
			prefs.ProviderPreference = "no preference"
			hasPreferences = true
			break
		}
	}
	if prefs.ProviderPreference == "" {
		prefs.ProviderPreference = providerPreferenceFromReply(history)
	}
	if prefs.ProviderPreference == "" {
		prefs.ProviderPreference = matchProviderNameInText(userMessages, history)
	}

	return prefs, hasPreferences
}
