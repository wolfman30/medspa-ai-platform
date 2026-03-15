package conversation

import "strings"

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
	{"wrinkle", "wrinkle relaxer"},
	{"anti-aging", "wrinkle relaxer"},
	{"fine lines", "wrinkle relaxer"},
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

// ---------- concern-based service categories ----------

// concernBasedServices maps concern categories to the booking service and a provider note.
// When a patient describes a concern (not a specific treatment), we book under the
// right service category but attach a note for the provider.
var concernBasedServices = map[string]struct {
	bookingService string // the actual service to search availability for
	providerNote   string // note to attach to the booking
}{
	"wrinkle relaxer": {
		bookingService: "Botox", // search availability under Botox (covers Botox/Dysport/Xeomin category)
		providerNote:   "Patient wants to address wrinkles — needs provider guidance on best option (Botox, Dysport, or Xeomin).",
	},
}

// isConcernBasedService returns true if the service interest is a concern category
// rather than a specific treatment.
func isConcernBasedService(service string) bool {
	_, ok := concernBasedServices[strings.ToLower(service)]
	return ok
}

// ResolveConcernToBookingService returns the actual bookable service name for a
// concern-based category, plus a provider note. Returns empty strings if not concern-based.
func ResolveConcernToBookingService(service string) (bookingService, providerNote string) {
	if info, ok := concernBasedServices[strings.ToLower(service)]; ok {
		return info.bookingService, info.providerNote
	}
	return "", ""
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
				// Return the alias key (patient-facing term) with title case,
				// not the resolved Moxie service name. ResolveServiceName()
				// handles the Moxie lookup downstream.
				return strings.Title(p.pattern) //nolint:staticcheck
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
