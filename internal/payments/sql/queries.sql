-- name: InsertPayment :one
INSERT INTO payments (
    id,
    org_id,
    lead_id,
    provider,
    provider_ref,
    booking_intent_id,
    amount_cents,
    status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdatePaymentStatusByProviderRef :one
UPDATE payments
SET status = $2,
    provider_ref = COALESCE($3, provider_ref)
WHERE provider_ref = $1
RETURNING *;

-- name: UpdatePaymentStatusByID :one
UPDATE payments
SET status = $2,
    provider_ref = COALESCE($3, provider_ref)
WHERE id = $1
RETURNING *;

-- name: GetPaymentByProviderRef :one
SELECT * FROM payments
WHERE provider_ref = $1;

-- name: GetPaymentByID :one
SELECT * FROM payments
WHERE id = $1;

