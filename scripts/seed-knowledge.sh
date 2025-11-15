#!/bin/bash
set -e

# Simple bash version of knowledge seeding using curl
# Usage: ./scripts/seed-knowledge.sh [api-url] [clinic-id] [knowledge-file]

API_URL=${1:-http://localhost:8080}
CLINIC_ID=${2:-demo-radiance-medspa}
KNOWLEDGE_FILE=${3:-testdata/sample-clinic-knowledge.json}

echo "🌱 Seeding Knowledge Base"
echo "============================"
echo "API URL: $API_URL"
echo "Clinic ID: $CLINIC_ID"
echo "Knowledge file: $KNOWLEDGE_FILE"
echo ""

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "⚠️  Warning: 'jq' not found. Install for better output formatting."
    echo "   Ubuntu/Debian: sudo apt install jq"
    echo "   macOS: brew install jq"
    echo ""
fi

# Check if file exists
if [ ! -f "$KNOWLEDGE_FILE" ]; then
    echo "❌ Error: File not found: $KNOWLEDGE_FILE"
    exit 1
fi

# Extract and format documents
if command -v jq &> /dev/null; then
    # With jq: Create properly formatted documents array
    DOCUMENTS=$(jq -c '[.documents[] | "\(.title)\n\n\(.content)"]' "$KNOWLEDGE_FILE")

    # Count documents
    DOC_COUNT=$(echo "$DOCUMENTS" | jq 'length')
    echo "📄 Documents to upload: $DOC_COUNT"
    echo ""

    # Upload (in batches if needed)
    echo "📤 Uploading to $API_URL/knowledge/$CLINIC_ID..."

    RESPONSE=$(curl -s -X POST "$API_URL/knowledge/$CLINIC_ID" \
      -H "Content-Type: application/json" \
      -H "X-Org-Id: $CLINIC_ID" \
      -d "{\"documents\": $DOCUMENTS}")

    STATUS_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API_URL/knowledge/$CLINIC_ID" \
      -H "Content-Type: application/json" \
      -H "X-Org-Id: $CLINIC_ID" \
      -d "{\"documents\": $DOCUMENTS}")

    if [ "$STATUS_CODE" = "201" ]; then
        echo "✅ Success!"
        echo "$RESPONSE" | jq .
    else
        echo "❌ Failed (status code: $STATUS_CODE)"
        echo "$RESPONSE"
        exit 1
    fi
else
    # Without jq: Simple manual approach
    echo "⚠️  Running in basic mode (install jq for better experience)"
    echo "📤 Uploading..."

    curl -X POST "$API_URL/knowledge/$CLINIC_ID" \
      -H "Content-Type: application/json" \
      -H "X-Org-Id: $CLINIC_ID" \
      -d @- << 'EOF'
{
  "documents": [
    "Services & Pricing - Injectables\n\nBOTOX COSMETIC:\n- Price: $12 per unit\n- Typical areas require 20-50 units\n- Common treatment areas: Forehead lines (10-30 units), Frown lines (10-25 units), Crow's feet (5-15 units per side)\n- Results last 3-4 months\n- Treatment time: 10-15 minutes\n- No downtime required",
    "Booking Policies & Payment\n\nDEPOSIT POLICY:\n- New clients: $50 deposit required to secure appointment\n- Returning clients: No deposit required\n- Deposits are refundable and applied to service cost\n\nCANCELLATION POLICY:\n- Must cancel or reschedule at least 24 hours in advance\n- Cancellations with less than 24 hours notice forfeit deposit",
    "Location, Hours & Contact\n\nLOCATION:\nRadiance Medical Spa\n123 Beauty Boulevard, Suite 200\nScottsdale, AZ 85251\n\nHOURS:\n- Monday: 9am-6pm\n- Tuesday: 10am-7pm\n- Wednesday: 9am-6pm\n- Thursday: 10am-7pm\n- Friday: 9am-5pm\n- Saturday: 9am-4pm\n- Sunday: Closed"
  ]
}
EOF
fi

echo ""
echo "✅ Knowledge seeding complete!"
echo ""
echo "📝 Next steps:"
echo "  1. Test with a conversation:"
echo "     curl -X POST $API_URL/conversations/start \\"
echo "       -H 'Content-Type: application/json' \\"
echo "       -d '{\"clinicId\":\"$CLINIC_ID\",\"phone\":\"+15551234567\",\"message\":\"How much does Botox cost?\"}'"
echo ""
echo "  2. Check that AI response includes pricing from knowledge base"
