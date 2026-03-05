package moxie

import (
	"context"
	"fmt"
)

// GetAvailableSlots queries Moxie for available appointment time slots within the
// given date range for a specific service. If providerUserMedspaID is provided and
// non-empty, slots are filtered to that provider; otherwise availability is returned
// across all eligible providers (noPreference mode).
func (c *Client) GetAvailableSlots(ctx context.Context, medspaID string, startDate, endDate string, serviceMenuItemID string, noPreference bool, providerUserMedspaID ...string) (*AvailabilityResult, error) {
	type serviceVar struct {
		ServiceMenuItemID string `json:"serviceMenuItemId"`
		NoPreference      bool   `json:"noPreference"`
		Order             int    `json:"order"`
		ProviderID        string `json:"providerId,omitempty"`
	}

	// Moxie's availableTimeSlots uses "providerId" (not "providerUserMedspaId")
	// for provider-specific availability. The value is the provider's userMedspaId.
	svc := serviceVar{ServiceMenuItemID: serviceMenuItemID, NoPreference: noPreference, Order: 1}
	if len(providerUserMedspaID) > 0 && providerUserMedspaID[0] != "" {
		svc.ProviderID = providerUserMedspaID[0]
		svc.NoPreference = false
	}

	variables := map[string]interface{}{
		"medspaId":  medspaID,
		"startDate": startDate,
		"endDate":   endDate,
		"services":  []serviceVar{svc},
	}

	query := `query AvailableTimeSlots($medspaId: ID!, $startDate: Date!, $endDate: Date!, $services: [CheckAvailabilityAppointmentServiceInput!]!) {
		availableTimeSlots(medspaId: $medspaId, startDate: $startDate, endDate: $endDate, services: $services) {
			dates {
				date
				slots { start end }
				__typename
			}
			__typename
		}
	}`

	var resp struct {
		Data struct {
			AvailableTimeSlots struct {
				Dates []struct {
					Date  string `json:"date"`
					Slots []struct {
						Start string `json:"start"`
						End   string `json:"end"`
					} `json:"slots"`
				} `json:"dates"`
			} `json:"availableTimeSlots"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := c.doRequest(ctx, "AvailableTimeSlots", variables, query, &resp); err != nil {
		return nil, fmt.Errorf("availability query failed: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("moxie API error: %s", resp.Errors[0].Message)
	}

	result := &AvailabilityResult{}
	for _, d := range resp.Data.AvailableTimeSlots.Dates {
		ds := DateSlots{Date: d.Date}
		for _, s := range d.Slots {
			ds.Slots = append(ds.Slots, TimeSlot{Start: s.Start, End: s.End})
		}
		result.Dates = append(result.Dates, ds)
	}
	return result, nil
}
