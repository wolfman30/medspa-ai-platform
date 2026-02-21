package boulevard

import "time"

const (
	defaultGraphQLEndpoint = "https://dashboard.joinblvd.com/api/2020-01/graphql"
)

// Service represents a bookable Boulevard service.
type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DurationMin int    `json:"durationMin,omitempty"`
}

// Provider represents a Boulevard staff member/provider.
type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TimeSlot represents a concrete time slot.
type TimeSlot struct {
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

// CartItem represents one service item in the booking cart.
type CartItem struct {
	ID         string `json:"id"`
	ServiceID  string `json:"serviceId"`
	ProviderID string `json:"providerId,omitempty"`
}

// Cart is the Boulevard booking cart.
type Cart struct {
	ID    string     `json:"id"`
	Items []CartItem `json:"items,omitempty"`
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
	ServiceID  string    `json:"serviceId"`
	ProviderID string    `json:"providerId,omitempty"`
	StartAt    time.Time `json:"startAt"`
	Client     Client    `json:"client"`
	Notes      string    `json:"notes,omitempty"`
}

// BookingResult is the outcome from checkout.
type BookingResult struct {
	BookingID string `json:"bookingId"`
	CartID    string `json:"cartId"`
	Status    string `json:"status,omitempty"`
}

type graphQLRequest struct {
	OperationName string      `json:"operationName,omitempty"`
	Query         string      `json:"query"`
	Variables     interface{} `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse[T any] struct {
	Data   T             `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// Narrow response payloads for each API operation.
type servicesData struct {
	Services []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Duration int    `json:"duration"`
	} `json:"services"`
}

type providersData struct {
	Staff []struct {
		ID       string `json:"id"`
		FullName string `json:"fullName"`
	} `json:"staff"`
}

type createCartData struct {
	CreateCart struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"createCart"`
}

type addServiceData struct {
	CartAddService struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"cartAddService"`
}

type availableSlotsData struct {
	CartAvailableTimeSlots []struct {
		StartAt string `json:"startAt"`
		EndAt   string `json:"endAt"`
	} `json:"cartAvailableTimeSlots"`
}

type reserveSlotData struct {
	CartReserveTimeSlot struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"cartReserveTimeSlot"`
}

type addClientData struct {
	CartSetClient struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"cartSetClient"`
}

type checkoutData struct {
	CartCheckout struct {
		Booking struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"booking"`
	} `json:"cartCheckout"`
}
