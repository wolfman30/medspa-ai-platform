package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

// Start handles POST /conversations/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode start request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	jobID := uuid.NewString()

	if err := h.recordPendingJob(r.Context(), jobID, jobTypeStart, &req, nil); err != nil {
		h.logger.Error("failed to persist job record", "error", err)
		http.Error(w, "Failed to persist job record", http.StatusInternalServerError)
		return
	}

	if err := h.enqueuer.EnqueueStart(r.Context(), jobID, req); err != nil {
		h.logger.Error("failed to enqueue start conversation", "error", err)
		http.Error(w, "Failed to schedule conversation start", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// Message handles POST /conversations/message.
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode message request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	jobID := uuid.NewString()

	if err := h.recordPendingJob(r.Context(), jobID, jobTypeMessage, nil, &req); err != nil {
		h.logger.Error("failed to persist job record", "error", err)
		http.Error(w, "Failed to persist job record", http.StatusInternalServerError)
		return
	}

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

func (h *Handler) recordPendingJob(ctx context.Context, jobID string, kind jobType, start *StartRequest, message *MessageRequest) error {
	if jobID == "" {
		return errors.New("missing job ID")
	}

	job := &JobRecord{
		JobID:          jobID,
		RequestType:    kind,
		StartRequest:   start,
		MessageRequest: message,
	}
	if message != nil {
		job.ConversationID = message.ConversationID
	}
	return h.jobs.PutPending(ctx, job)
}

// AddKnowledge handles POST /knowledge/{clinicID}.
func (h *Handler) AddKnowledge(w http.ResponseWriter, r *http.Request) {
	if h.knowledge == nil || h.rag == nil {
		http.Error(w, "knowledge ingestion not configured", http.StatusServiceUnavailable)
		return
	}
	clinicID := chi.URLParam(r, "clinicID")
	if strings.TrimSpace(clinicID) == "" {
		http.Error(w, "clinicID required", http.StatusBadRequest)
		return
	}

	var payload struct {
		Documents []string `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(payload.Documents) == 0 {
		http.Error(w, "documents required", http.StatusBadRequest)
		return
	}

	const maxDocs = 20
	if len(payload.Documents) > maxDocs {
		http.Error(w, fmt.Sprintf("maximum %d documents per request", maxDocs), http.StatusBadRequest)
		return
	}

	if err := h.knowledge.AppendDocuments(r.Context(), clinicID, payload.Documents); err != nil {
		h.logger.Error("failed to append knowledge", "error", err)
		http.Error(w, "failed to persist documents", http.StatusInternalServerError)
		return
	}

	if err := h.rag.AddDocuments(r.Context(), clinicID, payload.Documents); err != nil {
		h.logger.Error("failed to embed knowledge", "error", err)
		http.Error(w, "failed to embed documents", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusCreated, map[string]any{
		"clinicId":  clinicID,
		"documents": len(payload.Documents),
		"status":    "stored",
	})
}

// KnowledgeForm serves a responsive HTML form for uploading clinic knowledge.
func (h *Handler) KnowledgeForm(w http.ResponseWriter, r *http.Request) {
	if h.knowledge == nil || h.rag == nil {
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
