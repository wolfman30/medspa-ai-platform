#!/usr/bin/env bash
set -euo pipefail

# End-to-end smoke for SMS -> missed-call trigger -> knowledge seed -> checkout link.
# Requires API running locally and jq installed.

API_URL=${API_URL:-http://localhost:8080}
ORG_ID=${ORG_ID:-demo-radiance-medspa}
FROM_NUMBER=${FROM_NUMBER:-+15550002222}  # patient
TO_NUMBER=${TO_NUMBER:-+15559998888}      # clinic hosted number
KNOWLEDGE_FILE=${KNOWLEDGE_FILE:-testdata/demo-clinic-knowledge.json}
DEPOSIT_CENTS=${DEPOSIT_CENTS:-5000}

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for this script."
  exit 1
fi

STEP=1
step() {
  echo ""
  echo "[$STEP] $1"
  echo "------------------------------------"
  STEP=$((STEP + 1))
}

curl_json() {
  local method=$1
  local url=$2
  local body=$3
  curl -s -o /tmp/e2e_resp.json -w "%{http_code}" \
    -X "$method" "$url" \
    -H "Content-Type: application/json" \
    -H "X-Org-Id: $ORG_ID" \
    -d "$body"
}

step "Health check"
HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/health")
if [ "$HEALTH_STATUS" != "200" ]; then
  echo "API unhealthy (status $HEALTH_STATUS) at $API_URL"
  exit 1
fi
echo "API is up."

step "Seed knowledge"
./scripts/seed-knowledge.sh "$API_URL" "$ORG_ID" "$KNOWLEDGE_FILE"

step "Create test lead"
LEAD_PAYLOAD=$(jq -n --arg phone "$FROM_NUMBER" '{name:"E2E Test", phone:$phone, email:"e2e@test.dev", message:"test lead", source:"e2e"}')
STATUS=$(curl_json POST "$API_URL/leads/web" "$LEAD_PAYLOAD")
if [ "$STATUS" != "200" ] && [ "$STATUS" != "201" ]; then
  echo "Lead creation failed (status $STATUS):"
  cat /tmp/e2e_resp.json
  exit 1
fi
LEAD_ID=$(jq -r '.id // empty' /tmp/e2e_resp.json)
echo "Lead created: ${LEAD_ID:-unknown}"

step "Trigger hosted missed-call webhook (Telnyx)"
VOICE_EVENT=$(jq -n --arg from "$FROM_NUMBER" --arg to "$TO_NUMBER" '{
  data:{
    id:"evt_voice_cli",
    event_type:"call.hangup",
    occurred_at: (now | todate),
    payload:{
      id:"call_cli",
      status:"no_answer",
      hangup_cause:"no_answer",
      from:{phone_number:$from},
      to:[{phone_number:$to}]
    }
  }
}')
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$API_URL/webhooks/telnyx/voice" \
  -H "Content-Type: application/json" \
  -H "Telnyx-Timestamp: $(date +%s)" \
  -H "Telnyx-Signature: test" \
  -d "$VOICE_EVENT")
echo "Voice webhook status: $STATUS"

step "Simulate inbound SMS (Telnyx message.received)"
MESSAGE_EVENT=$(jq -n --arg from "$FROM_NUMBER" --arg to "$TO_NUMBER" '{
  data:{
    id:"evt_msg_cli",
    event_type:"message.received",
    occurred_at:(now | todate),
    payload:{
      id:"msg_cli",
      direction:"inbound",
      text:"I want weekday afternoon Botox",
      status:"received",
      from:{phone_number:$from},
      to:[{phone_number:$to}]
    }
  }
}')
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$API_URL/webhooks/telnyx/messages" \
  -H "Content-Type: application/json" \
  -H "Telnyx-Timestamp: $(date +%s)" \
  -H "Telnyx-Signature: test" \
  -d "$MESSAGE_EVENT")
echo "Message webhook status: $STATUS"

step "Create Square checkout for the lead (manual payment step)"
CHECKOUT_PAYLOAD=$(jq -n --arg lead "$LEAD_ID" --argjson amount "$DEPOSIT_CENTS" '{lead_id:$lead, amount_cents:$amount}')
STATUS=$(curl_json POST "$API_URL/payments/checkout" "$CHECKOUT_PAYLOAD")
if [ "$STATUS" != "200" ]; then
  echo "Checkout creation failed (status $STATUS):"
  cat /tmp/e2e_resp.json
  exit 1
fi
CHECKOUT_URL=$(jq -r '.checkout_url // empty' /tmp/e2e_resp.json)
echo "Checkout URL: $CHECKOUT_URL"
echo "Open the URL, pay the deposit, then capture the payment id from Square webhook logs to replay if needed."

echo ""
echo "Script complete. Capture results in docs/E2E_TEST_RESULTS.md."
