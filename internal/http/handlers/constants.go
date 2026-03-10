// Package handlers provides HTTP handler implementations for the MedSpa API.
package handlers

const (
	// DefaultPageSize is the number of items per page when no valid page_size
	// query parameter is provided.
	DefaultPageSize = 20

	// MaxPageSize is the upper bound for the page_size query parameter.
	// Requests exceeding this value are clamped to DefaultPageSize.
	MaxPageSize = 100

	// MaxUploadBytes is the maximum allowed size for multipart form uploads (10 MB).
	MaxUploadBytes = 10 << 20

	// USLocalDigits is the expected length of a US phone number without country code.
	USLocalDigits = 10

	// USFullDigits is the expected length of a US phone number with leading "1".
	USFullDigits = 11
)
