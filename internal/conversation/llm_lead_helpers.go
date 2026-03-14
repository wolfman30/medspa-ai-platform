package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// extractAndSavePreferences extracts scheduling preferences from conversation history and saves them.
func (s *LLMService) extractAndSavePreferences(ctx context.Context, leadID string, history []ChatMessage) error {
	return s.savePreferencesFromHistory(ctx, leadID, history, true)
}

// savePreferencesFromHistory parses scheduling preferences from conversation
// history and persists them. When addNote is true, a timestamp note is appended.
func (s *LLMService) savePreferencesFromHistory(ctx context.Context, leadID string, history []ChatMessage, addNote bool) error {
	if s == nil || s.leadsRepo == nil || strings.TrimSpace(leadID) == "" {
		return nil
	}
	prefs, ok := extractPreferences(history, nil)
	if !ok {
		return nil
	}
	if addNote {
		prefs.Notes = fmt.Sprintf("Auto-extracted from conversation at %s", time.Now().Format(time.RFC3339))
	}
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, prefs)
}

// savePreferencesNoNote silently saves preferences without a note, logging
// any errors at warn level with the given reason.
func (s *LLMService) savePreferencesNoNote(ctx context.Context, leadID string, history []ChatMessage, reason string) {
	if s == nil {
		return
	}
	if err := s.savePreferencesFromHistory(ctx, leadID, history, false); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", leadID, "reason", reason, "error", err)
		}
	}
}

// appendLeadNote appends a note to the lead's scheduling notes, avoiding duplicates.
func (s *LLMService) appendLeadNote(ctx context.Context, orgID, leadID, note string) {
	if s == nil || s.leadsRepo == nil {
		return
	}
	orgID = strings.TrimSpace(orgID)
	leadID = strings.TrimSpace(leadID)
	note = strings.TrimSpace(note)
	if orgID == "" || leadID == "" || note == "" {
		return
	}
	lead, err := s.leadsRepo.GetByID(ctx, orgID, leadID)
	if err != nil || lead == nil {
		return
	}
	existing := strings.TrimSpace(lead.SchedulingNotes)
	switch {
	case existing == "":
		existing = note
	case strings.Contains(existing, note):
		// Avoid duplication.
	default:
		existing = existing + " | " + note
	}
	_ = s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, leads.SchedulingPreferences{Notes: existing})
}

// isCapitalized checks if a string starts with an uppercase letter.
func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// formatLeadPreferenceContext builds a system message summarizing known lead
// preferences (name, service, patient type, preferred days/times) so the
// assistant avoids re-asking for already-captured information.
func formatLeadPreferenceContext(lead *leads.Lead) string {
	if lead == nil {
		return ""
	}
	lines := make([]string, 0, 5)
	name := strings.TrimSpace(lead.Name)
	if name != "" && !looksLikePhone(name, lead.Phone) {
		label := "Name"
		if len(strings.Fields(name)) == 1 {
			label = "Name (first only)"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, name))
	}
	service := strings.TrimSpace(lead.ServiceInterest)
	if service != "" {
		lines = append(lines, fmt.Sprintf("- Service: %s", service))
	}
	patientType := strings.TrimSpace(lead.PatientType)
	if patientType != "" {
		lines = append(lines, fmt.Sprintf("- Patient type: %s", patientType))
	}
	days := strings.TrimSpace(lead.PreferredDays)
	if days != "" {
		lines = append(lines, fmt.Sprintf("- Preferred days: %s", days))
	}
	times := strings.TrimSpace(lead.PreferredTimes)
	if times != "" {
		lines = append(lines, fmt.Sprintf("- Preferred times: %s", times))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Known scheduling preferences from earlier messages:\n" + strings.Join(lines, "\n")
}

// looksLikePhone returns true if the name appears to be a phone number
// (matches the lead's phone or contains 7+ digits).
func looksLikePhone(name string, phone string) bool {
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" {
		return false
	}
	if phone != "" && name == phone {
		return true
	}
	digits := 0
	for i := 0; i < len(name); i++ {
		if name[i] >= '0' && name[i] <= '9' {
			digits++
		}
	}
	return digits >= 7
}

// splitName splits a full name into first and last name.
// "Andy Wolf" → ("Andy", "Wolf"), "Madonna" → ("Madonna", ""), "  " → ("", "").
func splitName(full string) (string, string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], strings.Join(parts[1:], " ")
	}
}
