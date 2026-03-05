package moxie

import (
	"context"
	"fmt"
	"time"
)

// CreateAppointment creates an appointment directly via Moxie's GraphQL API.
// For new clients, no authentication is required. In dry-run mode, the request
// is logged and a fake success is returned without calling the Moxie API.
func (c *Client) CreateAppointment(ctx context.Context, req CreateAppointmentRequest) (*AppointmentResult, error) {
	if c.dryRun {
		c.logger.Info("DRY RUN: would create Moxie appointment",
			"medspa_id", req.MedspaID,
			"name", req.FirstName+" "+req.LastName,
			"email", req.Email,
			"phone", req.Phone,
			"services", fmt.Sprintf("%+v", req.Services),
			"is_new_client", req.IsNewClient,
		)
		return &AppointmentResult{
			OK:            true,
			Message:       "DRY_RUN",
			AppointmentID: "dry-run-" + fmt.Sprintf("%d", time.Now().UnixMilli()),
		}, nil
	}
	type serviceVar struct {
		ServiceMenuItemID string `json:"serviceMenuItemId"`
		ProviderID        string `json:"providerId"`
		StartTime         string `json:"startTime"`
		EndTime           string `json:"endTime"`
	}

	services := make([]serviceVar, len(req.Services))
	for i, s := range req.Services {
		services[i] = serviceVar{
			ServiceMenuItemID: s.ServiceMenuItemID,
			ProviderID:        s.ProviderID,
			StartTime:         s.StartTime,
			EndTime:           s.EndTime,
		}
	}

	variables := map[string]interface{}{
		"medspaId":                 req.MedspaID,
		"firstName":                req.FirstName,
		"lastName":                 req.LastName,
		"email":                    req.Email,
		"phone":                    req.Phone,
		"note":                     req.Note,
		"services":                 services,
		"bookingFlow":              "MAIA_BOOKING",
		"isNewClient":              req.IsNewClient,
		"noPreferenceProviderUsed": req.NoPreferenceProviderUsed,
	}

	query := `mutation createAppointmentByClient(
		$medspaId: ID!
		$firstName: String!
		$lastName: String!
		$email: String!
		$phone: String!
		$note: String!
		$services: [CreateAppointmentServiceInput!]!
		$bookingFlow: PublicBookingFlowTypeEnum
		$isNewClient: Boolean
		$noPreferenceProviderUsed: Boolean
	) {
		createAppointmentByClient(
			medspaId: $medspaId
			firstName: $firstName
			lastName: $lastName
			email: $email
			phone: $phone
			note: $note
			services: $services
			bookingFlow: $bookingFlow
			isNewClient: $isNewClient
			noPreferenceProviderUsed: $noPreferenceProviderUsed
		) {
			ok
			message
			clientAccessToken
			scheduledAppointment { id }
		}
	}`

	var resp struct {
		Data struct {
			CreateAppointmentByClient struct {
				OK                   bool   `json:"ok"`
				Message              string `json:"message"`
				ClientAccessToken    string `json:"clientAccessToken"`
				ScheduledAppointment *struct {
					ID string `json:"id"`
				} `json:"scheduledAppointment"`
			} `json:"createAppointmentByClient"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := c.doRequest(ctx, "createAppointmentByClient", variables, query, &resp); err != nil {
		return nil, fmt.Errorf("create appointment failed: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("moxie API error: %s", resp.Errors[0].Message)
	}

	r := resp.Data.CreateAppointmentByClient
	result := &AppointmentResult{
		OK:                r.OK,
		Message:           r.Message,
		ClientAccessToken: r.ClientAccessToken,
	}
	if r.ScheduledAppointment != nil {
		result.AppointmentID = r.ScheduledAppointment.ID
	}
	return result, nil
}
