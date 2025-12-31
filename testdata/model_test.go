package onboarding

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRegistrationValid(t *testing.T) {
	handler := NewHandler()
	payload := RegistrationRequest{
		ClinicID: "clinic-123",
		BusinessProfile: BusinessProfile{
			LegalBusinessName: "Glow Clinic LLC",
		},
		Contact: Contact{
			Email: "owner@glow.test",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/onboarding/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleRegistration(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Fatalf("expected status accepted, got %q", resp["status"])
	}
	if resp["clinic_id"] != payload.ClinicID {
		t.Fatalf("expected clinic_id %s, got %q", payload.ClinicID, resp["clinic_id"])
	}
}

func TestHandleRegistrationMissingFields(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name    string
		payload RegistrationRequest
	}{
		{
			name: "missing clinic_id",
			payload: RegistrationRequest{
				BusinessProfile: BusinessProfile{LegalBusinessName: "Glow Clinic LLC"},
				Contact:         Contact{Email: "owner@glow.test"},
			},
		},
		{
			name: "missing legal_business_name",
			payload: RegistrationRequest{
				ClinicID: "clinic-123",
				Contact:  Contact{Email: "owner@glow.test"},
			},
		},
		{
			name: "missing contact email",
			payload: RegistrationRequest{
				ClinicID:        "clinic-123",
				BusinessProfile: BusinessProfile{LegalBusinessName: "Glow Clinic LLC"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/onboarding/register", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			handler.HandleRegistration(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
			}
		})
	}
}
