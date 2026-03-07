package clinic

import (
	"sort"
	"strings"
)

// normalizeServiceKey lowercases and trims whitespace from a service name for map lookups.
func normalizeServiceKey(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}

// ResolveServiceName translates a patient-facing service name (e.g. "Botox") into the
// booking-platform search term using the clinic's ServiceAliases map. If no alias is
// configured the original name is returned unchanged.
func (c *Config) ResolveServiceName(service string) string {
	if c == nil || len(c.ServiceAliases) == 0 {
		return service
	}
	key := normalizeServiceKey(service)
	if alias, ok := c.ServiceAliases[key]; ok && alias != "" {
		return alias
	}
	// Try without trailing 's' (handle plurals like "lip fillers" → "lip filler")
	if strings.HasSuffix(key, "s") {
		if alias, ok := c.ServiceAliases[strings.TrimSuffix(key, "s")]; ok && alias != "" {
			return alias
		}
	}
	// Fuzzy match: check if the service contains an alias key or vice versa.
	// Prefer the longest matching key to avoid "filler" matching before "lip filler".
	bestAlias := ""
	bestKeyLen := 0
	for aliasKey, alias := range c.ServiceAliases {
		if alias == "" {
			continue
		}
		if strings.Contains(key, aliasKey) || strings.Contains(aliasKey, key) {
			if len(aliasKey) > bestKeyLen {
				bestAlias = alias
				bestKeyLen = len(aliasKey)
			}
		}
	}
	if bestAlias != "" {
		return bestAlias
	}
	return service
}

// GetServiceVariants returns the delivery variants for a service, if any.
// For example, "weight loss" might return ["Weight Loss Consultation - In Person", "Weight Loss Consultation - Virtual"].
// Returns nil if the service has no variants configured.
func (c *Config) GetServiceVariants(service string) []string {
	if c == nil || len(c.ServiceVariants) == 0 {
		return nil
	}
	key := normalizeServiceKey(service)
	// Exact match first
	if variants, ok := c.ServiceVariants[key]; ok && len(variants) > 1 {
		return variants
	}
	// Fuzzy match: check if the service contains a variant key or vice versa
	// e.g. "weight loss consultation" contains "weight loss"
	for variantKey, variants := range c.ServiceVariants {
		if len(variants) <= 1 {
			continue
		}
		// If the service IS one of the variants, it's already resolved — don't re-ask
		for _, v := range variants {
			if strings.EqualFold(normalizeServiceKey(v), key) {
				return nil
			}
		}
		if strings.Contains(key, variantKey) || strings.Contains(variantKey, key) {
			return variants
		}
	}
	return nil
}

// ResolveProviderID returns the Moxie userMedspaId for a provider name (case-insensitive partial match).
// Returns "" if no match found.
func (c *Config) ResolveProviderID(providerName string) string {
	if c.MoxieConfig == nil || c.MoxieConfig.ProviderNames == nil {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(providerName))
	if lower == "" || lower == "no preference" {
		return ""
	}
	// Exact match first
	for id, name := range c.MoxieConfig.ProviderNames {
		if strings.ToLower(name) == lower {
			return id
		}
	}
	// Partial match (first name only)
	for id, name := range c.MoxieConfig.ProviderNames {
		parts := strings.Fields(strings.ToLower(name))
		if len(parts) > 0 && parts[0] == lower {
			return id
		}
	}
	return ""
}

// ServiceNeedsProviderPreference returns true if the given service (by normalized name)
// has more than one eligible provider.
func (c *Config) ServiceNeedsProviderPreference(serviceName string) bool {
	if c.MoxieConfig == nil || c.MoxieConfig.ServiceProviderCount == nil {
		return false
	}
	itemID := c.ServiceMenuItemID(serviceName)
	if itemID == "" {
		return false
	}
	return c.MoxieConfig.ServiceProviderCount[itemID] > 1
}

// ServiceMenuItemID resolves a patient-facing service name to a Moxie serviceMenuItemId.
func (c *Config) ServiceMenuItemID(serviceName string) string {
	if c.MoxieConfig == nil || c.MoxieConfig.ServiceMenuItems == nil {
		return ""
	}
	resolved := c.ResolveServiceName(serviceName)
	itemID := c.MoxieConfig.ServiceMenuItems[strings.ToLower(resolved)]
	if itemID == "" {
		itemID = c.MoxieConfig.ServiceMenuItems[strings.ToLower(serviceName)]
	}
	return itemID
}

// ProviderNamesForService returns deterministic provider display names for a service.
// Returns an empty (non-nil) slice unless we have an explicit service->provider mapping
// with resolvable names.
func (c *Config) ProviderNamesForService(serviceName string) []string {
	empty := []string{}
	if c.MoxieConfig == nil || c.MoxieConfig.ProviderNames == nil || c.MoxieConfig.ServiceProviders == nil {
		return empty
	}

	itemID := c.ServiceMenuItemID(serviceName)
	if itemID == "" {
		return empty
	}
	ids := c.MoxieConfig.ServiceProviders[itemID]
	if len(ids) == 0 {
		return empty
	}

	names := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		name := strings.TrimSpace(c.MoxieConfig.ProviderNames[id])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// UsesManualHandoff returns true if the clinic should use the manual handoff adapter.
// This is the default when no EMR/booking platform is configured.
func (c *Config) UsesManualHandoff() bool {
	if c == nil {
		return true
	}
	adapter := strings.ToLower(c.BookingAdapter)
	if adapter == "manual" {
		return true
	}
	if adapter == "" && !c.UsesMoxieBooking() {
		return true
	}
	return false
}

// ResolvedBookingAdapter returns the effective booking adapter name.
func (c *Config) ResolvedBookingAdapter() string {
	if c == nil {
		return "manual"
	}
	if adapter := strings.ToLower(c.BookingAdapter); adapter != "" {
		return adapter
	}
	if platform := strings.ToLower(c.BookingPlatform); platform != "" {
		return platform
	}
	return "manual"
}

// UsesMoxieBooking returns true if the clinic is configured to use Moxie for booking.
// When true, Square is NOT used — the patient completes payment on Moxie's Step 5 page.
func (c *Config) UsesMoxieBooking() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.BookingPlatform) == "moxie"
}

// UsesVagaroBooking returns true if the clinic is configured to use Vagaro for booking.
func (c *Config) UsesVagaroBooking() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.BookingPlatform) == "vagaro"
}

// UsesBoulevardBooking returns true if the clinic is configured for Boulevard booking.
func (c *Config) UsesBoulevardBooking() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.BookingPlatform) == "boulevard"
}

// UsesBookingAPI returns true if the clinic uses any API-based booking
// (Moxie or Boulevard) that supports real-time availability lookup.
func (c *Config) UsesBookingAPI() bool {
	return c.UsesMoxieBooking() || c.UsesBoulevardBooking()
}

// UsesStripePayment returns true if the clinic is configured to use Stripe for deposit collection.
func (c *Config) UsesStripePayment() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.PaymentProvider) == "stripe"
}

// UsesSquarePayment returns true if the clinic uses Square for deposit collection.
// This is the default when no booking platform is specified. Mutually exclusive
// with Moxie — a clinic uses one or the other, never both.
func (c *Config) UsesSquarePayment() bool {
	if c == nil {
		return true // Default to Square
	}
	platform := strings.ToLower(c.BookingPlatform)
	return platform == "" || platform == "square"
}

// DepositAmountForService returns the configured deposit amount (in cents) for a service,
// falling back to the clinic default when no override is present.
func (c *Config) DepositAmountForService(service string) int {
	if c == nil {
		return 0
	}
	key := normalizeServiceKey(service)
	if key != "" && c.ServiceDepositAmountCents != nil {
		if amount, ok := c.ServiceDepositAmountCents[key]; ok && amount > 0 {
			return amount
		}
	}
	if c.DepositAmountCents > 0 {
		return c.DepositAmountCents
	}
	return 0
}

// PriceTextForService returns a configured price string for a service when available.
func (c *Config) PriceTextForService(service string) (string, bool) {
	if c == nil || c.ServicePriceText == nil {
		return "", false
	}
	key := normalizeServiceKey(service)
	if key == "" {
		return "", false
	}
	price := strings.TrimSpace(c.ServicePriceText[key])
	if price == "" {
		return "", false
	}
	return price, true
}
