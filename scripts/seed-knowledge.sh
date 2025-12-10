#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/seed-knowledge.sh [api-url] [clinic-id] [knowledge-file]

API_URL=${1:-http://localhost:8080}
CLINIC_ID=${2:-demo-radiance-medspa}
KNOWLEDGE_FILE=${3:-testdata/demo-clinic-knowledge.json}

echo "Seeding knowledge base"
echo "API URL      : $API_URL"
echo "Clinic ID    : $CLINIC_ID"
echo "Knowledge doc: $KNOWLEDGE_FILE"
echo ""

if ! command -v jq >/dev/null 2>&1; then
  echo "Error: jq is required for this script. Install jq and retry."
  exit 1
fi

if [ ! -f "$KNOWLEDGE_FILE" ]; then
  echo "Error: file not found: $KNOWLEDGE_FILE"
  exit 1
fi

DOCUMENTS=$(jq -c '[.documents[] | "\(.title)\n\n\(.content)"]' "$KNOWLEDGE_FILE")
DOC_COUNT=$(echo "$DOCUMENTS" | jq 'length')

echo "Uploading $DOC_COUNT snippets..."
PAYLOAD=$(printf '{"documents":%s}' "$DOCUMENTS")

TMP_RESPONSE=$(mktemp)
STATUS_CODE=$(curl -s -o "$TMP_RESPONSE" -w "%{http_code}" \
  -X POST "$API_URL/knowledge/$CLINIC_ID" \
  -H "Content-Type: application/json" \
  -H "X-Org-Id: $CLINIC_ID" \
  -d "$PAYLOAD")

if [ "$STATUS_CODE" = "201" ]; then
  echo "Seed completed."
  cat "$TMP_RESPONSE" | jq .
else
  echo "Seed failed (status $STATUS_CODE):"
  cat "$TMP_RESPONSE"
  rm -f "$TMP_RESPONSE"
  exit 1
fi

rm -f "$TMP_RESPONSE"

echo ""
echo "Next steps:"
echo "  - Trigger a conversation and confirm responses include clinic-specific details."
echo "  - Use docs/E2E_TEST_RESULTS.md to record what you verified."
