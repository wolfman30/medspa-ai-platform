package boulevard

import (
	"encoding/json"
	"time"
)

const (
	// Public widget GraphQL endpoint — no API key needed.
	publicGraphQLEndpoint = "https://www.joinblvd.com/b/.api/graph"
)

// Service represents a bookable Boulevard service from the cart's available categories.
type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	PriceCents  int    `json:"priceCents,omitempty"` // e.g. 37500 = $375.00
	DurationMin int    `json:"durationMin,omitempty"`
}

// Provider represents a Boulevard staff member/provider.
type Provider struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Name      string `json:"name"` // convenience: "FirstName LastName"
}

// TimeSlot represents a concrete bookable time slot.
type TimeSlot struct {
	ID      string    `json:"id"` // e.g. "t_2026-03-11T11:30:00"
	StartAt time.Time `json:"startAt"`
}

// StaffVariant represents a provider available for a specific time slot.
type StaffVariant struct {
	ID        string `json:"id"` // composite: "serviceId:staffId"
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// CartItem represents one selected service item in the booking cart.
type CartItem struct {
	ID   string `json:"id"`   // selected item ID (different from catalog ID)
	Name string `json:"name"` // service name
}

// Client is the patient/contact details passed to checkout.
type Client struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

// CreateBookingRequest is input for the full cart-based booking flow.
type CreateBookingRequest struct {
	ServiceID      string `json:"serviceId"`            // catalog service ID (s_...)
	ProviderID     string `json:"providerId,omitempty"` // staff variant ID
	BookableTimeID string `json:"bookableTimeId"`       // from GetBookableTimes (t_...)
	Client         Client `json:"client"`
	Notes          string `json:"notes,omitempty"`
}

// BookingResult is the outcome from checkout.
type BookingResult struct {
	BookingID string `json:"bookingId"`
	CartID    string `json:"cartId"`
	Status    string `json:"status,omitempty"`
}

// ---- GraphQL transport types ----

type graphQLRequest struct {
	OperationName string      `json:"operationName,omitempty"`
	Query         string      `json:"query"`
	Variables     interface{} `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}
