package nextech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// GetAvailability retrieves available appointment slots.
// Nextech FHIR: GET /Slot?schedule={scheduleID}&start={start}&end={end}&status=free
func (c *Client) GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	// Build query parameters
	params := url.Values{}
	if req.ProviderID != "" {
		params.Set("schedule", req.ProviderID) // FHIR schedule reference
	}
	params.Set("start", req.StartDate.Format(time.RFC3339))
	params.Set("end", req.EndDate.Format(time.RFC3339))
	params.Set("status", "free")

	endpoint := fmt.Sprintf("%s/Slot?%s", c.baseURL, params.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("nextech: failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("Accept", "application/fhir+json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nextech: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nextech: API error (status %d): %s", resp.StatusCode, string(body))
	}

	var bundle FHIRBundle
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		return nil, fmt.Errorf("nextech: failed to decode response: %w", err)
	}

	return c.parseSlots(bundle)
}

// CreateAppointment books an appointment in Nextech.
// Nextech FHIR: POST /Appointment
func (c *Client) CreateAppointment(ctx context.Context, req emr.AppointmentRequest) (*emr.Appointment, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	// Build FHIR Appointment resource
	fhirAppt := FHIRAppointment{
		ResourceType: "Appointment",
		Status:       req.Status,
		Start:        req.StartTime.Format(time.RFC3339),
		End:          req.EndTime.Format(time.RFC3339),
		Description:  req.Notes,
		Participant: []FHIRParticipant{
			{
				Actor: FHIRReference{
					Reference: fmt.Sprintf("Patient/%s", req.PatientID),
				},
				Status: "accepted",
			},
			{
				Actor: FHIRReference{
					Reference: fmt.Sprintf("Practitioner/%s", req.ProviderID),
				},
				Status: "accepted",
			},
		},
		Slot: []FHIRReference{
			{Reference: fmt.Sprintf("Slot/%s", req.SlotID)},
		},
	}

	if req.ServiceType != "" {
		fhirAppt.ServiceType = []FHIRCodeableConcept{
			{
				Coding: []FHIRCoding{
					{
						System:  "http://terminology.hl7.org/CodeSystem/service-type",
						Code:    req.ServiceType,
						Display: req.ServiceType,
					},
				},
			},
		}
	}

	body, err := json.Marshal(fhirAppt)
	if err != nil {
		return nil, fmt.Errorf("nextech: failed to marshal appointment: %w", err)
	}

	endpoint := fmt.Sprintf("%s/Appointment", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("nextech: failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("Content-Type", "application/fhir+json")
	httpReq.Header.Set("Accept", "application/fhir+json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nextech: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nextech: API error (status %d): %s", resp.StatusCode, string(body))
	}

	var createdAppt FHIRAppointment
	if err := json.NewDecoder(resp.Body).Decode(&createdAppt); err != nil {
		return nil, fmt.Errorf("nextech: failed to decode response: %w", err)
	}

	return c.parseFHIRAppointment(createdAppt), nil
}

// GetAppointment retrieves an appointment by ID.
// Nextech FHIR: GET /Appointment/{id}
func (c *Client) GetAppointment(ctx context.Context, appointmentID string) (*emr.Appointment, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s/Appointment/%s", c.baseURL, appointmentID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("nextech: failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("Accept", "application/fhir+json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nextech: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nextech: API error (status %d): %s", resp.StatusCode, string(body))
	}

	var appt FHIRAppointment
	if err := json.NewDecoder(resp.Body).Decode(&appt); err != nil {
		return nil, fmt.Errorf("nextech: failed to decode response: %w", err)
	}

	return c.parseFHIRAppointment(appt), nil
}

// CancelAppointment cancels an existing appointment.
// Nextech FHIR: PUT /Appointment/{id} with status=cancelled
func (c *Client) CancelAppointment(ctx context.Context, appointmentID string) error {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return fmt.Errorf("nextech: authentication failed: %w", err)
	}

	// First, get the existing appointment
	existing, err := c.GetAppointment(ctx, appointmentID)
	if err != nil {
		return fmt.Errorf("nextech: failed to get appointment: %w", err)
	}

	// Update status to cancelled
	fhirAppt := FHIRAppointment{
		ResourceType: "Appointment",
		ID:           appointmentID,
		Status:       "cancelled",
		Start:        existing.StartTime.Format(time.RFC3339),
		End:          existing.EndTime.Format(time.RFC3339),
	}

	body, err := json.Marshal(fhirAppt)
	if err != nil {
		return fmt.Errorf("nextech: failed to marshal appointment: %w", err)
	}

	endpoint := fmt.Sprintf("%s/Appointment/%s", c.baseURL, appointmentID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nextech: failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("Content-Type", "application/fhir+json")
	httpReq.Header.Set("Accept", "application/fhir+json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("nextech: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nextech: API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
