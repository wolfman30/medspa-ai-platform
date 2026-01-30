#!/bin/bash
# Configure Forever 22 Med Spa's AI Persona

# Forever 22 org ID
ORG_ID="d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599"

# Development API URL
API_URL="https://dev.aiwolfsolutions.com"

# Get admin JWT token (you'll need to provide this)
# TOKEN="your-admin-jwt-token"

# The AI persona configuration for Forever 22 / Brandi Sesock
read -r -d '' PAYLOAD << 'EOF'
{
  "ai_persona": {
    "provider_name": "Brandi",
    "is_solo_operator": false,
    "tone": "warm",
    "custom_greeting": "Hi! This is Brandi's AI assistant at Forever 22 Med Spa. Brandi is currently with a patient, so I'm here to help you get started. What can I help you with today?",
    "busy_message": "Brandi is currently in a treatment with a patient and can't come to the phone right now.",
    "special_services": ["hyperhidrosis treatment", "migraine relief (Trapezius injections)", "GLP-1 weight loss counseling"]
  }
}
EOF

echo "Forever 22 AI Persona Configuration:"
echo "$PAYLOAD" | jq .

echo ""
echo "To apply this configuration, run:"
echo "curl -X PUT \"${API_URL}/admin/clinics/${ORG_ID}/config\" \\"
echo "  -H \"Authorization: Bearer \$TOKEN\" \\"
echo "  -H \"Content-Type: application/json\" \\"
echo "  -d '\$PAYLOAD'"
