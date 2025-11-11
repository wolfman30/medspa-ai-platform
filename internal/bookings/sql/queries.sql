-- name: InsertBooking :one
INSERT INTO bookings (
    id,
    org_id,
    lead_id,
    status,
    confirmed_at,
    scheduled_for
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBookingForOrg :one
SELECT * FROM bookings
WHERE id = $1
  AND org_id = $2;
