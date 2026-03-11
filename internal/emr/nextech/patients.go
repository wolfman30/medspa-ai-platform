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

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// CreatePatient creates a new patient record in Nextech.
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

// GetPatient retrieves a patient by ID.
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

// SearchPatients searches for patients by phone, email, or name.
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
