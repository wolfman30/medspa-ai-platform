package boulevard

import "time"

const (
	// publicGraphQLEndpoint is Boulevard's public booking widget API.
	publicGraphQLEndpoint = "https://www.joinblvd.com/b/.api/graph"
)

// Service represents a bookable Boulevard service.
type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CategoryID  string `json:"categoryId,omitempty"`
	DurationMin int    `json:"durationMin,omitempty"`
}

// ServiceCategory groups services returned from cart creation.
type ServiceCategory struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Services []Service `json:"services,omitempty"`
}

// Provider represents a Boulevard staff member/provider.
type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BookableDate represents a date that has availability.
type BookableDate struct {
	Date string `json:"date"` // "YYYY-MM-DD"
}

// BookableTime represents an available time slot.
type BookableTime struct {
	ID        string `json:"id"`
	StartTime string `json:"startTime"` // ISO 8601
}

// StaffVariant represents a provider available for a specific time slot.
type StaffVariant struct {
	ID    string `json:"id"`
	Staff struct {
		ID        string `json:"id"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"staff"`
}

// TimeSlot represents a concrete time slot.
type TimeSlot struct {
	ID      string    `json:"id"` // bookableTimeId
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

// CartItem represents one service item in the booking cart.
type CartItem struct {
	ID         string `json:"id"`        // selected item ID (different from catalog ID)
	ServiceID  string `json:"serviceId"` // catalog service ID
	ProviderID string `json:"providerId,omitempty"`
}

// Cart is the Boulevard booking cart.
type Cart struct {
	ID                  string            `json:"id"`
	Token               string            `json:"token,omitempty"`
	Items               []CartItem        `json:"items,omitempty"`
	AvailableCategories []ServiceCategory `json:"availableCategories,omitempty"`
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
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// --- Response payload types for each API operation ---

type createCartData struct {
	CreateCart struct {
		Cart struct {
			ID    string `json:"id"`
			Token string `json:"token"`
			// AvailableCategories contains all bookable services
			AvailableCategories []struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				AvailableItems []struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"availableItems"`
			} `json:"availableCategories"`
		} `json:"cart"`
	} `json:"createCart"`
}

type addSelectedItemData struct {
	CartAddSelectedBookableItem struct {
		Cart struct {
			ID            string `json:"id"`
			SelectedItems []struct {
				ID           string `json:"id"`
				SelectedItem struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"selectedItem,omitempty"`
			} `json:"selectedItems"`
		} `json:"cart"`
	} `json:"cartAddSelectedBookableItem"`
}

type bookableDatesData struct {
	CartBookableDates []struct {
		Date string `json:"date"`
	} `json:"cartBookableDates"`
}

type bookableTimesData struct {
	CartBookableTimes []struct {
		ID        string `json:"id"`
		StartTime string `json:"startTime"`
	} `json:"cartBookableTimes"`
}

type staffVariantsData struct {
	CartBookableStaffVariants struct {
		StaffVariants []struct {
			ID    string `json:"id"`
			Staff struct {
				ID        string `json:"id"`
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
			} `json:"staff"`
		} `json:"staffVariants"`
	} `json:"cartBookableStaffVariants"`
}

type reserveItemsData struct {
	ReserveCartBookableItems struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"reserveCartBookableItems"`
}

type updateCartData struct {
	UpdateCart struct {
		Cart struct {
			ID string `json:"id"`
		} `json:"cart"`
	} `json:"updateCart"`
}

type checkoutCartData struct {
	CheckoutCart struct {
		Appointments []struct {
			ID                        string `json:"id"`
			StartAt                   string `json:"startAt"`
			AppointmentServiceOptions []struct {
				ID string `json:"id"`
			} `json:"appointmentServiceOptions"`
		} `json:"appointments"`
	} `json:"checkoutCart"`
}
