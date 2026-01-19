package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run main.go <org_id> <phone>")
		fmt.Println("Example: go run main.go bb507f20-7fcc-4941-9eac-9ed93b7834ed 5005550001")
		os.Exit(1)
	}

	orgID := os.Args[1]
	phone := os.Args[2]

	secret := os.Getenv("ADMIN_JWT_SECRET")
	if secret == "" {
		fmt.Println("Error: ADMIN_JWT_SECRET environment variable not set")
		os.Exit(1)
	}

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "https://api-dev.aiwolfsolutions.com"
	}

	// Generate JWT token
	claims := jwt.RegisteredClaims{
		Subject:   "admin",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		fmt.Printf("Error signing token: %v\n", err)
		os.Exit(1)
	}

	// Call the purge endpoint
	url := fmt.Sprintf("%s/admin/clinics/%s/phones/%s", apiURL, orgID, phone)
	fmt.Printf("Purging data for phone %s in org %s...\n", phone, orgID)
	fmt.Printf("URL: %s\n", url)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: HTTP %d\n", resp.StatusCode)
		fmt.Printf("Response: %s\n", string(body))
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("Response: %s\n", string(body))
	} else {
		prettyJSON, _ := json.MarshalIndent(result, "", "  ")
		fmt.Printf("Success!\n%s\n", string(prettyJSON))
	}
}
