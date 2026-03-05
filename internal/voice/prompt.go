package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

// BuildVoiceSystemPrompt constructs a dynamic system prompt for voice calls
// based on the clinic's configuration (providers, deposit policy, etc.).
func BuildVoiceSystemPrompt(l *slog.Logger, cs *clinic.Store, orgID, availabilitySummary string) string {
	// Defaults for unknown clinics
	clinicName := "our clinic"
	providerSection := ""
	depositSection := buildDefaultDepositSection()

	if cs != nil && orgID != "" {
		cfg, err := cs.Get(context.Background(), orgID)
		if err != nil {
			l.Warn("voice-prompt: could not load clinic config", "org_id", orgID, "error", err)
		} else if cfg != nil {
			if cfg.Name != "" {
				clinicName = cfg.Name
			}
			providerSection = buildProviderSection(cfg)
			depositSection = buildDepositSection(cfg)
		}
	}

	var sb strings.Builder

	// Core identity
	fmt.Fprintf(&sb, "You are a friendly and professional AI receptionist for a medical spa called %s. ", clinicName)
	fmt.Fprintf(&sb, "IMPORTANT: You must greet the caller FIRST. Say something like 'Hi there! Thanks for calling %s, how can I help you today?' ", clinicName)
	sb.WriteString("Then wait for the caller to respond. ")
	sb.WriteString("Keep ALL responses brief — 1-2 sentences max. Be warm but efficient. ")

	// Provider info
	sb.WriteString(providerSection)

	// Qualification flow
	sb.WriteString("When booking appointments, collect information in this order: ")
	sb.WriteString("1) What service they want, ")
	sb.WriteString("2) Their full name (repeat it back to confirm), ")
	sb.WriteString("3) Whether they're a new or returning patient, ")
	sb.WriteString("4) Their preferred DAYS and TIMES (not dates — say 'What days and times work best for you?'). ")

	// Deposit policy
	sb.WriteString(depositSection)

	// Conversation behavior
	sb.WriteString("REMEMBER everything the caller tells you throughout the conversation. Do not ask for information they already provided. ")
	sb.WriteString("NEVER say 'sorry' or 'I apologize' more than once in a conversation. If you don't have information, say 'I don't have that right now' and move on. ")
	sb.WriteString("When the caller asks about availability or you have enough info to suggest times, ")
	sb.WriteString("SPEAK the available times directly WITH THE FULL DATE — for example: ")
	sb.WriteString("'I have openings on Tuesday, March 4th at 2 PM, Wednesday, March 5th at 10 AM, and Thursday, March 6th at 4 PM. Which works best for you?' ")
	sb.WriteString("ALWAYS include both the day of week AND the date (month and day number). ")
	sb.WriteString("Never say just 'Tuesday' without the date. Be specific. Do NOT say 'let me check' — use the availability data below.")

	// Service alias mappings and available services
	if cs != nil && orgID != "" {
		cfg, err := cs.Get(context.Background(), orgID)
		if err == nil && cfg != nil {
			sb.WriteString(buildAvailableServicesSection(cfg))
			sb.WriteString(buildServiceAliasSection(cfg))
		}
	}

	// Availability data
	if availabilitySummary != "" {
		sb.WriteString("\n\nCURRENT AVAILABILITY:\n")
		sb.WriteString(availabilitySummary)
	} else {
		sb.WriteString("\n\nNote: Availability data is not loaded. If asked about times, say 'I apologize, I'm having trouble accessing our schedule right now. Can I take your information and have someone call you back with available times?'")
	}

	return sb.String()
}

// buildProviderSection returns provider instructions based on clinic config.
func buildProviderSection(cfg *clinic.Config) string {
	if cfg.MoxieConfig == nil || len(cfg.MoxieConfig.ProviderNames) == 0 {
		// Single provider from legacy AIPersona field
		if cfg.AIPersona.ProviderName != "" {
			return fmt.Sprintf(
				"PROVIDERS: The only provider at %s is %s. "+
					"Do NOT make up provider names — there is no Dr. Smith or anyone else. Always use '%s'. "+
					"Since there is only one provider, do NOT ask about provider preference. ",
				cfg.Name, cfg.AIPersona.ProviderName, firstName(cfg.AIPersona.ProviderName))
		}
		return "Do NOT make up provider names like 'Dr. Smith'. If you don't know the provider names, say 'one of our providers'. "
	}

	names := make([]string, 0, len(cfg.MoxieConfig.ProviderNames))
	for _, name := range cfg.MoxieConfig.ProviderNames {
		names = append(names, name)
	}

	if len(names) == 1 {
		name := names[0]
		return fmt.Sprintf(
			"PROVIDERS: The only provider at %s is %s. "+
				"Do NOT make up provider names — there is no Dr. Smith or anyone else. Always use '%s'. "+
				"Since there is only one provider, do NOT ask about provider preference. ",
			cfg.Name, name, firstName(name))
	}

	return fmt.Sprintf(
		"PROVIDERS at %s: %s. "+
			"Do NOT make up provider names — only use the names listed above. "+
			"Ask which provider the patient prefers if the requested service has multiple providers. ",
		cfg.Name, strings.Join(names, ", "))
}

// buildDepositSection returns deposit policy instructions.
func buildDepositSection(cfg *clinic.Config) string {
	cents := cfg.DepositAmountCents
	if cents <= 0 {
		return ""
	}
	dollars := cents / 100
	return fmt.Sprintf(
		"DEPOSIT POLICY: We require a $%d deposit to secure your appointment. "+
			"The deposit goes toward your treatment cost. "+
			"If you cancel 24 hours or more in advance, you'll receive a full refund. "+
			"If you don't show up, the deposit is forfeited. "+
			"After the caller picks a time, inform them about the deposit and tell them: "+
			"'After we hang up, you'll receive a text message with a secure payment link to complete your deposit.' "+
			"Do NOT say 'I'll send it right now' — the text is sent automatically after the call ends. ",
		dollars)
}

func buildDefaultDepositSection() string {
	return "DEPOSIT POLICY: We require a $50 deposit to secure your appointment. " +
		"The deposit goes toward your treatment cost. " +
		"If you cancel 24 hours or more in advance, you'll receive a full refund. " +
		"If you don't show up, the deposit is forfeited. " +
		"After the caller picks a time, inform them about the deposit and say you'll text them a secure payment link. "
}

// buildServiceAliasSection generates prompt text that teaches the AI about service name mappings.
// For example, if a patient says "Botox" but the booking system calls it "Wrinkle Relaxers",
// this section tells the AI to use the correct name when calling tools.
func buildServiceAliasSection(cfg *clinic.Config) string {
	if cfg == nil || len(cfg.ServiceAliases) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\nSERVICE NAME MAPPINGS (use the correct service name when calling tools):\n")
	for alias, actual := range cfg.ServiceAliases {
		fmt.Fprintf(&sb, "- When a patient says '%s', they mean '%s'. Use '%s' when calling tools.\n", alias, actual, actual)
	}
	return sb.String()
}

// buildAvailableServicesSection lists the bookable services from MoxieConfig so the AI knows what's available.
func buildAvailableServicesSection(cfg *clinic.Config) string {
	if cfg == nil || cfg.MoxieConfig == nil || len(cfg.MoxieConfig.ServiceMenuItems) == 0 {
		return ""
	}

	names := make([]string, 0, len(cfg.MoxieConfig.ServiceMenuItems))
	for name := range cfg.MoxieConfig.ServiceMenuItems {
		names = append(names, name)
	}

	var sb strings.Builder
	sb.WriteString("\n\nAVAILABLE BOOKABLE SERVICES: ")
	sb.WriteString(strings.Join(names, ", "))
	sb.WriteString(". Only these services can be booked. If a patient asks for something not listed, let them know what's available.")
	return sb.String()
}

// firstName extracts the first name from a full name.
func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) > 0 {
		return parts[0]
	}
	return full
}
