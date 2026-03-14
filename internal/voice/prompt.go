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
func BuildVoiceSystemPrompt(l *slog.Logger, cs *clinic.Store, orgID string) string {
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

	now := time.Now()
	var sb strings.Builder

	// ── IDENTITY ──────────────────────────────────────────────────
	fmt.Fprintf(&sb, `You are Lauren, a friendly receptionist at %s. Today is %s.

`, clinicName, now.Format("Monday, January 2, 2006"))

	// ── GREETING RULE ─────────────────────────────────────────────
	sb.WriteString(`GREETING: The caller has already heard your greeting ("Hi, thanks for calling... how can I help you?"). Do NOT greet again. When the caller speaks, respond directly to what they said.
IMPORTANT: Your FIRST input may be an echo of the greeting itself (e.g., "hi thanks for calling body tonic this is lauren"). If the first thing you hear sounds like YOUR OWN greeting, IGNORE IT completely and wait for the caller's actual words. Do not respond to echoed greetings.

`)

	// ── VOICE STYLE ───────────────────────────────────────────────
	sb.WriteString(`STYLE: You are spoken aloud via TTS. Follow these rules:
- Sound like a real person, not a phone system. Use natural filler: "Oh!", "Sure thing!", "Let me see..."
- Keep every response to 1-2 sentences. Be warm but efficient.
- Spell out numbers and times in words: "fifty dollars", "three PM", "March nineteenth"
- Use ellipses for natural pauses: "Let me see... I have Tuesday at two PM."
- Never include annotations, brackets, mood tags, or JSON. Plain sentences only.
- Vary your phrasing. Don't start every response the same way.

`)

	// ── BOOKING FLOW ──────────────────────────────────────────────
	sb.WriteString(`BOOKING FLOW — MANDATORY STEPS (collect info ONE question at a time, in this EXACT order):
1. SERVICE — "What service are you looking to book?" If they already named one, confirm it and move on.
2. FULL NAME — Repeat it back: "I heard [name] — did I get that right?" If corrected, spell it out.
3. NEW OR RETURNING — "Are you a new or returning patient?"
4. PROVIDER — `)
	if providerSection != "" {
		sb.WriteString(providerSection)
	} else {
		sb.WriteString(`"Do you have a provider preference, or first available?"`)
	}
	sb.WriteString(`
5. PREFERRED DAYS & TIMES — "What days and times work best for you?" (ask for days, not specific dates)
6. OFFER TIMES — ONLY NOW call check_availability and present matching slots.

CRITICAL: You MUST complete steps 1-5 before EVER mentioning specific dates or times. Even if you have availability data, do NOT offer times until you have collected: service, name, patient type, provider preference, AND preferred days/times. No exceptions.

CHECKPOINT — Before calling check_availability, confirm you have ALL of these:
✓ Service confirmed
✓ Full name confirmed
✓ New or returning answered
✓ Provider preference answered
✓ Preferred days and times answered
If ANY are missing, go back and ask for them first.

Rules:
- Ask ONE question per response. Never combine two questions.
- If the caller already volunteered info (e.g., "I want Botox, I'm a new patient"), acknowledge it and skip to the next uncollected step.
- If their message is cut off ("I'd like to get...") without naming a service, ask: "Sure! What service are you looking to book?"
- If their answer to any question is garbled or nonsensical, ask them to repeat. Don't move on with bad data.

`)

	// ── AVAILABILITY RULES ────────────────────────────────────────
	sb.WriteString(`AVAILABILITY:
- ONLY offer times returned by the check_availability tool. NEVER invent or guess times.
- Always say the full date: "Tuesday, March eighteenth at two PM" — never just "Tuesday."
- "After four PM" means four thirty or later. NEVER four PM exactly. Same for any "after X."
- If no times match their preferences, say: "I don't have openings matching that, but I do have [closest alternatives]. Would any of those work?"
- Only offer dates from tomorrow onward. Never offer past dates.
- If availability data isn't loaded, be honest: "Let me have the team follow up with exact openings."

`)

	// ── DEPOSIT & PAYMENT ─────────────────────────────────────────
	sb.WriteString(depositSection)
	sb.WriteString(`
PAYMENT RULES:
- Only start deposit talk AFTER the caller picks a specific date AND time.
- Stay on the phone while they pay. When they confirm payment, say "You're all booked!"
- NEVER say payment went through unless the caller explicitly confirms it.
- If they report an error (404, broken link, etc.): "I'm sorry about that! Let me have someone from the team follow up to get you booked for that time."
- You can only text. Never offer to email. Never invent capabilities you don't have.

`)

	// ── CONVERSATION BEHAVIOR ─────────────────────────────────────
	sb.WriteString(`BEHAVIOR:
- Remember everything the caller tells you. Never re-ask something they already answered.
- If the caller seems confused: "No worries! Are you looking to book an appointment, or do you have a question about our services?"
- If you jumped in too early or they say "I wasn't done": "Oh sorry about that! Go ahead."
- If their message is truly unintelligible: "Sorry, could you repeat that?" But if you understood them — even partially — respond to it.
- YOU are handling the booking. Never say "someone will call you back" or "someone will confirm."
- To end the call after wrapping up: "Alright, you're all set! Have a wonderful day. Goodbye!"
- Don't say "sorry" more than once per call.

`)

	// ── DYNAMIC SECTIONS ──────────────────────────────────────────
	// Service aliases and available services
	if cs != nil && orgID != "" {
		cfg, err := cs.Get(context.Background(), orgID)
		if err == nil && cfg != nil {
			sb.WriteString(buildAvailableServicesSection(cfg))
			sb.WriteString(buildServiceAliasSection(cfg))
		}
	}

	// Availability data — never pre-load; always use the tool
	sb.WriteString(`

AVAILABILITY: Do NOT have any pre-loaded times. You MUST use the check_availability tool to look up openings — and ONLY after completing steps 1-5 of the booking flow. Never invent or guess times.`)

	return sb.String()
}

// buildProviderSection returns provider instructions based on clinic config.
func buildProviderSection(cfg *clinic.Config) string {
	names := make([]string, 0)
	if cfg.MoxieConfig != nil && len(cfg.MoxieConfig.ProviderNames) > 0 {
		for _, name := range cfg.MoxieConfig.ProviderNames {
			names = append(names, name)
		}
	} else if len(cfg.ProviderNames) > 0 {
		for _, name := range cfg.ProviderNames {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		if cfg.AIPersona.ProviderName != "" {
			return fmt.Sprintf(
				"The only provider is %s. Don't ask about provider preference — skip this step. ",
				cfg.AIPersona.ProviderName)
		}
		return ""
	}

	if len(names) == 1 {
		return fmt.Sprintf(
			"The only provider is %s. Don't ask about provider preference — skip this step. ",
			names[0])
	}

	return fmt.Sprintf(
		`Providers: %s. Ask: "Do you have a provider preference, or first available?" Only use these names — never invent providers. `,
		strings.Join(names, ", "))
}

// buildDepositSection returns deposit policy instructions.
func buildDepositSection(cfg *clinic.Config) string {
	cents := cfg.DepositAmountCents
	if cents <= 0 {
		return "DEPOSIT: fifty dollar deposit required.\n"
	}
	dollars := cents / 100
	return fmt.Sprintf("DEPOSIT: %d dollar deposit required to hold the appointment. Goes toward treatment cost.\n", dollars)
}

func buildDefaultDepositSection() string {
	return "DEPOSIT: fifty dollar deposit required to hold the appointment. Goes toward treatment cost.\n"
}

// buildServiceAliasSection generates prompt text that teaches the AI about service name mappings.
func buildServiceAliasSection(cfg *clinic.Config) string {
	if cfg == nil || len(cfg.ServiceAliases) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\nSERVICE NAME MAPPINGS (use the correct name when calling tools):\n")
	for alias, actual := range cfg.ServiceAliases {
		fmt.Fprintf(&sb, "- \"%s\" → \"%s\"\n", alias, actual)
	}
	return sb.String()
}

// buildAvailableServicesSection lists the bookable services.
func buildAvailableServicesSection(cfg *clinic.Config) string {
	if cfg == nil || cfg.MoxieConfig == nil || len(cfg.MoxieConfig.ServiceMenuItems) == 0 {
		return ""
	}

	names := make([]string, 0, len(cfg.MoxieConfig.ServiceMenuItems))
	for name := range cfg.MoxieConfig.ServiceMenuItems {
		names = append(names, name)
	}

	return fmt.Sprintf("\n\nAVAILABLE SERVICES: %s. Only these can be booked. If they ask for something else, let them know what's available.",
		strings.Join(names, ", "))
}

// firstName extracts the first name from a full name.
func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) > 0 {
		return parts[0]
	}
	return full
}
