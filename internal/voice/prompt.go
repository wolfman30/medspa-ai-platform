package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

	// Current date awareness (bug #2: prevent offering past dates)
	now := time.Now()
	fmt.Fprintf(&sb, "Today's date is %s. NEVER offer appointment dates in the past. Only offer dates from tomorrow onward. ", now.Format("Monday, January 2, 2006"))

	// Time filtering rules (bugs #3, #4: respect "after X" constraints)
	sb.WriteString(`When a caller says "after X PM", ONLY offer times STRICTLY AFTER that hour. "After 4 PM" means 4:30 PM or later, NEVER 4:00 PM exactly. `)
	sb.WriteString("If the caller corrects you on timing, acknowledge the correction and only offer times matching their updated preference. ")

	// Core identity
	fmt.Fprintf(&sb, "You are Lauren, a friendly receptionist at %s. You speak casually and naturally, like a real person — not like a corporate phone system. ", clinicName)
	sb.WriteString("IMPORTANT: The caller has ALREADY been greeted. You already said hi. Do NOT introduce yourself or greet them again. Jump straight into helping with whatever they need. ")
	sb.WriteString("CRITICAL: Never include any annotations, mood tags, stage directions, or JSON in your responses. No [warm], [excited], [empathetic], {interrupted}, or any bracketed/braced text. Just speak plain natural sentences. ")
	sb.WriteString("Keep ALL responses brief — 1-2 sentences max. Be warm but efficient. ")
	sb.WriteString("PATIENCE IS CRITICAL: If the caller's message seems incomplete, cut off, or doesn't make clear sense, do NOT assume what they meant. Instead say something like 'Sorry, I missed that — what were you saying?' or 'Go ahead, I'm listening.' NEVER rush to answer a partial or unclear statement. Wait for a complete thought before responding. ")
	sb.WriteString("If the caller says you jumped in too early or 'I didn't say anything yet', apologize briefly and let them speak: 'Oh sorry about that! Go ahead.' ")

	// ElevenLabs v3 TTS prompting instructions
	sb.WriteString("\n\nSPEECH DELIVERY INSTRUCTIONS (critical — your text output is spoken aloud via TTS): ")
	sb.WriteString("Sound like a REAL human receptionist. Use natural filler: 'Oh!', 'Hmm, let me check...', 'Sure thing!', 'Of course!' ")
	sb.WriteString("Use ellipses (...) for natural pauses: 'Let me see... I have Tuesday at 2 PM.' ")
	sb.WriteString("NEVER sound robotic or overly formal. Vary your responses — don't start every sentence the same way. ")
	sb.WriteString("NEVER use any tags, brackets, JSON, or annotations. Just speak plain natural sentences. ")
	sb.WriteString("Spell out all numbers, prices, and times in words: say 'fifty dollars' not '$50', say 'three PM' not '3 PM'. ")
	sb.WriteString("DEPOSIT FLOW: After the caller picks a time, keep it SHORT: confirm the time, mention the fifty dollar deposit briefly, and say you'll text them a link. Example: 'Thursday March 19th at 5 PM — great! There's a fifty dollar deposit to hold your spot, and I'll text you a secure link right now.' Do NOT read the full cancellation policy unless they ask. Do NOT say 'Perfect!' before the caller has confirmed their choice. WAIT for them to pick a time, THEN confirm. Do NOT say 'someone will confirm' or 'someone will call you back' — YOU are handling the booking. ")

	// Provider info
	sb.WriteString(providerSection)

	// Qualification flow
	sb.WriteString("When booking appointments, collect information ONE QUESTION AT A TIME in this order: ")
	sb.WriteString("1) What service they want, ")
	sb.WriteString("2) Their full name (repeat it back to confirm), ")
	sb.WriteString("3) Whether they're a new or returning patient, ")
	sb.WriteString("4) Their preferred DAYS and TIMES (not dates — say 'What days and times work best for you?'). ")
	sb.WriteString("CRITICAL: Ask only ONE question per response. Do NOT combine questions like 'What's your name and are you new or returning?' — ask for the name first, wait for the answer, THEN ask if they're new or returning. ")

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
		sb.WriteString("\n\nNote: Availability data is not pre-loaded. When the caller asks about times, use the check_availability tool to look up real-time availability. Do NOT make up times or dates.")
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
			"'Perfect! I'll send you a text with a secure deposit link right now.' "+
			"Then call the send_sms tool with a message containing the deposit link. ",
		dollars)
}

func buildDefaultDepositSection() string {
	return "DEPOSIT POLICY: We require a $50 deposit to secure your appointment. " +
		"The deposit goes toward your treatment cost. " +
		"If you cancel 24 hours or more in advance, you'll receive a full refund. " +
		"If you don't show up, the deposit is forfeited. " +
		"After the caller picks a time, inform them about the deposit and say: 'Perfect! I'll send you a text with a secure deposit link right now.' Then call the send_sms tool. "
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
