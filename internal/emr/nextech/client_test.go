package nextech

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				BaseURL:      "https://api.nextech.com",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "missing base URL",
			cfg: Config{
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: true,
		},
		{
			name: "missing client ID",
			cfg: Config{
				BaseURL:      "https://api.nextech.com",
				ClientSecret: "test-secret",
			},
			wantErr: true,
		},
		{
			name: "missing client secret",
			cfg: Config{
				BaseURL:  "https://api.nextech.com",
				ClientID: "test-client",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if client == nil {
				t.Error("expected client but got nil")
			}
		})
	}
}

func TestSearchPatients(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock OAuth token endpoint
		if r.URL.Path == "/connect/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
			return
		}

		// Mock Patient search endpoint
		if r.URL.Path == "/Patient" {
			bundle := FHIRBundle{
				ResourceType: "Bundle",
				Type:         "searchset",
				Total:        1,
				Entry: []struct {
					Resource interface{} `json:"resource"`
				}{
					{
						Resource: map[string]interface{}{
							"resourceType": "Patient",
							"id":           "123",
							"name": []map[string]interface{}{
								{
									"family": "Smith",
									"given":  []string{"John"},
								},
							},
							"telecom": []map[string]interface{}{
								{
									"system": "phone",
									"value":  "+15551234567",
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/fhir+json")
			json.NewEncoder(w).Encode(bundle)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:      server.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	patients, err := client.SearchPatients(ctx, emr.PatientSearchQuery{
		Phone: "+15551234567",
	})

	if err != nil {
		t.Errorf("SearchPatients failed: %v", err)
	}

	if len(patients) == 0 {
		t.Error("expected at least one patient")
	}
}

func TestCreateAppointment(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock OAuth token endpoint
		if r.URL.Path == "/connect/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
			return
		}

		// Mock Appointment creation endpoint
		if r.URL.Path == "/Appointment" && r.Method == http.MethodPost {
			appointment := FHIRAppointment{
				ResourceType: "Appointment",
				ID:           "appt-123",
				Status:       "booked",
				Start:        "2025-01-15T14:00:00Z",
				End:          "2025-01-15T15:00:00Z",
				Description:  "Initial consultation",
			}
			w.Header().Set("Content-Type", "application/fhir+json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(appointment)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:      server.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req := emr.AppointmentRequest{
		PatientID:   "patient-123",
		ProviderID:  "provider-456",
		SlotID:      "slot-789",
		StartTime:   time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
		EndTime:     time.Date(2025, 1, 15, 15, 0, 0, 0, time.UTC),
		ServiceType: "consultation",
		Notes:       "Initial consultation",
		Status:      "booked",
	}

	appointment, err := client.CreateAppointment(ctx, req)
	if err != nil {
		t.Errorf("CreateAppointment failed: %v", err)
	}

	if appointment == nil {
		t.Fatal("expected appointment but got nil")
	}

	if appointment.ID != "appt-123" {
		t.Errorf("expected appointment ID 'appt-123', got '%s'", appointment.ID)
	}

	if appointment.Status != "booked" {
		t.Errorf("expected status 'booked', got '%s'", appointment.Status)
	}
}

func TestParseFHIRSlot(t *testing.T) {
	client := &Client{}

	fhirSlot := FHIRSlot{
		ResourceType: "Slot",
		ID:           "slot-123",
		Status:       "free",
		Start:        "2025-01-15T14:00:00Z",
		End:          "2025-01-15T14:30:00Z",
		Schedule: FHIRReference{
			Reference: "Schedule/provider-456",
			Display:   "Dr. Jane Smith",
		},
		ServiceType: []FHIRCoding{
			{
				Display: "Consultation",
			},
		},
	}

	slot, err := client.parseFHIRSlot(fhirSlot)
	if err != nil {
		t.Fatalf("parseFHIRSlot failed: %v", err)
	}

	if slot.ID != "slot-123" {
		t.Errorf("expected ID 'slot-123', got '%s'", slot.ID)
	}

	if slot.Status != "free" {
		t.Errorf("expected status 'free', got '%s'", slot.Status)
	}

	if slot.ProviderID != "provider-456" {
		t.Errorf("expected provider ID 'provider-456', got '%s'", slot.ProviderID)
	}

	if slot.ProviderName != "Dr. Jane Smith" {
		t.Errorf("expected provider name 'Dr. Jane Smith', got '%s'", slot.ProviderName)
	}

	if slot.ServiceType != "Consultation" {
		t.Errorf("expected service type 'Consultation', got '%s'", slot.ServiceType)
	}
}

func TestExtractIDFromReference(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		want      string
	}{
		{
			name:      "patient reference",
			reference: "Patient/123",
			want:      "123",
		},
		{
			name:      "practitioner reference",
			reference: "Practitioner/456",
			want:      "456",
		},
		{
			name:      "appointment reference",
			reference: "Appointment/789",
			want:      "789",
		},
		{
			name:      "no slash",
			reference: "123",
			want:      "123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIDFromReference(tt.reference)
			if got != tt.want {
				t.Errorf("extractIDFromReference() = %v, want %v", got, tt.want)
			}
		})
	}
}
