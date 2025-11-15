package nextech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// Client implements the emr.Client interface for Nextech EMR
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	// OAuth 2.0 token management
	accessToken string
	tokenExpiry time.Time
}

// Config holds configuration for the Nextech client
type Config struct {
	BaseURL      string // e.g., "https://api.nextech.com" or sandbox URL
	ClientID     string // OAuth 2.0 client ID
	ClientSecret string // OAuth 2.0 client secret
	Timeout      time.Duration
}

// New creates a new Nextech EMR client
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("nextech: BaseURL is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("nextech: ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("nextech: ClientSecret is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &Client{
		baseURL:      strings.TrimSuffix(cfg.BaseURL, "/"),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}

	return client, nil
}

// GetAvailability retrieves available appointment slots
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

// CreateAppointment books an appointment in Nextech
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

// GetAppointment retrieves an appointment by ID
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

// CancelAppointment cancels an existing appointment
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

// CreatePatient creates a new patient record in Nextech
// Nextech FHIR: POST /Patient
func (c *Client) CreatePatient(ctx context.Context, patient emr.Patient) (*emr.Patient, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	// Build FHIR Patient resource
	fhirPatient := FHIRPatient{
		ResourceType: "Patient",
		Name: []FHIRHumanName{
			{
				Use:    "official",
				Family: patient.LastName,
				Given:  []string{patient.FirstName},
			},
		},
		Gender:    patient.Gender,
		BirthDate: patient.DateOfBirth.Format("2006-01-02"),
	}

	// Add contact information
	if patient.Phone != "" {
		fhirPatient.Telecom = append(fhirPatient.Telecom, FHIRContactPoint{
			System: "phone",
			Value:  patient.Phone,
			Use:    "mobile",
		})
	}
	if patient.Email != "" {
		fhirPatient.Telecom = append(fhirPatient.Telecom, FHIRContactPoint{
			System: "email",
			Value:  patient.Email,
		})
	}

	// Add address
	if patient.Address.Line1 != "" {
		fhirPatient.Address = []FHIRAddress{
			{
				Use:        "home",
				Line:       []string{patient.Address.Line1, patient.Address.Line2},
				City:       patient.Address.City,
				State:      patient.Address.State,
				PostalCode: patient.Address.PostalCode,
				Country:    patient.Address.Country,
			},
		}
	}

	body, err := json.Marshal(fhirPatient)
	if err != nil {
		return nil, fmt.Errorf("nextech: failed to marshal patient: %w", err)
	}

	endpoint := fmt.Sprintf("%s/Patient", c.baseURL)
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

	var createdPatient FHIRPatient
	if err := json.NewDecoder(resp.Body).Decode(&createdPatient); err != nil {
		return nil, fmt.Errorf("nextech: failed to decode response: %w", err)
	}

	return c.parseFHIRPatient(createdPatient), nil
}

// GetPatient retrieves a patient by ID
// Nextech FHIR: GET /Patient/{id}
func (c *Client) GetPatient(ctx context.Context, patientID string) (*emr.Patient, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s/Patient/%s", c.baseURL, patientID)
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

	var patient FHIRPatient
	if err := json.NewDecoder(resp.Body).Decode(&patient); err != nil {
		return nil, fmt.Errorf("nextech: failed to decode response: %w", err)
	}

	return c.parseFHIRPatient(patient), nil
}

// SearchPatients searches for patients by phone, email, or name
// Nextech FHIR: GET /Patient?phone={phone}&email={email}&name={name}
func (c *Client) SearchPatients(ctx context.Context, query emr.PatientSearchQuery) ([]emr.Patient, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, fmt.Errorf("nextech: authentication failed: %w", err)
	}

	params := url.Values{}
	if query.Phone != "" {
		params.Set("telecom", query.Phone)
	}
	if query.Email != "" {
		params.Set("email", query.Email)
	}
	if query.FirstName != "" || query.LastName != "" {
		name := strings.TrimSpace(query.FirstName + " " + query.LastName)
		params.Set("name", name)
	}

	if len(params) == 0 {
		return nil, fmt.Errorf("nextech: at least one search parameter is required")
	}

	endpoint := fmt.Sprintf("%s/Patient?%s", c.baseURL, params.Encode())
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

	patients := make([]emr.Patient, 0, len(bundle.Entry))
	for _, entry := range bundle.Entry {
		// Convert interface{} to FHIRPatient via JSON marshaling
		var fhirPatient FHIRPatient
		if data, err := json.Marshal(entry.Resource); err == nil {
			if err := json.Unmarshal(data, &fhirPatient); err == nil {
				// Only process if it's actually a Patient resource
				if fhirPatient.ResourceType == "Patient" {
					patients = append(patients, *c.parseFHIRPatient(fhirPatient))
				}
			}
		}
	}

	return patients, nil
}

// ensureAuthenticated ensures we have a valid access token
func (c *Client) ensureAuthenticated(ctx context.Context) error {
	// Check if token is still valid (with 5-minute buffer)
	if c.accessToken != "" && time.Now().Add(5*time.Minute).Before(c.tokenExpiry) {
		return nil
	}

	// Request new token using OAuth 2.0 client credentials flow
	return c.authenticate(ctx)
}

// authenticate performs OAuth 2.0 client credentials authentication
func (c *Client) authenticate(ctx context.Context) error {
	tokenURL := c.baseURL + "/connect/token"

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("scope", "patient/*.read patient/*.write appointment/*.read appointment/*.write slot/*.read")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return nil
}
