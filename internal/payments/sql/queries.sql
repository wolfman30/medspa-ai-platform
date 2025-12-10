-- name: InsertPayment :one
INSERT INTO payments (
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at;

-- name: UpdatePaymentStatusByProviderRef :one
UPDATE payments
SET status = $2,
    provider_ref = COALESCE($3, provider_ref)
WHERE provider_ref = $1
RETURNING
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at;

-- name: UpdatePaymentStatusByID :one
UPDATE payments
SET status = $2,
    provider_ref = COALESCE($3, provider_ref)
WHERE id = $1
RETURNING
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at;

-- name: GetPaymentByProviderRef :one
SELECT
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at
FROM payments
WHERE provider_ref = $1;

-- name: GetPaymentByID :one
SELECT
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at
FROM payments
WHERE id = $1;

-- name: GetOpenDepositByOrgAndLead :one
-- Returns the most recent pending or succeeded deposit for an org/lead within 72 hours.
-- Used to prevent duplicate payment links after successful payment.
SELECT
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status,
    scheduled_for,
    created_at
FROM payments
WHERE org_id = $1
  AND lead_id = $2
  AND status IN ('deposit_pending', 'succeeded')
  AND created_at >= now() - interval '72 hours'
ORDER BY created_at DESC
LIMIT 1;

