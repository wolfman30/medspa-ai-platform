package nextech

import (
	"encoding/json"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// FHIR Resource models for Nextech API (FHIR STU 3 / 3.0.1)

// FHIRBundle represents a FHIR Bundle resource (search results container)
type FHIRBundle struct {
	ResourceType string `json:"resourceType"`
	Type         string `json:"type"` // "searchset", "collection", etc.
	Total        int    `json:"total"`
	Entry        []struct {
		Resource interface{} `json:"resource"`
	} `json:"entry"`
}

// FHIRAppointment represents a FHIR Appointment resource
type FHIRAppointment struct {
	ResourceType string                `json:"resourceType"`
	ID           string                `json:"id,omitempty"`
	Status       string                `json:"status"` // proposed, pending, booked, arrived, fulfilled, cancelled
	ServiceType  []FHIRCodeableConcept `json:"serviceType,omitempty"`
	Description  string                `json:"description,omitempty"`
	Start        string                `json:"start"` // RFC3339 datetime
	End          string                `json:"end"`   // RFC3339 datetime
	Participant  []FHIRParticipant     `json:"participant"`
	Slot         []FHIRReference       `json:"slot,omitempty"`
	Created      string                `json:"created,omitempty"`
	Meta         *FHIRMeta             `json:"meta,omitempty"`
}

// FHIRSlot represents a FHIR Slot resource
type FHIRSlot struct {
	ResourceType string        `json:"resourceType"`
	ID           string        `json:"id"`
	Schedule     FHIRReference `json:"schedule"` // Reference to Schedule resource
	Status       string        `json:"status"`   // free, busy, busy-unavailable, busy-tentative
	Start        string        `json:"start"`    // RFC3339 datetime
	End          string        `json:"end"`      // RFC3339 datetime
	ServiceType  []FHIRCoding  `json:"serviceType,omitempty"`
	Meta         *FHIRMeta     `json:"meta,omitempty"`
}

// FHIRPatient represents a FHIR Patient resource
type FHIRPatient struct {
	ResourceType string             `json:"resourceType"`
	ID           string             `json:"id,omitempty"`
	Name         []FHIRHumanName    `json:"name"`
	Gender       string             `json:"gender,omitempty"`    // male, female, other, unknown
	BirthDate    string             `json:"birthDate,omitempty"` // YYYY-MM-DD
	Telecom      []FHIRContactPoint `json:"telecom,omitempty"`
	Address      []FHIRAddress      `json:"address,omitempty"`
	Meta         *FHIRMeta          `json:"meta,omitempty"`
}

// FHIRParticipant represents a participant in an appointment
type FHIRParticipant struct {
	Actor  FHIRReference `json:"actor"`  // Reference to Patient, Practitioner, etc.
	Status string        `json:"status"` // accepted, declined, tentative, needs-action
}

// FHIRReference represents a reference to another FHIR resource
type FHIRReference struct {
	Reference string `json:"reference"` // e.g., "Patient/123"
	Display   string `json:"display,omitempty"`
}

// FHIRCodeableConcept represents a coded value with optional text
type FHIRCodeableConcept struct {
	Coding []FHIRCoding `json:"coding,omitempty"`
	Text   string       `json:"text,omitempty"`
}

// FHIRCoding represents a specific code from a code system
type FHIRCoding struct {
	System  string `json:"system,omitempty"`  // Code system URL
	Code    string `json:"code,omitempty"`    // Code value
	Display string `json:"display,omitempty"` // Human-readable display
}

// FHIRHumanName represents a person's name
type FHIRHumanName struct {
	Use    string   `json:"use,omitempty"` // usual, official, temp, nickname, etc.
	Family string   `json:"family,omitempty"`
	Given  []string `json:"given,omitempty"`
	Prefix []string `json:"prefix,omitempty"`
	Suffix []string `json:"suffix,omitempty"`
}

// FHIRContactPoint represents a contact detail (phone, email, etc.)
type FHIRContactPoint struct {
	System string `json:"system,omitempty"` // phone, fax, email, pager, url, sms, other
	Value  string `json:"value,omitempty"`
	Use    string `json:"use,omitempty"` // home, work, temp, old, mobile
}

// FHIRAddress represents a physical address
type FHIRAddress struct {
	Use        string   `json:"use,omitempty"` // home, work, temp, old, billing
	Line       []string `json:"line,omitempty"`
	City       string   `json:"city,omitempty"`
	State      string   `json:"state,omitempty"`
	PostalCode string   `json:"postalCode,omitempty"`
	Country    string   `json:"country,omitempty"`
}

// FHIRMeta contains metadata about the resource
type FHIRMeta struct {
	LastUpdated string `json:"lastUpdated,omitempty"`
	VersionID   string `json:"versionId,omitempty"`
}

// parseSlots converts FHIR Slot bundle to emr.Slot slice
func (c *Client) parseSlots(bundle FHIRBundle) ([]emr.Slot, error) {
	slots := make([]emr.Slot, 0, len(bundle.Entry))

	for _, entry := range bundle.Entry {
		// Type assert to map to decode as FHIRSlot
		data, err := jsonMarshal(entry.Resource)
		if err != nil {
			continue
		}

		var fhirSlot FHIRSlot
		if err := jsonUnmarshal(data, &fhirSlot); err != nil {
			continue
		}

		slot, err := c.parseFHIRSlot(fhirSlot)
		if err != nil {
			continue
		}

		slots = append(slots, *slot)
	}

	return slots, nil
}

// parseFHIRSlot converts a FHIR Slot to emr.Slot
func (c *Client) parseFHIRSlot(fhirSlot FHIRSlot) (*emr.Slot, error) {
	startTime, err := time.Parse(time.RFC3339, fhirSlot.Start)
	if err != nil {
		return nil, err
	}

	endTime, err := time.Parse(time.RFC3339, fhirSlot.End)
	if err != nil {
		return nil, err
	}

	slot := &emr.Slot{
		ID:        fhirSlot.ID,
		StartTime: startTime,
		EndTime:   endTime,
		Status:    fhirSlot.Status,
	}

	// Extract provider ID from schedule reference
	if fhirSlot.Schedule.Reference != "" {
		slot.ProviderID = extractIDFromReference(fhirSlot.Schedule.Reference)
		slot.ProviderName = fhirSlot.Schedule.Display
	}

	// Extract service type
	if len(fhirSlot.ServiceType) > 0 {
		slot.ServiceType = fhirSlot.ServiceType[0].Display
	}

	return slot, nil
}

// parseFHIRAppointment converts a FHIR Appointment to emr.Appointment
func (c *Client) parseFHIRAppointment(fhirAppt FHIRAppointment) *emr.Appointment {
	appt := &emr.Appointment{
		ID:     fhirAppt.ID,
		Status: fhirAppt.Status,
		Notes:  fhirAppt.Description,
	}

	// Parse timestamps
	if startTime, err := time.Parse(time.RFC3339, fhirAppt.Start); err == nil {
		appt.StartTime = startTime
	}
	if endTime, err := time.Parse(time.RFC3339, fhirAppt.End); err == nil {
		appt.EndTime = endTime
	}
	if fhirAppt.Created != "" {
		if createdTime, err := time.Parse(time.RFC3339, fhirAppt.Created); err == nil {
			appt.CreatedAt = createdTime
		}
	}
	if fhirAppt.Meta != nil && fhirAppt.Meta.LastUpdated != "" {
		if updatedTime, err := time.Parse(time.RFC3339, fhirAppt.Meta.LastUpdated); err == nil {
			appt.UpdatedAt = updatedTime
		}
	}

	// Extract participant information
	for _, participant := range fhirAppt.Participant {
		ref := participant.Actor.Reference
		if containsPrefix(ref, "Patient/") {
			appt.PatientID = extractIDFromReference(ref)
		} else if containsPrefix(ref, "Practitioner/") {
			appt.ProviderID = extractIDFromReference(ref)
			appt.ProviderName = participant.Actor.Display
		}
	}

	// Extract service type
	if len(fhirAppt.ServiceType) > 0 && len(fhirAppt.ServiceType[0].Coding) > 0 {
		appt.ServiceType = fhirAppt.ServiceType[0].Coding[0].Display
	}

	return appt
}

// parseFHIRPatient converts a FHIR Patient to emr.Patient
func (c *Client) parseFHIRPatient(fhirPatient FHIRPatient) *emr.Patient {
	patient := &emr.Patient{
		ID:     fhirPatient.ID,
		Gender: fhirPatient.Gender,
	}

	// Extract name
	if len(fhirPatient.Name) > 0 {
		name := fhirPatient.Name[0]
		patient.LastName = name.Family
		if len(name.Given) > 0 {
			patient.FirstName = name.Given[0]
		}
	}

	// Extract birth date
	if fhirPatient.BirthDate != "" {
		if birthDate, err := time.Parse("2006-01-02", fhirPatient.BirthDate); err == nil {
			patient.DateOfBirth = birthDate
		}
	}

	// Extract contact information
	for _, telecom := range fhirPatient.Telecom {
		switch telecom.System {
		case "phone":
			patient.Phone = telecom.Value
		case "email":
			patient.Email = telecom.Value
		}
	}

	// Extract address
	if len(fhirPatient.Address) > 0 {
		addr := fhirPatient.Address[0]
		patient.Address = emr.Address{
			City:       addr.City,
			State:      addr.State,
			PostalCode: addr.PostalCode,
			Country:    addr.Country,
		}
		if len(addr.Line) > 0 {
			patient.Address.Line1 = addr.Line[0]
		}
		if len(addr.Line) > 1 {
			patient.Address.Line2 = addr.Line[1]
		}
	}

	// Extract metadata timestamps
	if fhirPatient.Meta != nil && fhirPatient.Meta.LastUpdated != "" {
		if updatedTime, err := time.Parse(time.RFC3339, fhirPatient.Meta.LastUpdated); err == nil {
			patient.UpdatedAt = updatedTime
			patient.CreatedAt = updatedTime // Nextech may not provide separate created timestamp
		}
	}

	return patient
}

// Helper functions

func extractIDFromReference(reference string) string {
	// Extract ID from reference like "Patient/123" -> "123"
	parts := splitString(reference, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return reference
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func splitString(s, sep string) []string {
	// Simple string split implementation
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// JSON helpers to work around interface{} type assertion
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
