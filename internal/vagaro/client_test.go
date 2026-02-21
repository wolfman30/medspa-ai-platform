package vagaro

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *VagaroClient {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return NewVagaroClient(ts.URL, logging.Default())
}

func TestVagaroClient_GetServices_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/businesses/biz-1/services" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"services":[{"id":"svc-1","name":"Botox","active":true}]}`))
	})

	services, err := client.GetServices(context.Background(), "biz-1")
	if err != nil {
		t.Fatalf("GetServices() error = %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(services))
	}
	if services[0].ID != "svc-1" {
		t.Fatalf("service ID = %s, want svc-1", services[0].ID)
	}
}

func TestVagaroClient_GetAvailableSlots_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("serviceId") != "svc-1" {
			t.Fatalf("serviceId = %s", r.URL.Query().Get("serviceId"))
		}
		if r.URL.Query().Get("providerId") != "prov-1" {
			t.Fatalf("providerId = %s", r.URL.Query().Get("providerId"))
		}
		if r.URL.Query().Get("date") != "2026-02-21" {
			t.Fatalf("date = %s", r.URL.Query().Get("date"))
		}
		_, _ = w.Write([]byte(`{"slots":[{"start":"2026-02-21T10:00:00Z","end":"2026-02-21T10:30:00Z","providerId":"prov-1","available":true}]}`))
	})

	dt := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	slots, err := client.GetAvailableSlots(context.Background(), "biz-1", "svc-1", "prov-1", dt)
	if err != nil {
		t.Fatalf("GetAvailableSlots() error = %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("len(slots) = %d, want 1", len(slots))
	}
	if !slots[0].Available {
		t.Fatal("slot should be available")
	}
}

func TestVagaroClient_GetProviders_HTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	})

	_, err := client.GetProviders(context.Background(), "biz-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVagaroClient_GetServices_InvalidJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"services":[`))
	})

	_, err := client.GetServices(context.Background(), "biz-1")
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestVagaroClient_CreateAppointment_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/appointments" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"appointmentId":"appt-1","confirmationCode":"CNF123"}`))
	})

	resp, err := client.CreateAppointment(context.Background(), AppointmentRequest{
		BusinessID:   "biz-1",
		ServiceID:    "svc-1",
		ProviderID:   "prov-1",
		Start:        time.Now().UTC(),
		End:          time.Now().UTC().Add(30 * time.Minute),
		PatientName:  "Test Patient",
		PatientPhone: "+15555555555",
	})
	if err != nil {
		t.Fatalf("CreateAppointment() error = %v", err)
	}
	if resp == nil || !resp.OK {
		t.Fatalf("response = %+v, want OK=true", resp)
	}
	if resp.ConfirmationCode != "CNF123" {
		t.Fatalf("confirmation = %s, want CNF123", resp.ConfirmationCode)
	}
}

func TestVagaroClient_ContextCancelled(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"services":[]}`))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.GetServices(ctx, "biz-1")
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if got := fmt.Sprintf("%v", err); got == "" {
		t.Fatal("expected non-empty error message")
	}
}
