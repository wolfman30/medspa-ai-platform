// Package main runs an automated E2E test of the full SMS booking flow.
// Usage: ADMIN_JWT_SECRET=... API_BASE_URL=... go run scripts/e2e/run_e2e.go
//
// This script:
// 1. Purges test phone data
// 2. Sends an inbound SMS with all qualifications
// 3. Waits for availability slots (Mon/Wed after 3pm only)
// 4. Selects a slot
// 5. Verifies booking policies + Stripe deposit link
// 6. Reports pass/fail with details
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

const (
	testPhone    = "+15005550002"
	clinicPhone  = "+14407448197"
	orgID        = "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599"
	convID       = "sms:d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599:5005550002"
	maxWaitSecs  = 30
	pollInterval = 2 * time.Second
)

var (
	apiBase   string
	jwtSecret string
)

func main() {
	apiBase = os.Getenv("API_BASE_URL")
	jwtSecret = os.Getenv("ADMIN_JWT_SECRET")
	if apiBase == "" || jwtSecret == "" {
		fmt.Fprintln(os.Stderr, "ERROR: API_BASE_URL and ADMIN_JWT_SECRET required")
		os.Exit(1)
	}

	jwt := generateJWT(jwtSecret)
	passed := 0
	failed := 0

	// Step 1: Purge
	fmt.Println("=== Step 1: Purge test data ===")
	if err := purge(jwt); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: purge: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  PASS: purged")

	// Step 2: Send all-in-one qualification message
	fmt.Println("\n=== Step 2: Send qualification message ===")
	msg := "I want Botox. I'm Andy Wolf. I'm new. I prefer Mondays and Wednesdays after 3p. No provider preference. Email is andywolf@test.com"
	if err := sendSMS(msg); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: send SMS: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  SENT:", msg)

	// Step 3: Wait for availability slots
	fmt.Println("\n=== Step 3: Wait for availability slots ===")
	conv, err := waitForStatus(jwt, "awaiting_time_selection", maxWaitSecs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}

	messages := conv["messages"].([]interface{})
	slotsMsg := ""
	for _, m := range messages {
		mm := m.(map[string]interface{})
		content := mm["content"].(string)
		if strings.Contains(content, "Reply with the number") {
			slotsMsg = content
		}
	}

	if slotsMsg == "" {
		fmt.Fprintln(os.Stderr, "FAIL: no slot message found")
		failed++
	} else {
		fmt.Println("  PASS: slots presented")

		// Check: only Mon/Wed
		check("slots contain only Mon/Wed", func() bool {
			for _, day := range []string{"Thu ", "Tue ", "Fri ", "Sat ", "Sun "} {
				if strings.Contains(slotsMsg, day) {
					fmt.Printf("    Found unexpected day: %s\n", day)
					return false
				}
			}
			return true
		}, &passed, &failed)

		// Check: no 3:00 PM (strictly after)
		check("no 3:00 PM slots (strictly after 3pm)", func() bool {
			return !strings.Contains(slotsMsg, "3:00 PM")
		}, &passed, &failed)

		// Check: says "Botox" not "Tox"
		check("says 'Botox' not 'Tox'", func() bool {
			return strings.Contains(slotsMsg, "Botox") && !strings.Contains(slotsMsg, "for Tox")
		}, &passed, &failed)
	}

	// Step 4: Select slot 1
	fmt.Println("\n=== Step 4: Select slot ===")
	if err := sendSMS("1"); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: send slot selection: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  SENT: 1")

	// Step 5: Wait for deposit link
	fmt.Println("\n=== Step 5: Wait for deposit link ===")
	time.Sleep(10 * time.Second)
	conv2, err := getConversation(jwt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: get conversation: %v\n", err)
		os.Exit(1)
	}

	messages2 := conv2["messages"].([]interface{})
	lastMsg := ""
	for _, m := range messages2 {
		mm := m.(map[string]interface{})
		lastMsg = mm["content"].(string)
	}

	// Check: booking policies before payment
	check("booking policies shown before payment link", func() bool {
		return strings.Contains(lastMsg, "Before you pay") || strings.Contains(lastMsg, "please note")
	}, &passed, &failed)

	// Check: cancellation policy
	check("cancellation policy present", func() bool {
		return strings.Contains(lastMsg, "24 hours") && strings.Contains(lastMsg, "cancellation")
	}, &passed, &failed)

	// Check: SMS consent
	check("SMS consent present", func() bool {
		return strings.Contains(lastMsg, "automated text messages")
	}, &passed, &failed)

	// Check: age confirmation
	check("age confirmation present", func() bool {
		return strings.Contains(lastMsg, "18")
	}, &passed, &failed)

	// Check: deposit link
	check("Stripe deposit link present", func() bool {
		return strings.Contains(lastMsg, "/pay/")
	}, &passed, &failed)

	// Check: $50 amount
	check("deposit amount is $50", func() bool {
		return strings.Contains(lastMsg, "$50.00")
	}, &passed, &failed)

	// Summary
	fmt.Printf("\n=== Results: %d passed, %d failed ===\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
	fmt.Println("ALL TESTS PASSED ✅")
}

func check(name string, fn func() bool, passed, failed *int) {
	if fn() {
		fmt.Printf("  PASS: %s\n", name)
		*passed++
	} else {
		fmt.Printf("  FAIL: %s\n", name)
		*failed++
	}
}

func purge(jwt string) error {
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
			"id":         fmt.Sprintf("e2e-%d", ts),
			"event_type": "message.received",
			"payload": map[string]interface{}{
				"id":        fmt.Sprintf("msg-%d", ts),
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

func waitForStatus(jwt, targetStatus string, maxSecs int) (map[string]interface{}, error) {
	deadline := time.Now().Add(time.Duration(maxSecs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		conv, err := getConversation(jwt)
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

func getConversation(jwt string) (map[string]interface{}, error) {
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
