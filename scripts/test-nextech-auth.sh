#!/bin/bash
set -e

echo "🔐 Testing Nextech OAuth Authentication"
echo "========================================"
echo ""

# Check if .env exists
if [ ! -f .env ]; then
    echo "❌ Error: .env file not found"
    echo "Run: cp .env.bootstrap.example .env"
    exit 1
fi

# Load environment variables
source .env

# Check required variables
if [ -z "$NEXTECH_BASE_URL" ] || [ "$NEXTECH_BASE_URL" = "your-nextech-base-url" ]; then
    echo "❌ Error: NEXTECH_BASE_URL not set in .env"
    exit 1
fi

if [ -z "$NEXTECH_CLIENT_ID" ] || [ "$NEXTECH_CLIENT_ID" = "your-nextech-client-id" ]; then
    echo "❌ Error: NEXTECH_CLIENT_ID not set in .env"
    echo "Get credentials from: https://www.nextech.com/developers-portal"
    exit 1
fi

if [ -z "$NEXTECH_CLIENT_SECRET" ] || [ "$NEXTECH_CLIENT_SECRET" = "your-nextech-client-secret" ]; then
    echo "❌ Error: NEXTECH_CLIENT_SECRET not set in .env"
    echo "Get credentials from: https://www.nextech.com/developers-portal"
    exit 1
fi

echo "Configuration:"
echo "  Base URL: $NEXTECH_BASE_URL"
echo "  Client ID: ${NEXTECH_CLIENT_ID:0:8}***"
echo ""

# Request OAuth token
echo "Requesting OAuth token..."
TOKEN_RESPONSE=$(curl -s -X POST "$NEXTECH_BASE_URL/connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=$NEXTECH_CLIENT_ID" \
  -d "client_secret=$NEXTECH_CLIENT_SECRET" \
  -d "scope=patient/*.read patient/*.write appointment/*.read appointment/*.write slot/*.read")

# Check if jq is available
if command -v jq &> /dev/null; then
    echo ""
    echo "Response:"
    echo "$TOKEN_RESPONSE" | jq .
    echo ""

    # Check if token was received
    if echo "$TOKEN_RESPONSE" | jq -e '.access_token' > /dev/null; then
        echo "✅ SUCCESS! OAuth authentication working."
        echo ""
        echo "Access token received:"
        echo "  Type: $(echo "$TOKEN_RESPONSE" | jq -r '.token_type')"
        echo "  Expires in: $(echo "$TOKEN_RESPONSE" | jq -r '.expires_in') seconds"
        echo "  Token (first 20 chars): $(echo "$TOKEN_RESPONSE" | jq -r '.access_token' | cut -c1-20)..."
        echo ""
        echo "Next steps:"
        echo "  1. Run: go test ./internal/emr/nextech -v"
        echo "  2. Try: scripts/test-nextech-patient.sh"
        exit 0
    else
        echo "❌ FAILED! Authentication error."
        echo ""
        echo "Common issues:"
        echo "  - Check Client ID and Client Secret are correct"
        echo "  - Verify you're using sandbox vs production URL"
        echo "  - Ensure your application is approved by Nextech"
        exit 1
    fi
else
    # jq not available, do basic check
    echo ""
    echo "Response (raw):"
    echo "$TOKEN_RESPONSE"
    echo ""

    if echo "$TOKEN_RESPONSE" | grep -q "access_token"; then
        echo "✅ SUCCESS! OAuth authentication working."
        echo ""
        echo "Install 'jq' for better response formatting:"
        echo "  Ubuntu/Debian: sudo apt install jq"
        echo "  macOS: brew install jq"
        echo "  Windows: choco install jq"
        exit 0
    else
        echo "❌ FAILED! Authentication error."
        echo "Check your credentials in .env"
        exit 1
    fi
fi
