#!/bin/bash
# Configure Forever 22 Med Spa's Moxie Booking Settings
# This script updates the clinic configuration to use Moxie for booking instead of Square

# Forever 22 org ID
ORG_ID="d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599"

# Development API URL
API_URL="https://dev.aiwolfsolutions.com"

# The Moxie booking configuration for Forever 22
# NOTE: Update BOOKING_URL with the actual Forever 22 Moxie booking page URL
read -r -d '' PAYLOAD << 'EOF'
{
  "booking_platform": "moxie",
  "booking_url": "https://forever22medspa.moxie.com/book"
}
EOF

echo "Forever 22 Moxie Booking Configuration:"
echo "$PAYLOAD" | jq .

echo ""
echo "To apply this configuration, run:"
echo "curl -X PUT \"${API_URL}/admin/clinics/${ORG_ID}/config\" \\"
echo "  -H \"Authorization: Bearer \$TOKEN\" \\"
echo "  -H \"Content-Type: application/json\" \\"
echo "  -d '\$PAYLOAD'"

echo ""
echo "Example with actual token (replace YOUR_ADMIN_JWT_TOKEN):"
echo ""
cat << 'EXAMPLE'
TOKEN="YOUR_ADMIN_JWT_TOKEN"
curl -X PUT "https://dev.aiwolfsolutions.com/admin/clinics/d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599/config" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "booking_platform": "moxie",
    "booking_url": "https://forever22medspa.moxie.com/book"
  }'
EXAMPLE
