// Package moxie provides a direct GraphQL client for the Moxie booking platform.
// This replaces browser automation for booking creation, using Moxie's public API.
package moxie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	defaultGraphQLEndpoint = "https://graphql.joinmoxie.com/v1/graphql"
	defaultTimeout         = 15 * time.Second
)

// Client is a Moxie GraphQL API client.
type Client struct {
	endpoint   string
	httpClient *http.Client
	logger     *logging.Logger
	dryRun     bool // When true, CreateAppointment logs but doesn't actually create
}

// Option configures a Client.
type Option func(*Client)

// WithDryRun enables dry-run mode — CreateAppointment will log the request
// but return a fake success without calling Moxie's API.
func WithDryRun(dryRun bool) Option {
	return func(c *Client) {
		c.dryRun = dryRun
	}
}

// NewClient creates a new Moxie API client.
func NewClient(logger *logging.Logger, opts ...Option) *Client {
	c := &Client{
		endpoint: defaultGraphQLEndpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// TimeSlot represents an available appointment slot from Moxie.
type TimeSlot struct {
	Start string `json:"start"` // ISO 8601 with timezone, e.g. "2026-02-19T18:45:00-05:00"
	End   string `json:"end"`
}

// DateSlots represents available slots for a specific date.
type DateSlots struct {
	Date  string     `json:"date"` // YYYY-MM-DD
	Slots []TimeSlot `json:"slots"`
}

// AvailabilityResult is the response from querying available time slots.
type AvailabilityResult struct {
	Dates []DateSlots `json:"dates"`
}

// AppointmentResult is the response from creating an appointment.
type AppointmentResult struct {
	OK                bool   `json:"ok"`
	Message           string `json:"message"`
	ClientAccessToken string `json:"clientAccessToken"`
	AppointmentID     string `json:"appointmentId"`
}

// ServiceInput describes a service for appointment creation.
type ServiceInput struct {
	ServiceMenuItemID string `json:"serviceMenuItemId"`
	ProviderID        string `json:"providerId"`
	StartTime         string `json:"startTime"` // ISO 8601 UTC
	EndTime           string `json:"endTime"`   // ISO 8601 UTC
}

// CreateAppointmentRequest contains all data needed to create an appointment.
type CreateAppointmentRequest struct {
	MedspaID                 string
	FirstName                string
	LastName                 string
	Email                    string
	Phone                    string
	Note                     string
	Services                 []ServiceInput
	IsNewClient              bool
	NoPreferenceProviderUsed bool
}

// graphqlRequest is the generic GraphQL request body.
type graphqlRequest struct {
	OperationName string      `json:"operationName"`
	Variables     interface{} `json:"variables"`
	Query         string      `json:"query"`
}

// GetAvailableSlots queries Moxie for available time slots.
// If providerUserMedspaID is non-empty, slots are filtered to that provider.
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

// CreateAppointment creates an appointment directly via Moxie's GraphQL API.
// For new clients, no authentication is required.
// In dry-run mode, logs the request and returns a fake success without calling Moxie.
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

// doRequest executes a GraphQL request against Moxie's API.
func (c *Client) doRequest(ctx context.Context, operationName string, variables interface{}, query string, result interface{}) error {
	body := graphqlRequest{
		OperationName: operationName,
		Variables:     variables,
		Query:         query,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.joinmoxie.com")
	req.Header.Set("Referer", "https://app.joinmoxie.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("moxie API returned %d: %s", resp.StatusCode, string(respBody[:min(200, len(respBody))]))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}
