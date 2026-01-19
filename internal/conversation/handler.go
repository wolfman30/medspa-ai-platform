package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Enqueuer defines how conversation requests are dispatched.
type Enqueuer interface {
	EnqueueStart(ctx context.Context, jobID string, req StartRequest, opts ...PublishOption) error
	EnqueueMessage(ctx context.Context, jobID string, req MessageRequest, opts ...PublishOption) error
}

// Handler wires HTTP requests to the conversation queue.
type Handler struct {
	enqueuer  Enqueuer
	jobs      JobRecorder
	knowledge KnowledgeRepository
	rag       RAGIngestor
	service   Service
	sms       *SMSTranscriptStore
	logger    *logging.Logger
}

// NewHandler creates a conversation handler.
func NewHandler(enqueuer Enqueuer, jobs JobRecorder, knowledge KnowledgeRepository, rag RAGIngestor, logger *logging.Logger) *Handler {
	return &Handler{
		enqueuer:  enqueuer,
		jobs:      jobs,
		knowledge: knowledge,
		rag:       rag,
		logger:    logger,
	}
}

// SetService attaches the conversation service for transcript lookups.
func (h *Handler) SetService(s Service) {
	h.service = s
}

// SetSMSTranscriptStore attaches the Redis-backed SMS transcript store for phone-view / E2E.
func (h *Handler) SetSMSTranscriptStore(store *SMSTranscriptStore) {
	h.sms = store
}

// Start handles POST /conversations/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		StartRequest
		ScheduledFor string `json:"scheduled_for,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("failed to decode start request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req := payload.StartRequest
	if strings.TrimSpace(payload.ScheduledFor) != "" {
		when, err := time.Parse(time.RFC3339, payload.ScheduledFor)
		if err != nil {
			http.Error(w, "invalid scheduled_for format", http.StatusBadRequest)
			return
		}
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata["scheduled_for"] = when.UTC().Format(time.RFC3339)
		req.Metadata["scheduledFor"] = when.UTC().Format(time.RFC3339)
	}

	jobID := uuid.NewString()

	if err := h.enqueuer.EnqueueStart(r.Context(), jobID, req); err != nil {
		h.logger.Error("failed to enqueue start conversation", "error", err)
		http.Error(w, "Failed to schedule conversation start", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// Message handles POST /conversations/message.
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		MessageRequest
		ScheduledFor string `json:"scheduled_for,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("failed to decode message request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req := payload.MessageRequest
	if strings.TrimSpace(payload.ScheduledFor) != "" {
		when, err := time.Parse(time.RFC3339, payload.ScheduledFor)
		if err != nil {
			http.Error(w, "invalid scheduled_for format", http.StatusBadRequest)
			return
		}
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata["scheduled_for"] = when.UTC().Format(time.RFC3339)
		req.Metadata["scheduledFor"] = when.UTC().Format(time.RFC3339)
	}

	jobID := uuid.NewString()

	if err := h.enqueuer.EnqueueMessage(r.Context(), jobID, req); err != nil {
		h.logger.Error("failed to enqueue message", "error", err)
		http.Error(w, "Failed to schedule message", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// JobStatus handles GET /conversations/jobs/{jobID}.
func (h *Handler) JobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		http.Error(w, "jobID is required", http.StatusBadRequest)
		return
	}

	job, err := h.jobs.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to load job", "error", err, "job_id", jobID)
		http.Error(w, "Failed to load job", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusOK, job)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Error("failed to write JSON response", "error", err)
	}
}

func (h *Handler) writeAccepted(w http.ResponseWriter, jobID string) {
	h.writeJSON(w, http.StatusAccepted, struct {
		JobID  string `json:"jobId"`
		Status string `json:"status"`
	}{
		JobID:  jobID,
		Status: "accepted",
	})
}

// AddKnowledge handles POST /knowledge/{clinicID}.
func (h *Handler) AddKnowledge(w http.ResponseWriter, r *http.Request) {
	if h.knowledge == nil {
		http.Error(w, "knowledge ingestion not configured", http.StatusServiceUnavailable)
		return
	}
	clinicID := chi.URLParam(r, "clinicID")
	if strings.TrimSpace(clinicID) == "" {
		http.Error(w, "clinicID required", http.StatusBadRequest)
		return
	}
	if orgID, ok := tenancy.OrgIDFromContext(r.Context()); ok && orgID != "" && orgID != clinicID {
		http.Error(w, "clinicID does not match org", http.StatusForbidden)
		return
	}

	var payload struct {
		Documents json.RawMessage `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(payload.Documents) == 0 {
		http.Error(w, "documents required", http.StatusBadRequest)
		return
	}

	var documents []string
	if err := json.Unmarshal(payload.Documents, &documents); err != nil {
		type titledDocument struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		var titled []titledDocument
		if err := json.Unmarshal(payload.Documents, &titled); err != nil {
			http.Error(w, "documents must be an array of strings or {title, content} objects", http.StatusBadRequest)
			return
		}
		documents = make([]string, 0, len(titled))
		for _, doc := range titled {
			title := strings.TrimSpace(doc.Title)
			content := strings.TrimSpace(doc.Content)
			switch {
			case title != "" && content != "":
				documents = append(documents, title+"\n\n"+content)
			case content != "":
				documents = append(documents, content)
			case title != "":
				documents = append(documents, title)
			}
		}
	}
	if len(documents) == 0 {
		http.Error(w, "documents required", http.StatusBadRequest)
		return
	}

	const maxDocs = 20
	if len(documents) > maxDocs {
		http.Error(w, fmt.Sprintf("maximum %d documents per request", maxDocs), http.StatusBadRequest)
		return
	}

	if err := h.knowledge.AppendDocuments(r.Context(), clinicID, documents); err != nil {
		h.logger.Error("failed to append knowledge", "error", err)
		http.Error(w, "failed to persist documents", http.StatusInternalServerError)
		return
	}

	embedded := false
	if h.rag != nil {
		if err := h.rag.AddDocuments(r.Context(), clinicID, documents); err != nil {
			h.logger.Error("failed to embed knowledge", "error", err)
			http.Error(w, "failed to embed documents", http.StatusInternalServerError)
			return
		}
		embedded = true
	}

	h.writeJSON(w, http.StatusCreated, map[string]any{
		"clinicId":  clinicID,
		"documents": len(documents),
		"embedded":  embedded,
		"status":    "stored",
	})
}

// KnowledgeForm serves a responsive HTML form for uploading clinic knowledge.
func (h *Handler) KnowledgeForm(w http.ResponseWriter, r *http.Request) {
	if h.knowledge == nil {
		http.Error(w, "knowledge ingestion not configured", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(knowledgeFormHTML))
}

const knowledgeFormHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>MedSpa Knowledge Intake</title>
  <style>
    :root {
      font-family: "Inter", system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: #0f172a;
      background: #f8fafc;
    }
    body {
      margin: 0;
      min-height: 100vh;
      display: flex;
      justify-content: center;
      padding: 2rem;
      background: linear-gradient(135deg, #f8fafc, #eef2ff);
    }
    .card {
      width: 100%;
      max-width: 720px;
      background: #fff;
      border-radius: 1.25rem;
      box-shadow: 0 25px 50px -12px rgba(15, 23, 42, 0.15);
      padding: 2rem;
      box-sizing: border-box;
    }
    h1 {
      margin-top: 0;
      font-size: 1.75rem;
      color: #0f172a;
    }
    p.description {
      color: #475569;
      line-height: 1.5;
      margin-bottom: 1.5rem;
    }
    label {
      display: block;
      font-weight: 600;
      margin-bottom: 0.35rem;
      color: #0f172a;
    }
    input, textarea {
      width: 100%;
      border: 1px solid #cbd5f5;
      border-radius: 0.75rem;
      padding: 0.85rem 1rem;
      font-size: 1rem;
      font-family: inherit;
      background: #f8fafc;
      transition: border 0.2s ease, box-shadow 0.2s ease;
      box-sizing: border-box;
    }
    input:focus, textarea:focus {
      border-color: #6366f1;
      outline: none;
      box-shadow: 0 0 0 3px rgba(99,102,241,0.15);
      background: #fff;
    }
    textarea {
      min-height: 120px;
      resize: vertical;
    }
    .row {
      display: flex;
      flex-direction: column;
      gap: 1rem;
    }
    @media (min-width: 640px) {
      .row.two-cols {
        flex-direction: row;
      }
      .row.two-cols > div {
        flex: 1;
      }
    }
    button {
      width: 100%;
      border: none;
      border-radius: 9999px;
      padding: 0.95rem;
      font-size: 1rem;
      font-weight: 600;
      color: white;
      background: linear-gradient(135deg, #6366f1, #8b5cf6);
      cursor: pointer;
      transition: transform 0.2s ease, box-shadow 0.2s ease;
      margin-top: 1rem;
    }
    button:hover {
      transform: translateY(-1px);
      box-shadow: 0 15px 30px -10px rgba(99, 102, 241, 0.5);
    }
    .status {
      margin-top: 1rem;
      padding: 0.85rem 1rem;
      border-radius: 0.75rem;
      font-size: 0.95rem;
      display: none;
    }
    .status.success {
      background: #ecfdf5;
      color: #065f46;
    }
    .status.error {
      background: #fef2f2;
      color: #991b1b;
    }
  </style>
</head>
<body>
  <main class="card">
    <h1>MedSpa Knowledge Intake</h1>
    <p class="description">
      Drop in highlights about your services, FAQs, contraindications, pricing, or tone. We'll ground the AI on these insights in seconds.
      Use separate fields for each topic so we can index them precisely.
    </p>
    <form id="knowledge-form">
      <div class="row two-cols">
        <div>
          <label for="clinicId">Clinic ID / Handle *</label>
          <input id="clinicId" name="clinicId" placeholder="e.g. spa-west" required />
        </div>
        <div>
          <label for="contactEmail">Contact Email (optional)</label>
          <input id="contactEmail" name="contactEmail" placeholder="you@example.com" />
        </div>
      </div>

      <label for="services">Services & Pricing</label>
      <textarea id="services" data-doc placeholder="List key services, packages, and typical pricing."></textarea>

      <label for="faqs">Common FAQs</label>
      <textarea id="faqs" data-doc placeholder="Top 3-5 questions clients ask and your preferred answers."></textarea>

      <label for="prep">Prep & Aftercare</label>
      <textarea id="prep" data-doc placeholder="Pre-appointment prep, contraindications, or aftercare instructions."></textarea>

      <label for="voice">Brand Voice / Tone</label>
      <textarea id="voice" data-doc placeholder="How should we greet clients? Any phrasing to use or avoid?"></textarea>

      <label for="custom">Any other notes</label>
      <textarea id="custom" data-doc placeholder="Deposits, promos, escalation rules, etc."></textarea>

      <button type="submit">Save Knowledge</button>
      <div id="status" class="status"></div>
    </form>
  </main>

  <script>
    const form = document.getElementById("knowledge-form");
    const statusBox = document.getElementById("status");

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      statusBox.style.display = "none";
      statusBox.textContent = "";
      statusBox.className = "status";

      const clinicId = document.getElementById("clinicId").value.trim();
      const docs = Array.from(document.querySelectorAll("[data-doc]"))
        .map((el) => el.value.trim())
        .filter(Boolean);

      if (!clinicId || docs.length === 0) {
        showStatus("Please provide a clinic ID and at least one section of content.", false);
        return;
      }

      try {
        const response = await fetch(
          "/knowledge/" + encodeURIComponent(clinicId),
          {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ documents: docs }),
          }
        );

        if (!response.ok) {
          const errorText = await response.text();
          throw new Error(errorText || "Failed to store knowledge.");
        }

        showStatus("Saved! You can add more or close this window.", true);
        form.reset();
      } catch (err) {
        showStatus(err.message || "Something went wrong.", false);
      }
    });

    function showStatus(message, success) {
      statusBox.style.display = "block";
      statusBox.className = "status " + (success ? "success" : "error");
      statusBox.textContent = message;
    }
  </script>
</body>
</html>`

// TranscriptResponse is the response for GET /admin/clinics/{orgID}/conversations/{phone}
type TranscriptResponse struct {
	ConversationID string    `json:"conversation_id"`
	Messages       []Message `json:"messages"`
}

// GetTranscript handles GET /admin/clinics/{orgID}/conversations/{phone}
// Returns the conversation transcript for a given phone number.
func (h *Handler) GetTranscript(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	phoneParam := chi.URLParam(r, "phone")
	phone, err := url.PathUnescape(phoneParam)
	if err != nil {
		http.Error(w, "invalid phone encoding", http.StatusBadRequest)
		return
	}
	phone = strings.TrimSpace(phone)

	if orgID == "" || phone == "" {
		http.Error(w, "missing org_id or phone", http.StatusBadRequest)
		return
	}

	if h.service == nil {
		http.Error(w, "transcript service not configured", http.StatusInternalServerError)
		return
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	digits = normalizeUSDigits(digits)
	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)

	messages, err := h.service.GetHistory(r.Context(), conversationID)
	if err != nil {
		// Check if it's a "not found" error
		if strings.Contains(err.Error(), "unknown conversation") {
			http.Error(w, "conversation not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to get transcript", "error", err, "conversation_id", conversationID)
		http.Error(w, "failed to retrieve transcript", http.StatusInternalServerError)
		return
	}

	resp := TranscriptResponse{
		ConversationID: conversationID,
		Messages:       messages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func sanitizeDigits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeUSDigits converts 10-digit US numbers to E.164 digits by prefixing "1".
func normalizeUSDigits(digits string) string {
	if len(digits) == 10 {
		return "1" + digits
	}
	return digits
}

type SMSTranscriptResponse struct {
	ConversationID string                 `json:"conversation_id"`
	Messages       []SMSTranscriptMessage `json:"messages"`
}

// GetSMSTranscript handles GET /admin/clinics/{orgID}/sms/{phone}
// Returns a Redis-backed SMS transcript that includes webhook acks, AI replies, deposit links, and confirmations.
func (h *Handler) GetSMSTranscript(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	phoneParam := chi.URLParam(r, "phone")
	phone, err := url.PathUnescape(phoneParam)
	if err != nil {
		http.Error(w, "invalid phone encoding", http.StatusBadRequest)
		return
	}
	phone = strings.TrimSpace(phone)

	if orgID == "" || phone == "" {
		http.Error(w, "missing org_id or phone", http.StatusBadRequest)
		return
	}
	if h.sms == nil {
		http.Error(w, "sms transcript store not configured", http.StatusServiceUnavailable)
		return
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	digits = normalizeUSDigits(digits)
	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)

	var limit int64
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	messages, err := h.sms.List(r.Context(), conversationID, limit)
	if err != nil {
		h.logger.Error("failed to load sms transcript", "error", err, "conversation_id", conversationID)
		http.Error(w, "failed to retrieve sms transcript", http.StatusInternalServerError)
		return
	}

	resp := SMSTranscriptResponse{
		ConversationID: conversationID,
		Messages:       messages,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
