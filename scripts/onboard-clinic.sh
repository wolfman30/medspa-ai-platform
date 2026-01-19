#!/usr/bin/env bash
#
# onboard-clinic.sh - Orchestrates the complete clinic onboarding process
#
# Usage:
#   ./scripts/onboard-clinic.sh --name "Clinic Name" [options]
#
# Options:
#   --name        Clinic name (required)
#   --timezone    Timezone (default: America/New_York)
#   --api-url     API base URL (default: http://localhost:8080)
#   --token       Admin JWT token (or set ADMIN_JWT_TOKEN env var)
#   --skip-square Skip Square OAuth step (manual later)
#   --help        Show this help message
#
# Environment Variables:
#   ADMIN_JWT_TOKEN - Admin JWT for authentication
#   API_URL         - API base URL (overridden by --api-url)
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
API_URL="${API_URL:-http://localhost:8080}"
TIMEZONE="America/New_York"
SKIP_SQUARE=false
CLINIC_NAME=""
ADMIN_TOKEN="${ADMIN_JWT_TOKEN:-}"
ONBOARDING_TOKEN="${ONBOARDING_TOKEN:-}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --name)
            CLINIC_NAME="$2"
            shift 2
            ;;
        --timezone)
            TIMEZONE="$2"
            shift 2
            ;;
        --api-url)
            API_URL="$2"
            shift 2
            ;;
        --token)
            ADMIN_TOKEN="$2"
            shift 2
            ;;
        --skip-square)
            SKIP_SQUARE=true
            shift
            ;;
        --help|-h)
            head -30 "$0" | tail -25
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Validate required arguments
if [[ -z "$CLINIC_NAME" ]]; then
    echo -e "${RED}Error: --name is required${NC}"
    echo "Usage: $0 --name \"Clinic Name\" [options]"
    exit 1
fi

if [[ -z "$ADMIN_TOKEN" ]]; then
    echo -e "${RED}Error: Admin token required. Set ADMIN_JWT_TOKEN or use --token${NC}"
    exit 1
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  MedSpa AI Platform - Clinic Onboarding${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Clinic Name: ${GREEN}$CLINIC_NAME${NC}"
echo -e "Timezone:    ${GREEN}$TIMEZONE${NC}"
echo -e "API URL:     ${GREEN}$API_URL${NC}"
echo ""

# Step 1: Create the clinic
echo -e "${YELLOW}Step 1: Creating clinic...${NC}"
CREATE_RESPONSE=$(curl -s -X POST "${API_URL}/admin/clinics" \
    -H "Authorization: Bearer ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"${CLINIC_NAME}\", \"timezone\": \"${TIMEZONE}\"}")

ORG_ID=$(echo "$CREATE_RESPONSE" | jq -r '.org_id // empty')
if [[ -z "$ORG_ID" ]]; then
    echo -e "${RED}Failed to create clinic:${NC}"
    echo "$CREATE_RESPONSE" | jq .
    exit 1
fi

echo -e "${GREEN}Created clinic with org_id: ${ORG_ID}${NC}"
echo ""

# Step 2: Check onboarding status
echo -e "${YELLOW}Step 2: Checking onboarding status...${NC}"
STATUS_RESPONSE=$(curl -s -X GET "${API_URL}/admin/clinics/${ORG_ID}/onboarding-status" \
    -H "Authorization: Bearer ${ADMIN_TOKEN}")

echo "$STATUS_RESPONSE" | jq '.steps[] | {name: .name, completed: .completed}'
echo ""

# Step 3: Square OAuth
if [[ "$SKIP_SQUARE" == "false" ]]; then
    echo -e "${YELLOW}Step 3: Square OAuth${NC}"
    echo -e "Open the following URL in your browser to connect Square:"
    echo ""
    echo -e "${GREEN}${API_URL}/admin/clinics/${ORG_ID}/square/connect${NC}"
    echo ""
    echo -e "Press Enter after completing Square OAuth..."
    read -r
else
    echo -e "${YELLOW}Step 3: Square OAuth (skipped)${NC}"
    echo -e "Run this later: ${GREEN}${API_URL}/admin/clinics/${ORG_ID}/square/connect${NC}"
    echo ""
fi

# Step 4: Configure phone number
echo -e "${YELLOW}Step 4: Configure SMS phone number${NC}"
echo -n "Enter clinic phone number (E.164 format, e.g., +15551234567): "
read -r PHONE_NUMBER

if [[ -n "$PHONE_NUMBER" ]]; then
    PHONE_RESPONSE=$(curl -s -X PUT "${API_URL}/admin/clinics/${ORG_ID}/phone" \
        -H "Authorization: Bearer ${ADMIN_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{\"phone_number\": \"${PHONE_NUMBER}\"}")

    if echo "$PHONE_RESPONSE" | jq -e '.success' > /dev/null 2>&1; then
        echo -e "${GREEN}Phone number configured: ${PHONE_NUMBER}${NC}"
    else
        echo -e "${RED}Failed to configure phone:${NC}"
        echo "$PHONE_RESPONSE" | jq .
    fi
else
    echo -e "${YELLOW}Skipped phone configuration${NC}"
fi
echo ""

# Step 5: Seed knowledge
echo -e "${YELLOW}Step 5: Seed clinic knowledge${NC}"
echo "Would you like to seed sample knowledge now? (y/n): "
read -r SEED_KNOWLEDGE

if [[ "$SEED_KNOWLEDGE" == "y" || "$SEED_KNOWLEDGE" == "Y" ]]; then
    KNOWLEDGE_PAYLOAD=$(cat <<EOF
{
    "documents": [
        "${CLINIC_NAME} offers a variety of aesthetic services including Botox, dermal fillers, laser treatments, and skincare treatments.",
        "Our Botox treatments start at \$12 per unit. Most patients need 20-40 units for common treatment areas.",
        "Dermal filler treatments range from \$600-\$1200 depending on the type and amount of filler used.",
        "We require a \$50 deposit to book your appointment. This deposit is applied to your treatment cost.",
        "Our cancellation policy requires 24 hours notice. Deposits are non-refundable for no-shows or late cancellations.",
        "New patients should arrive 15 minutes early to complete paperwork. Existing patients can check in at their appointment time."
    ]
}
EOF
)

    TOKEN_HEADER=()
    if [[ -n "$ONBOARDING_TOKEN" ]]; then
        TOKEN_HEADER=(-H "X-Onboarding-Token: ${ONBOARDING_TOKEN}")
    fi

    KNOWLEDGE_RESPONSE=$(curl -s -X POST "${API_URL}/knowledge/${ORG_ID}" \
        -H "X-Org-Id: ${ORG_ID}" \
        -H "Content-Type: application/json" \
        "${TOKEN_HEADER[@]}" \
        -d "$KNOWLEDGE_PAYLOAD")

    echo -e "${GREEN}Knowledge seeded successfully${NC}"
else
    echo -e "${YELLOW}Skipped knowledge seeding${NC}"
    echo -e "Seed later with: POST ${API_URL}/knowledge/${ORG_ID}"
fi
echo ""

# Final status check
echo -e "${YELLOW}Final onboarding status:${NC}"
FINAL_STATUS=$(curl -s -X GET "${API_URL}/admin/clinics/${ORG_ID}/onboarding-status" \
    -H "Authorization: Bearer ${ADMIN_TOKEN}")

echo "$FINAL_STATUS" | jq '{
    clinic_name: .clinic_name,
    progress: .overall_progress,
    ready_for_launch: .ready_for_launch,
    next_action: .next_action
}'

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Onboarding Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Org ID:      ${GREEN}${ORG_ID}${NC}"
echo -e "Clinic Name: ${GREEN}${CLINIC_NAME}${NC}"
PROGRESS=$(echo "$FINAL_STATUS" | jq -r '.overall_progress')
READY=$(echo "$FINAL_STATUS" | jq -r '.ready_for_launch')
echo -e "Progress:    ${GREEN}${PROGRESS}%${NC}"

if [[ "$READY" == "true" ]]; then
    echo -e "Status:      ${GREEN}READY FOR LAUNCH${NC}"
else
    NEXT_ACTION=$(echo "$FINAL_STATUS" | jq -r '.next_action')
    echo -e "Status:      ${YELLOW}PENDING${NC}"
    echo -e "Next Step:   ${YELLOW}${NEXT_ACTION}${NC}"
fi

echo ""
echo -e "Save this org_id for future reference: ${GREEN}${ORG_ID}${NC}"
