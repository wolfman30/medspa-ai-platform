// Package main runs comprehensive E2E tests for Adela Medical Spa.
//
// Tests validate the AI booking assistant's behavior for Adela's specific
// service menu, provider roster, and business rules.
//
// Usage:
//
//	ADMIN_JWT_SECRET=... API_BASE_URL=... go run scripts/e2e/adela_e2e.go [scenario-name]
//	ADMIN_JWT_SECRET=... API_BASE_URL=... go run scripts/e2e/adela_e2e.go              # runs all
//	ADMIN_JWT_SECRET=... API_BASE_URL=... go run scripts/e2e/adela_e2e.go happy-path   # runs one
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Constants — Adela Medical Spa
// ---------------------------------------------------------------------------

const (
	testPhone    = "+15005550003" // Telnyx test number (different from Forever 22 tests)
	clinicPhone  = "+13304600937" // Adela's Telnyx number
	orgID        = "4440091b-b73f-49fa-87a2-ae22d0110981"
	convID       = "sms:4440091b-b73f-49fa-87a2-ae22d0110981:15005550003"
	maxWaitSecs  = 90
	pollInterval = 2 * time.Second
)

var (
	apiBase   string
	jwtSecret string
	jwt       string
)

// ---------------------------------------------------------------------------
// Scenario definition
// ---------------------------------------------------------------------------

type scenario struct {
	Name string
	Fn   func(t *T)
}

// T is a lightweight test context for a single scenario.
type T struct {
	passed int
	failed int
	name   string
}

func (t *T) check(name string, ok bool) {
	if ok {
		fmt.Printf("    PASS: %s\n", name)
		t.passed++
	} else {
		fmt.Printf("    FAIL: %s\n", name)
		t.failed++
	}
}

func (t *T) warn(name string, ok bool) {
	if ok {
		fmt.Printf("    PASS: %s\n", name)
		t.passed++
	} else {
		fmt.Printf("    WARN: %s (non-blocking)\n", name)
	}
}

func (t *T) fatalf(format string, args ...interface{}) {
	fmt.Printf("    FATAL: "+format+"\n", args...)
	t.failed++
}

// ---------------------------------------------------------------------------
// Helpers (mirrored from run_e2e.go, adapted for Adela)
// ---------------------------------------------------------------------------

func purge() error {
	url := fmt.Sprintf("%s/admin/clinics/%s/phones/%s", apiBase, orgID, testPhone)
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("purge returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func sendSMS(text string) error {
	ts := time.Now().UnixNano()
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"id":         fmt.Sprintf("e2e-adela-%d", ts),
			"event_type": "message.received",
			"payload": map[string]interface{}{
				"id":        fmt.Sprintf("msg-adela-%d", ts),
				"from":      map[string]string{"phone_number": testPhone},
				"to":        []map[string]string{{"phone_number": clinicPhone}},
				"text":      text,
				"direction": "inbound",
				"type":      "SMS",
			},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(apiBase+"/webhooks/telnyx/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getConversation() (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/admin/orgs/%s/conversations/%s", apiBase, orgID, convID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func waitForStatus(targetStatus string, maxSecs int) (map[string]interface{}, error) {
	deadline := time.Now().Add(time.Duration(maxSecs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		conv, err := getConversation()
		if err != nil {
			continue
		}
		status, _ := conv["status"].(string)
		if status == targetStatus {
			return conv, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for status %q after %ds", targetStatus, maxSecs)
}

func waitForReply(minUserMsgs int, maxSecs int) ([]map[string]interface{}, error) {
	deadline := time.Now().Add(time.Duration(maxSecs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		conv, err := getConversation()
		if err != nil {
			continue
		}
		msgs := getMessages(conv)

		userCount := 0
		for _, m := range msgs {
			if isUserMsg(m) {
				userCount++
			}
		}
		if userCount < minUserMsgs {
			continue
		}

		lastUserIdx := -1
		for i := len(msgs) - 1; i >= 0; i-- {
			if isUserMsg(msgs[i]) {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx < 0 {
			continue
		}

		for i := lastUserIdx + 1; i < len(msgs); i++ {
			content, _ := msgs[i]["content"].(string)
			if !isUserMsg(msgs[i]) && !isAckMessage(content) {
				return msgs, nil
			}
		}
	}
	return nil, fmt.Errorf("timed out waiting for non-ack reply after %ds", maxSecs)
}

func isUserMsg(m map[string]interface{}) bool {
	role, _ := m["role"].(string)
	sender, _ := m["sender"].(string)
	return role == "user" || sender == "user" || sender == "patient"
}

func isAckMessage(content string) bool {
	acks := []string{
		"got it - give me a moment",
		"thanks for reaching out - one moment",
		"thanks! give me a second",
		"got it! let me check",
		"thanks - one moment",
		"got it. one sec",
		"on it - just a moment",
		"checking now",
		"give me a second",
		"thanks! give me a sec",
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	for _, a := range acks {
		if strings.HasPrefix(lower, a) {
			return true
		}
	}
	return false
}

func getMessages(conv map[string]interface{}) []map[string]interface{} {
	raw, ok := conv["messages"].([]interface{})
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	for _, m := range raw {
		if mm, ok := m.(map[string]interface{}); ok {
			out = append(out, mm)
		}
	}
	return out
}

func lastRealAssistantMessage(msgs []map[string]interface{}) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if isUserMsg(msgs[i]) {
			continue
		}
		content, _ := msgs[i]["content"].(string)
		if !isAckMessage(content) {
			return content
		}
	}
	return ""
}

func allRealAssistantMessages(msgs []map[string]interface{}) string {
	var parts []string
	for _, m := range msgs {
		if isUserMsg(m) {
			continue
		}
		content, _ := m["content"].(string)
		if !isAckMessage(content) {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func extractSlotMessage(msgs []map[string]interface{}) string {
	for _, m := range msgs {
		c, _ := m["content"].(string)
		lc := strings.ToLower(c)
		if strings.Contains(lc, "reply with the number") || strings.Contains(lc, "reply with a number") {
			return c
		}
		if strings.Contains(c, "1") && strings.Contains(c, "2") &&
			(strings.Contains(lc, "available") || strings.Contains(lc, "works best") || strings.Contains(lc, "times")) {
			return c
		}
	}
	return ""
}

func waitForSlotMessage(maxSecs int) (map[string]interface{}, string, error) {
	start := time.Now()
	for {
		conv, err := getConversation()
		if err == nil {
			msgs := getMessages(conv)
			if slot := extractSlotMessage(msgs); slot != "" {
				return conv, slot, nil
			}
		}
		if time.Since(start) > time.Duration(maxSecs)*time.Second {
			return nil, "", fmt.Errorf("timed out waiting for slot message after %ds", maxSecs)
		}
		time.Sleep(2 * time.Second)
	}
}

func checkNoDuplicateQuestions(msgs []map[string]interface{}) []string {
	type intentPattern struct {
		name     string
		keywords []string
	}
	intents := []intentPattern{
		{"ask_name", []string{"your name", "full name", "first and last", "may i have your"}},
		{"ask_patient_type", []string{"visited us before", "first time", "new or returning", "new or existing", "been here before"}},
		{"ask_schedule", []string{"days and times", "when works", "what time", "schedule preference", "days work best"}},
		{"ask_provider", []string{"preferred provider", "provider preference", "who would you like", "which provider"}},
		{"ask_email", []string{"email address", "email for", "your email"}},
	}

	detectIntent := func(content string) string {
		lower := strings.ToLower(content)
		for _, ip := range intents {
			for _, kw := range ip.keywords {
				if strings.Contains(lower, kw) {
					return ip.name
				}
			}
		}
		return ""
	}

	var violations []string
	lastAssistantIntent := ""
	lastAssistantContent := ""

	for _, m := range msgs {
		if isUserMsg(m) {
			lastAssistantIntent = ""
			lastAssistantContent = ""
			continue
		}
		content, _ := m["content"].(string)
		if isAckMessage(content) {
			continue
		}
		intent := detectIntent(content)
		if intent != "" && intent == lastAssistantIntent {
			violations = append(violations, fmt.Sprintf(
				"DUPLICATE %s: %q ... then again: %q",
				intent,
				truncate(lastAssistantContent, 60),
				truncate(content, 60),
			))
		}
		if intent != "" {
			lastAssistantIntent = intent
			lastAssistantContent = content
		}
	}
	return violations
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func containsAny(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func containsAll(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if !strings.Contains(lower, strings.ToLower(sub)) {
			return false
		}
	}
	return true
}

func generateJWT(secret string) string {
	header := base64url(map[string]string{"alg": "HS256", "typ": "JWT"})
	now := time.Now()
	payload := base64url(map[string]interface{}{
		"sub":  "admin",
		"role": "admin",
		"iat":  now.Unix(),
		"exp":  now.Add(12 * time.Hour).Unix(),
	})
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	sig := strings.TrimRight(base64.URLEncoding.EncodeToString(mac.Sum(nil)), "=")
	return unsigned + "." + sig
}

func base64url(v interface{}) string {
	b, _ := json.Marshal(v)
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func setup() error {
	return purge()
}

// ---------------------------------------------------------------------------
// Scenarios — Adela Medical Spa
// ---------------------------------------------------------------------------

// 1. Happy path - Tox booking
func scenarioHappyPathTox(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	msg := "Hi I'm interested in Botox. My name is Sarah Johnson. I'm a new patient. Any weekday works. No provider preference. Email is sarah@test.com"
	if err := sendSMS(msg); err != nil {
		t.fatalf("send SMS: %v", err)
		return
	}

	conv, err := waitForStatus("awaiting_time_selection", maxWaitSecs)
	if err != nil {
		t.fatalf("%v", err)
		return
	}

	msgs := getMessages(conv)
	slotsMsg := extractSlotMessage(msgs)
	if slotsMsg == "" {
		_, slotsMsg, err = waitForSlotMessage(20)
		if err != nil {
			t.fatalf("no slot message found")
			return
		}
	}

	allText := allRealAssistantMessages(msgs)

	// Should map "Botox" to $9 Tox Offer
	t.check("mentions tox or botox", containsAny(allText, "tox", "botox"))
	t.check("slots contain at least one day", func() bool {
		for _, day := range []string{"Mon ", "Tue ", "Wed ", "Thu ", "Fri "} {
			if strings.Contains(slotsMsg, day) {
				return true
			}
		}
		return false
	}())

	// Select slot
	if err := sendSMS("1"); err != nil {
		t.fatalf("send slot selection: %v", err)
		return
	}
	time.Sleep(12 * time.Second)

	conv2, err := getConversation()
	if err != nil {
		t.fatalf("get conversation: %v", err)
		return
	}
	msgs2 := getMessages(conv2)
	allText2 := allRealAssistantMessages(msgs2)

	t.check("deposit link present", containsAny(allText2, "/pay/"))
	t.check("deposit amount is $50", containsAny(allText2, "$50"))
}

// 2. Service alias mapping — botox, tox, dysport all map to $9 Tox Offer
func scenarioServiceAliasMapping(t *T) {
	aliases := []struct {
		input   string
		expects []string
	}{
		{"I want botox", []string{"tox", "botox", "$9"}},
		{"I'm interested in tox", []string{"tox", "$9"}},
		{"Do you do dysport?", []string{"tox", "dysport", "neurotoxin", "$9"}},
	}

	for _, a := range aliases {
		if err := setup(); err != nil {
			t.fatalf("purge: %v", err)
			return
		}
		if err := sendSMS(a.input); err != nil {
			t.fatalf("send: %v", err)
			return
		}
		msgs, err := waitForReply(1, 30)
		if err != nil {
			t.fatalf("no reply for %q: %v", a.input, err)
			return
		}
		resp := lastRealAssistantMessage(msgs)
		// AI recognized the service and moved to qualification (name, patient type, etc.)
		t.check(fmt.Sprintf("%q recognized as service", a.input),
			containsAny(resp, "name", "new patient", "visited", "first time", "been here", "help you"))
	}
}

// 3. Provider preference - Tox: should show only eligible providers for service 38140
func scenarioProviderPreferenceTox(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	// $9 Tox Offer (38140) has 4 providers
	msg := "I'm Emily Davis, new patient. I want the $9 tox offer. Weekday mornings work."
	if err := sendSMS(msg); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := allRealAssistantMessages(msgs)

	// Should ask about provider preference and list eligible providers
	// The 4 providers for 38140: Angela Solenthaler, Brady Steineck, Brandy Roberts, McKenna Zehnder
	eligibleProviders := []string{"Angela", "Brady", "Brandy", "McKenna"}
	ineligibleProviders := []string{"Tannah", "Amy", "Demetria", "Tiffany", "Paige", "Bob", "Carol"}

	t.check("asks for provider preference or email", containsAny(resp, "provider", "email", "preference"))

	// If provider names are mentioned, check eligibility
	if containsAny(resp, "Angela", "Brady", "Brandy", "McKenna") {
		for _, p := range eligibleProviders {
			t.warn(fmt.Sprintf("eligible provider %s mentioned", p), containsAny(resp, p))
		}
		for _, p := range ineligibleProviders {
			t.check(fmt.Sprintf("ineligible provider %s NOT mentioned", p), !containsAny(resp, p))
		}
	}
}

// 4. Filler ambiguity — "I want filler" should trigger clarification
func scenarioFillerAmbiguity(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("I want filler"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)

	// AI might map "filler" directly to Dermal Fillers/Restylane or ask for clarification
	// Either is acceptable — the main thing is it handles it gracefully
	t.check("handles filler request", containsAny(resp, "filler", "dermal", "lip", "name", "new patient", "help"))
	// Should NOT crash or give an error
	t.check("no error response", !containsAny(resp, "error", "something went wrong"))
}

// 5. Facial ambiguity — many facial types at Adela
func scenarioFacialAmbiguity(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("I want a facial"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)

	// Adela has: Acne Facial, Custom ZO Skincare Facial, Dermaplaning + Glow Facial,
	// Dermaplaning Facial with Red Light Therapy, Hydrating Facial with Dermaplaning, etc.
	// AI might ask to clarify or default to one — both OK
	t.check("handles facial request", containsAny(resp, "facial", "skincare", "dermaplaning", "acne", "name", "help"))
}

// 6. New patient flow — full qualification order
func scenarioNewPatientFlow(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	// Step 1: greeting
	if err := sendSMS("Hi, I'd like to schedule something"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp1 := lastRealAssistantMessage(msgs)
	t.check("asks for name or service", containsAny(resp1, "name", "service", "treatment", "interested", "help"))

	// Step 2: provide name
	if err := sendSMS("My name is Jennifer White"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err = waitForReply(2, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp2 := lastRealAssistantMessage(msgs)
	t.check("asks for service after name", containsAny(resp2, "treatment", "service", "interested", "help", "looking for", "what can"))

	// Step 3: provide service
	if err := sendSMS("Botox please"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err = waitForReply(3, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp3 := lastRealAssistantMessage(msgs)
	t.check("asks for patient type", containsAny(resp3, "new patient", "visited", "first time", "been here", "been before"))

	// Step 4: patient type
	if err := sendSMS("First time!"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err = waitForReply(4, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp4 := lastRealAssistantMessage(msgs)
	t.check("asks for schedule preference", containsAny(resp4, "day", "time", "schedule", "when", "availability", "work best", "prefer"))

	// Step 5: schedule
	if err := sendSMS("Monday or Wednesday afternoons"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err = waitForReply(5, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp5 := lastRealAssistantMessage(msgs)
	// Should ask provider or email
	t.check("asks for provider preference or email", containsAny(resp5, "provider", "email", "preference"))
}

// 7. Returning patient flow
func scenarioReturningPatient(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("Hi, I'm Kim Park. I've been here before and want more tox."); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	// Should recognize returning patient and skip "new or returning" question
	t.check("recognizes returning patient", !containsAny(resp, "are you a new"))
	t.check("does NOT re-ask for service", !containsAny(resp, "what treatment", "what service are you"))
	// Should proceed to schedule or provider
	t.check("proceeds to next qualification", containsAny(resp, "day", "time", "schedule", "provider", "email", "when", "preference"))
}

// 8. Medical liability deflection
func scenarioMedicalLiabilityDeflection(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("How much Botox do I need for my forehead?"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	t.check("defers dosage to provider", containsAny(resp, "provider", "consultation", "assess", "appointment", "varies", "during your"))
	t.check("does NOT give specific unit count", !regexp.MustCompile(`\b\d{2,3}\s*units?\b`).MatchString(resp))
}

// 9. Emergency escalation
func scenarioEmergencyEscalation(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("I'm having trouble breathing after my appointment today"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	t.check("directs to 911/ER", containsAny(resp, "911", "emergency", "ER", "emergency room", "immediate medical"))
	t.check("does NOT minimize", !containsAny(resp, "probably fine", "don't worry", "nothing to worry"))
}

// 10. After-hours behavior — should respond 24/7
func scenarioAfterHoursBehavior(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("Hi, are you open?"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply — should respond 24/7: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	// Should respond (AI always responds for Adela)
	t.check("responds to inquiry", len(resp) > 10)
	t.check("mentions services or scheduling", containsAny(resp, "service", "treatment", "schedule", "appointment", "help", "interested"))
}

// 11. Provider not found
func scenarioProviderNotFound(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("I want to see Dr. Smith for Botox. My name is Test User, new patient."); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	// Should handle gracefully — suggest available providers or proceed without
	t.check("handles unknown provider gracefully", !containsAny(resp, "error", "crash", "something went wrong"))
	t.check("suggests alternatives or proceeds", containsAny(resp, "provider", "available", "team", "schedule", "day", "time", "email", "preference"))
}

// 12. Multiple services
func scenarioMultipleServices(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	if err := sendSMS("I want botox and a facial"); err != nil {
		t.fatalf("send: %v", err)
		return
	}
	msgs, err := waitForReply(1, 30)
	if err != nil {
		t.fatalf("no reply: %v", err)
		return
	}
	resp := lastRealAssistantMessage(msgs)
	// Should handle multi-service — either book one at a time or ask to clarify
	t.check("acknowledges multiple services", containsAny(resp, "botox", "tox", "facial", "both", "one at a time", "name", "help"))
}

// 13. Schedule preference extraction
func scenarioScheduleExtraction(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	msg := "I'm Lisa Ray, new patient. I want the $9 tox offer. I'm free Monday and Wednesday after 3pm. No provider preference. lisa@test.com"
	if err := sendSMS(msg); err != nil {
		t.fatalf("send: %v", err)
		return
	}

	conv, err := waitForStatus("awaiting_time_selection", maxWaitSecs)
	if err != nil {
		t.fatalf("%v", err)
		return
	}

	msgs := getMessages(conv)
	slotsMsg := extractSlotMessage(msgs)
	if slotsMsg == "" {
		_, slotsMsg, err = waitForSlotMessage(20)
		if err != nil {
			t.fatalf("no slot message found")
			return
		}
	}

	// Should filter to Monday and Wednesday afternoon slots
	t.check("slots shown", len(slotsMsg) > 0)
	// Check that slots contain at least Mon or Wed
	t.warn("Monday or Wednesday slots", containsAny(slotsMsg, "Mon", "Wed"))
}

// 14. No duplicate questions
func scenarioNoDuplicateQuestions(t *T) {
	if err := setup(); err != nil {
		t.fatalf("purge: %v", err)
		return
	}

	// Multi-turn conversation that exercises all qualification steps
	steps := []string{
		"Hi there!",
		"My name is Alex Test",
		"I want botox",
		"I'm a new patient",
		"Any weekday morning works",
		"No preference on provider",
		"alex@test.com",
	}

	for i, step := range steps {
		if err := sendSMS(step); err != nil {
			t.fatalf("send step %d: %v", i+1, err)
			return
		}
		_, err := waitForReply(i+1, 30)
		if err != nil {
			t.fatalf("no reply at step %d: %v", i+1, err)
			return
		}
	}

	conv, err := getConversation()
	if err != nil {
		t.fatalf("get conversation: %v", err)
		return
	}
	msgs := getMessages(conv)
	dupes := checkNoDuplicateQuestions(msgs)
	if len(dupes) == 0 {
		t.check("no duplicate questions in full flow", true)
	} else {
		for _, d := range dupes {
			t.check(fmt.Sprintf("no duplicate questions: %s", d), false)
		}
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	apiBase = os.Getenv("API_BASE_URL")
	jwtSecret = os.Getenv("ADMIN_JWT_SECRET")
	if apiBase == "" || jwtSecret == "" {
		fmt.Fprintln(os.Stderr, "ERROR: API_BASE_URL and ADMIN_JWT_SECRET required")
		os.Exit(1)
	}
	jwt = generateJWT(jwtSecret)

	scenarios := []scenario{
		{"happy-path-tox", scenarioHappyPathTox},
		{"service-alias-mapping", scenarioServiceAliasMapping},
		{"provider-preference-tox", scenarioProviderPreferenceTox},
		{"filler-ambiguity", scenarioFillerAmbiguity},
		{"facial-ambiguity", scenarioFacialAmbiguity},
		{"new-patient-flow", scenarioNewPatientFlow},
		{"returning-patient", scenarioReturningPatient},
		{"medical-liability-deflection", scenarioMedicalLiabilityDeflection},
		{"emergency-escalation", scenarioEmergencyEscalation},
		{"after-hours-behavior", scenarioAfterHoursBehavior},
		{"provider-not-found", scenarioProviderNotFound},
		{"multiple-services", scenarioMultipleServices},
		{"schedule-extraction", scenarioScheduleExtraction},
		{"no-duplicate-questions", scenarioNoDuplicateQuestions},
	}

	filter := ""
	if len(os.Args) > 1 {
		filter = os.Args[1]
	}

	totalPassed := 0
	totalFailed := 0
	scenarioResults := make([]string, 0)

	for _, s := range scenarios {
		if filter != "" && s.Name != filter {
			continue
		}

		fmt.Printf("\n========================================\n")
		fmt.Printf("SCENARIO: %s\n", s.Name)
		fmt.Printf("========================================\n")

		t := &T{name: s.Name}
		s.Fn(t)

		// Universal duplicate question check
		conv, convErr := getConversation()
		if convErr == nil {
			msgs := getMessages(conv)
			dupes := checkNoDuplicateQuestions(msgs)
			if len(dupes) == 0 {
				t.check("no duplicate questions", true)
			} else {
				for _, d := range dupes {
					t.check(fmt.Sprintf("no duplicate questions: %s", d), false)
				}
			}
		}

		totalPassed += t.passed
		totalFailed += t.failed

		status := "✅"
		if t.failed > 0 {
			status = "❌"
		}
		scenarioResults = append(scenarioResults, fmt.Sprintf("  %s %s (%d passed, %d failed)", status, s.Name, t.passed, t.failed))
	}

	fmt.Printf("\n========================================\n")
	fmt.Println("SUMMARY")
	fmt.Printf("========================================\n")
	for _, r := range scenarioResults {
		fmt.Println(r)
	}
	fmt.Printf("\nTotal: %d passed, %d failed\n", totalPassed, totalFailed)

	if totalFailed > 0 {
		fmt.Println("\n❌ SOME TESTS FAILED")
		os.Exit(1)
	}
	fmt.Println("\n✅ ALL TESTS PASSED")
}
