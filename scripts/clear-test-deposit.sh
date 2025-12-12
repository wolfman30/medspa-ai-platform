#!/usr/bin/env bash
set -euo pipefail

# Clears deposit-related state for a test phone number.
# Safe by default: runs a dry-run unless YES=1 is set.
#
# Usage:
#   ./scripts/clear-test-deposit.sh <phone>
#   YES=1 ./scripts/clear-test-deposit.sh <phone>
#   TEST_PHONE=<phone> YES=1 ./scripts/clear-test-deposit.sh

PHONE=${1:-${TEST_PHONE:-}}
if [[ -z "${PHONE}" ]]; then
  echo "Usage: $0 <phone>"
  echo "Example: YES=1 $0 5005550001"
  exit 1
fi

# Strip to digits for matching either +937... or +1 937...
DIGITS=$(echo "${PHONE}" | tr -cd '0-9')
if [[ -z "${DIGITS}" ]]; then
  echo "Invalid phone: ${PHONE}"
  exit 1
fi

# Choose docker compose command (v2 preferred).
DC="docker compose"
if ! docker compose version >/dev/null 2>&1; then
  DC="docker-compose"
fi

echo "Dry-run for phone digits: ${DIGITS}"
${DC} exec -T postgres psql -U medspa -d medspa -v digits="${DIGITS}" <<'SQL'
\set ON_ERROR_STOP on
WITH target_leads AS (
  SELECT id, org_id, phone, deposit_status, priority_level, created_at
  FROM leads
  WHERE regexp_replace(phone, '\D', '', 'g') LIKE '%' || :'digits'
)
SELECT * FROM target_leads ORDER BY created_at DESC;

WITH target_leads AS (
  SELECT id
  FROM leads
  WHERE regexp_replace(phone, '\D', '', 'g') LIKE '%' || :'digits'
)
SELECT count(*) AS payments_matching_phone
FROM payments
WHERE lead_id IN (SELECT id FROM target_leads);
SQL

echo ""
echo "Redis conversation keys matching digits: ${DIGITS}"
if ${DC} exec -T redis redis-cli PING >/dev/null 2>&1; then
  mapfile -t REDIS_KEYS < <(${DC} exec -T redis redis-cli --scan --pattern "conversation:*${DIGITS}*" || true)
  if [[ ${#REDIS_KEYS[@]} -eq 0 ]]; then
    echo "(none)"
  else
    printf '%s\n' "${REDIS_KEYS[@]}"
  fi
else
  echo "(redis not running; skipping)"
fi

if [[ "${YES:-0}" != "1" ]]; then
  echo ""
  echo "Dry-run only. Re-run with YES=1 to delete matching deposits."
  exit 0
fi

echo ""
echo "Deleting deposits and resetting lead deposit fields..."
${DC} exec -T postgres psql -U medspa -d medspa -v digits="${DIGITS}" <<'SQL'
\set ON_ERROR_STOP on
WITH target_leads AS (
  SELECT id
  FROM leads
  WHERE regexp_replace(phone, '\D', '', 'g') LIKE '%' || :'digits'
),
deleted_payments AS (
  DELETE FROM payments
  WHERE lead_id IN (SELECT id FROM target_leads)
  RETURNING id
),
reset_leads AS (
  UPDATE leads
  SET deposit_status = NULL,
      priority_level = 'normal'
  WHERE id IN (SELECT id FROM target_leads)
  RETURNING id
),
deleted_outbox AS (
  DELETE FROM outbox
  WHERE event_type LIKE 'payments.deposit.%'
    AND (payload->>'lead_id') IN (SELECT id::text FROM target_leads)
  RETURNING id
)
SELECT
  (SELECT count(*) FROM deleted_payments) AS deleted_payments,
  (SELECT count(*) FROM reset_leads) AS reset_leads,
  (SELECT count(*) FROM deleted_outbox) AS deleted_outbox;
SQL

echo ""
echo "Deleting redis conversation keys..."
if ${DC} exec -T redis redis-cli PING >/dev/null 2>&1; then
  mapfile -t REDIS_KEYS < <(${DC} exec -T redis redis-cli --scan --pattern "conversation:*${DIGITS}*" || true)
  if [[ ${#REDIS_KEYS[@]} -eq 0 ]]; then
    echo "(none)"
  else
    for key in "${REDIS_KEYS[@]}"; do
      if [[ -n "${key}" ]]; then
        ${DC} exec -T redis redis-cli DEL "${key}" >/dev/null
      fi
    done
    echo "Deleted ${#REDIS_KEYS[@]} key(s)."
  fi
else
  echo "(redis not running; skipping)"
fi

echo "Done."
