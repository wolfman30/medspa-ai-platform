package leads

import "errors"

var (
	// ErrInvalidName is returned when the name is invalid
	ErrInvalidName = errors.New("name is required")

	// ErrMissingContact is returned when both email and phone are missing
	ErrMissingContact = errors.New("either email or phone is required")

	// ErrLeadNotFound is returned when a lead is not found
	ErrLeadNotFound = errors.New("lead not found")
)
