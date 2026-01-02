package onboarding

import (
	"encoding/json"
	"log"
	"net/http"
)

// Handler manages the HTTP endpoints for client onboarding.
type Handler struct {
	// In the future, you will inject your database store and Telnyx client here.
	// store  Store
	// telnyx TelnyxClient
}

// NewHandler creates a new onboarding HTTP handler.
func NewHandler() *Handler {
	return &Handler{}
}

// HandleRegistration accepts a POST request with the RegistrationRequest payload.
// It validates the input and queues the 10DLC registration process.
func (h *Handler) HandleRegistration(w http.ResponseWriter, r *http.Request) {
	// 1. Ensure correct method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. Decode the JSON payload
	var req RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding registration request: %v", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 3. Validate critical fields
	// This prevents "garbage in" before we even touch the database.
	if req.ClinicID == "" {
		http.Error(w, "Field 'clinic_id' is required", http.StatusBadRequest)
		return
	}
	if req.BusinessProfile.LegalBusinessName == "" {
		http.Error(w, "Field 'business_profile.legal_business_name' is required", http.StatusBadRequest)
		return
	}
	if req.Contact.Email == "" {
		http.Error(w, "Field 'contact.email' is required", http.StatusBadRequest)
		return
	}

	// 4. TODO: Persistence & Async Processing
	// - Save 'req' to database with status="PENDING_SUBMISSION"
	// - Enqueue a job for the Telnyx Worker to pick up

	log.Printf("Received registration request for clinic: %s", req.ClinicID)

	// 5. Return 202 Accepted
	// We return 202 because the registration is an asynchronous process (Telnyx takes time).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	resp := map[string]string{
		"status":    "accepted",
		"clinic_id": req.ClinicID,
		"message":   "Registration request received. 10DLC verification process started.",
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}
