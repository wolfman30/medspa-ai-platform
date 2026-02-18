// Package main implements a config-driven service test generator.
//
// It pulls a clinic's config from the admin API and auto-generates E2E tests
// for every service: alias resolution, provider count, availability, and
// optionally a full booking flow.
//
// Usage:
//
//	go run scripts/e2e/service_test.go --org=<orgID> [--tier=1|2|3] [--api=URL] [--secret=SECRET]
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Config types
// ---------------------------------------------------------------------------

type ClinicConfig struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`

	MoxieConfig    *MoxieConfig      `json:"moxie_config"`
	ServiceAliases map[string]string `json:"service_aliases"`
}

type MoxieConfig struct {
	MedSpaID             string            `json:"medspa_id"`
	MedSpaSlug           string            `json:"medspa_slug"`
	ServiceMenuItems     map[string]string `json:"service_menu_items"`
	ServiceProviderCount map[string]int    `json:"service_provider_count"`
	ProviderNames        map[string]string `json:"provider_names"`
}

// ---------------------------------------------------------------------------
// Test result types
// ---------------------------------------------------------------------------

type ServiceResult struct {
	Name           string
	MoxieID        string
	AliasPass      *bool // nil = skipped
	AliasDetail    string
	ProviderPass   *bool
	ProviderDetail string
	AvailPass      *bool
	AvailDetail    string
	FlowPass       *bool
	FlowDetail     string
}

// ---------------------------------------------------------------------------
// Globals
// ---------------------------------------------------------------------------

var (
	flagOrg    string
	flagTier   int
	flagAPI    string
	flagSecret string
	jwt        string
)

func init() {
	flag.StringVar(&flagOrg, "org", "", "Organization ID (required)")
	flag.IntVar(&flagTier, "tier", 1, "Test tier: 1=alias+provider, 2=+availability, 3=+full flow")
	flag.StringVar(&flagAPI, "api", "https://api-dev.aiwolfsolutions.com", "API base URL")
	flag.StringVar(&flagSecret, "secret", "", "Admin JWT secret (or ADMIN_JWT_SECRET env)")
}

// ---------------------------------------------------------------------------
// JWT
// ---------------------------------------------------------------------------

func generateJWT() string {
	header := base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now().Unix()
	payload := base64url([]byte(fmt.Sprintf(`{"sub":"admin","iat":%d,"exp":%d}`, now, now+3600)))
	msg := header + "." + payload
	mac := hmac.New(sha256.New, []byte(flagSecret))
	mac.Write([]byte(msg))
	sig := base64url(mac.Sum(nil))
	return msg + "." + sig
}

func base64url(data []byte) string {
	s := base64.RawURLEncoding.EncodeToString(data)
	return s
}

var clinicPhone = "+14407448197"

func sendSMS(from, to, text string) error {
	ts := time.Now().UnixNano()
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"id":         fmt.Sprintf("svc-test-%d", ts),
			"event_type": "message.received",
			"payload": map[string]interface{}{
				"id":        fmt.Sprintf("msg-svc-%d", ts),
				"from":      map[string]string{"phone_number": from},
				"to":        []map[string]string{{"phone_number": to}},
				"text":      text,
				"direction": "inbound",
				"type":      "SMS",
			},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(flagAPI+"/webhooks/telnyx/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getConversation(orgID, phone string) (map[string]interface{}, error) {
	digits := strings.TrimPrefix(phone, "+")
	convID := fmt.Sprintf("sms:%s:%s", orgID, digits)
	u := fmt.Sprintf("%s/portal/orgs/%s/conversations/%s", flagAPI, orgID, url.PathEscape(convID))
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func getMessages(conv map[string]interface{}) []map[string]interface{} {
	msgs, ok := conv["messages"].([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]interface{}
	for _, m := range msgs {
		if mm, ok := m.(map[string]interface{}); ok {
			result = append(result, mm)
		}
	}
	return result
}

func waitForReply(orgID, phone string, minMsgs int, timeoutSec int) ([]map[string]interface{}, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		conv, err := getConversation(orgID, phone)
		if err == nil {
			msgs := getMessages(conv)
			if len(msgs) >= minMsgs {
				return msgs, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	conv, _ := getConversation(orgID, phone)
	return getMessages(conv), fmt.Errorf("timeout waiting for %d messages", minMsgs)
}

// ---------------------------------------------------------------------------
// API helpers
// ---------------------------------------------------------------------------

func fetchClinicConfig(orgID string) (*ClinicConfig, error) {
	u := fmt.Sprintf("%s/admin/clinics/%s/config", flagAPI, orgID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("config returned %d: %s", resp.StatusCode, string(body))
	}
	var cfg ClinicConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func testPhoneForIndex(idx int) string {
	return fmt.Sprintf("+1555000%04d", idx)
}

func purgePhone(orgID, phone string) error {
	u := fmt.Sprintf("%s/admin/clinics/%s/phones/%s", flagAPI, orgID, url.PathEscape(phone))
	req, _ := http.NewRequest("DELETE", u, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func isUserMsg(m map[string]interface{}) bool {
	role, _ := m["role"].(string)
	return role == "user"
}

func isAckMessage(content string) bool {
	acks := []string{
		"got it", "give me a moment", "let me check", "checking", "one moment",
		"hold on", "looking into", "let me look",
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	for _, a := range acks {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return len(lower) < 50 && (strings.HasSuffix(lower, "...") || strings.HasSuffix(lower, "!"))
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

func containsAny(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Test: Alias Resolution (Tier 1)
// ---------------------------------------------------------------------------

func testAlias(cfg *ClinicConfig, serviceName string) (bool, string) {
	if cfg.ServiceAliases == nil {
		return false, "no service_aliases in config"
	}
	// Check if any alias maps to this service (case-insensitive)
	var found []string
	for alias, target := range cfg.ServiceAliases {
		if strings.EqualFold(target, serviceName) {
			found = append(found, alias)
		}
	}
	if len(found) == 0 {
		return false, "no aliases map to this service"
	}
	return true, fmt.Sprintf("%d aliases: %s", len(found), strings.Join(found, ", "))
}

// ---------------------------------------------------------------------------
// Test: Provider Count (Tier 1)
// ---------------------------------------------------------------------------

func testProviderCount(cfg *ClinicConfig, serviceName, moxieID string) (bool, string) {
	if cfg.MoxieConfig == nil || cfg.MoxieConfig.ServiceProviderCount == nil {
		return false, "no service_provider_count in config"
	}
	count, exists := cfg.MoxieConfig.ServiceProviderCount[moxieID]
	if !exists {
		return false, fmt.Sprintf("moxie ID %s not in service_provider_count", moxieID)
	}
	return true, fmt.Sprintf("%d provider(s)", count)
}

// ---------------------------------------------------------------------------
// Test: Availability (Tier 2) — via webhook simulation
// ---------------------------------------------------------------------------

func testAvailability(cfg *ClinicConfig, serviceName string, idx int) (bool, string) {
	phone := testPhoneForIndex(idx)
	_ = purgePhone(flagOrg, phone)
	defer purgePhone(flagOrg, phone)

	// Send a message that provides everything needed to trigger availability
	msg := fmt.Sprintf("I want %s. I'm Test Avail %d. I'm a new patient. Anytime works. No provider preference. testavail%d@test.com", serviceName, idx, idx)
	if err := sendSMS(phone, clinicPhone, msg); err != nil {
		return false, fmt.Sprintf("send error: %v", err)
	}

	// Wait up to 30s for a response with time slots or a "no availability" message
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		conv, err := getConversation(flagOrg, phone)
		if err != nil {
			continue
		}
		msgs := getMessages(conv)
		allText := allRealAssistantMessages(msgs)
		lower := strings.ToLower(allText)

		// Check for time slots
		if strings.Contains(lower, "reply with the number") || strings.Contains(lower, "available time") {
			return true, "time slots returned"
		}
		// Check for explicit no-availability
		if containsAny(lower, "couldn't find", "no available", "no times", "unfortunately") {
			return false, "no time slots available"
		}
		// Check conversation status
		status, _ := conv["status"].(string)
		if status == "awaiting_time_selection" {
			return true, "awaiting_time_selection status"
		}
	}
	return false, "timed out waiting for availability"
}

// ---------------------------------------------------------------------------
// Test: Full Flow (Tier 3)
// ---------------------------------------------------------------------------

// Categories for tier 3 — pick one service per category
var tier3Categories = map[string][]string{
	"injectable":     {"tox (wrinkle relaxer)"},
	"filler":         {"lip filler", "dermal filler", "mini lip filler"},
	"skin_treatment": {"microneedling", "chemical peel", "salmon dna facial"},
	"laser":          {"ipl - full face", "laser hair removal - small area", "tixel - full face"},
	"weight_loss":    {"weight loss consultation - in person", "weight loss consultation - virtual"},
}

func pickTier3Services(menuItems map[string]string) map[string]string {
	picked := make(map[string]string) // category -> service name
	for cat, candidates := range tier3Categories {
		for _, c := range candidates {
			if _, ok := menuItems[c]; ok {
				picked[cat] = c
				break
			}
		}
	}
	return picked
}

func testFullFlow(cfg *ClinicConfig, serviceName string, idx int) (bool, string) {
	phone := testPhoneForIndex(idx + 1000) // offset to avoid collision with availability tests
	_ = purgePhone(flagOrg, phone)
	defer purgePhone(flagOrg, phone)

	providerCount := 0
	if cfg.MoxieConfig != nil {
		if moxieID, ok := cfg.MoxieConfig.ServiceMenuItems[strings.ToLower(serviceName)]; ok {
			providerCount = cfg.MoxieConfig.ServiceProviderCount[moxieID]
		}
	}

	steps := []struct {
		send    string
		waitFor []string
		desc    string
	}{
		{
			send:    fmt.Sprintf("Hi I'm interested in %s", serviceName),
			waitFor: []string{"name", "who", "call you", "your name"},
			desc:    "name question",
		},
		{
			send:    fmt.Sprintf("Test Patient %d", idx),
			waitFor: []string{"new patient", "visited", "been before", "first time", "been here"},
			desc:    "patient type question",
		},
		{
			send:    "New patient",
			waitFor: []string{"day", "time", "schedule", "prefer", "when", "work best", "availability", "morning", "afternoon"},
			desc:    "schedule question",
		},
		{
			send:    "Anytime works",
			waitFor: []string{"provider", "preference", "reply with the number", "available time", "couldn't find", "no available"},
			desc:    "provider question or availability",
		},
	}

	for i, step := range steps {
		if err := sendSMS(phone, clinicPhone, step.send); err != nil {
			return false, fmt.Sprintf("step %d send error: %v", i+1, err)
		}
		msgs, err := waitForReply(flagOrg, phone, i+1, 15)
		if err != nil {
			return false, fmt.Sprintf("step %d (%s): %v", i+1, step.desc, err)
		}
		resp := lastRealAssistantMessage(msgs)
		if !containsAny(resp, step.waitFor...) {
			// On step 4, if providerCount <= 1, we might go straight to slots
			if i == 3 && providerCount <= 1 {
				allText := allRealAssistantMessages(msgs)
				if containsAny(allText, "reply with the number", "available time", "awaiting") {
					return true, "flow complete (no provider question, single provider)"
				}
			}
			return false, fmt.Sprintf("step %d (%s): expected %v, got: %.100s", i+1, step.desc, step.waitFor, resp)
		}
	}

	// If we got provider question, answer it
	msgs, _ := waitForReply(flagOrg, phone, 4, 5)
	resp := lastRealAssistantMessage(msgs)
	if containsAny(resp, "provider", "preference") && providerCount > 1 {
		if err := sendSMS(phone, clinicPhone, "Whoever is available first"); err != nil {
			return false, "send provider pref error"
		}
		msgs, err := waitForReply(flagOrg, phone, 5, 15)
		if err != nil {
			return false, fmt.Sprintf("provider pref response: %v", err)
		}
		allText := allRealAssistantMessages(msgs)
		if containsAny(allText, "reply with the number", "available time") {
			return true, "flow complete (with provider selection)"
		}
		return false, "no time slots after provider selection"
	}

	// Check if we already have slots
	allText := allRealAssistantMessages(msgs)
	if containsAny(allText, "reply with the number", "available time") {
		// Check for duplicate questions
		if dupes := checkNoDuplicateQuestions(msgs); len(dupes) > 0 {
			return false, fmt.Sprintf("flow complete but DUPLICATE QUESTIONS: %s", dupes[0])
		}
		return true, "flow complete"
	}

	return false, "no time slots in final response"
}

// checkNoDuplicateQuestions scans for consecutive assistant messages asking the same question.
func checkNoDuplicateQuestions(msgs []map[string]interface{}) []string {
	type intentPattern struct {
		name     string
		keywords []string
	}
	intents := []intentPattern{
		{"ask_name", []string{"your name", "full name", "first and last", "may i have your"}},
		{"ask_patient_type", []string{"visited us before", "first time", "new or returning", "new or existing"}},
		{"ask_schedule", []string{"days and times", "when works", "what time", "days work best"}},
		{"ask_provider", []string{"preferred provider", "provider preference", "which provider"}},
		{"ask_email", []string{"email address", "your email"}},
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
	lastIntent := ""
	for _, m := range msgs {
		if isUserMsg(m) {
			lastIntent = ""
			continue
		}
		content, _ := m["content"].(string)
		if isAckMessage(content) {
			continue
		}
		intent := detectIntent(content)
		if intent != "" && intent == lastIntent {
			violations = append(violations, fmt.Sprintf("DUPLICATE %s", intent))
		}
		if intent != "" {
			lastIntent = intent
		}
	}
	return violations
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

func boolIcon(b *bool) string {
	if b == nil {
		return " — "
	}
	if *b {
		return " ✅ "
	}
	return " ❌ "
}

func printReport(clinicName string, results []ServiceResult) {
	fmt.Printf("\nSERVICE TEST REPORT — %s\n", clinicName)
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("%-42s| Alias | Providers | Availability | Flow\n", "Service")
	fmt.Println(strings.Repeat("-", 90))

	passing := 0
	total := 0
	for _, r := range results {
		fmt.Printf("%-42s|%s|   %s   |    %s     |%s\n",
			truncate(r.Name, 42),
			boolIcon(r.AliasPass),
			boolIcon(r.ProviderPass),
			boolIcon(r.AvailPass),
			boolIcon(r.FlowPass),
		)
		if r.AliasPass != nil {
			total++
			if *r.AliasPass {
				passing++
			}
		}
		if r.ProviderPass != nil {
			total++
			if *r.ProviderPass {
				passing++
			}
		}
		if r.AvailPass != nil {
			total++
			if *r.AvailPass {
				passing++
			}
		}
		if r.FlowPass != nil {
			total++
			if *r.FlowPass {
				passing++
			}
		}
	}
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("TOTAL: %d/%d passing\n\n", passing, total)

	// Print details for failures
	hasFailures := false
	for _, r := range results {
		details := []struct {
			name   string
			pass   *bool
			detail string
		}{
			{"Alias", r.AliasPass, r.AliasDetail},
			{"Provider", r.ProviderPass, r.ProviderDetail},
			{"Availability", r.AvailPass, r.AvailDetail},
			{"Flow", r.FlowPass, r.FlowDetail},
		}
		for _, d := range details {
			if d.pass != nil && !*d.pass {
				if !hasFailures {
					fmt.Println("FAILURE DETAILS:")
					fmt.Println(strings.Repeat("-", 60))
					hasFailures = true
				}
				fmt.Printf("  ❌ %s / %s: %s\n", r.Name, d.name, d.detail)
			}
		}
	}

	if passing == total {
		fmt.Println("✅ ALL TESTS PASSED")
	} else {
		fmt.Printf("\n❌ %d TESTS FAILED\n", total-passing)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()

	if flagOrg == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --org is required")
		os.Exit(1)
	}

	secret := flagSecret
	if secret == "" {
		secret = os.Getenv("ADMIN_JWT_SECRET")
	}
	if secret == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --secret or ADMIN_JWT_SECRET required")
		os.Exit(1)
	}

	flagSecret = secret
	jwt = generateJWT()

	fmt.Printf("Fetching clinic config for org %s...\n", flagOrg)
	cfg, err := fetchClinicConfig(flagOrg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR fetching config: %v\n", err)
		os.Exit(1)
	}

	if cfg.MoxieConfig == nil || len(cfg.MoxieConfig.ServiceMenuItems) == 0 {
		fmt.Fprintln(os.Stderr, "ERROR: no moxie_config.service_menu_items in config")
		os.Exit(1)
	}

	// Get clinic phone from config if available
	// (clinicPhone is set as default above)

	fmt.Printf("Clinic: %s\n", cfg.Name)
	fmt.Printf("Services: %d\n", len(cfg.MoxieConfig.ServiceMenuItems))
	fmt.Printf("Tier: %d\n", flagTier)

	// Sort services for deterministic output
	var serviceNames []string
	for name := range cfg.MoxieConfig.ServiceMenuItems {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	// Determine tier 3 services
	tier3Set := make(map[string]bool)
	if flagTier >= 3 {
		picked := pickTier3Services(cfg.MoxieConfig.ServiceMenuItems)
		for _, svc := range picked {
			tier3Set[svc] = true
		}
		fmt.Printf("Tier 3 services: %v\n", picked)
	}

	results := make([]ServiceResult, 0, len(serviceNames))

	for idx, svcName := range serviceNames {
		moxieID := cfg.MoxieConfig.ServiceMenuItems[svcName]
		// Use title case for display
		displayName := strings.Title(svcName)
		fmt.Printf("[%d/%d] Testing: %s (Moxie ID: %s)\n", idx+1, len(serviceNames), displayName, moxieID)

		r := ServiceResult{
			Name:    displayName,
			MoxieID: moxieID,
		}

		// Tier 1: Alias
		pass, detail := testAlias(cfg, svcName)
		r.AliasPass = &pass
		r.AliasDetail = detail
		if pass {
			fmt.Printf("  ✅ Alias: %s\n", detail)
		} else {
			fmt.Printf("  ❌ Alias: %s\n", detail)
		}

		// Tier 1: Provider count
		pass2, detail2 := testProviderCount(cfg, svcName, moxieID)
		r.ProviderPass = &pass2
		r.ProviderDetail = detail2
		if pass2 {
			fmt.Printf("  ✅ Provider: %s\n", detail2)
		} else {
			fmt.Printf("  ❌ Provider: %s\n", detail2)
		}

		// Tier 2: Availability
		if flagTier >= 2 {
			fmt.Printf("  ⏳ Checking availability...\n")
			pass3, detail3 := testAvailability(cfg, svcName, idx)
			r.AvailPass = &pass3
			r.AvailDetail = detail3
			if pass3 {
				fmt.Printf("  ✅ Availability: %s\n", detail3)
			} else {
				fmt.Printf("  ❌ Availability: %s\n", detail3)
			}
		}

		// Tier 3: Full flow (only for selected services)
		if flagTier >= 3 && tier3Set[svcName] {
			fmt.Printf("  ⏳ Running full flow...\n")
			pass4, detail4 := testFullFlow(cfg, svcName, idx)
			r.FlowPass = &pass4
			r.FlowDetail = detail4
			if pass4 {
				fmt.Printf("  ✅ Flow: %s\n", detail4)
			} else {
				fmt.Printf("  ❌ Flow: %s\n", detail4)
			}
		}

		results = append(results, r)
	}

	printReport(cfg.Name, results)

	// Exit with failure code if any tests failed
	for _, r := range results {
		for _, p := range []*bool{r.AliasPass, r.ProviderPass, r.AvailPass, r.FlowPass} {
			if p != nil && !*p {
				os.Exit(1)
			}
		}
	}
}
