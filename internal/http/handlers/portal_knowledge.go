package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// PortalKnowledgeHandler handles portal knowledge CRUD.
type PortalKnowledgeHandler struct {
	repo   conversation.KnowledgeRepository
	audit  *compliance.AuditService
	logger *logging.Logger
}

// NewPortalKnowledgeHandler creates a new portal handler.
func NewPortalKnowledgeHandler(repo conversation.KnowledgeRepository, audit *compliance.AuditService, logger *logging.Logger) *PortalKnowledgeHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &PortalKnowledgeHandler{
		repo:   repo,
		audit:  audit,
		logger: logger,
	}
}

// GetKnowledge returns clinic knowledge.
// GET /portal/orgs/{orgID}/knowledge
func (h *PortalKnowledgeHandler) GetKnowledge(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}
	if h.repo == nil {
		jsonError(w, "knowledge disabled", http.StatusServiceUnavailable)
		return
	}

	docs, err := h.repo.GetDocuments(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to fetch knowledge", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logAudit(r, orgID, compliance.EventKnowledgeRead, len(docs))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"org_id":    orgID,
		"documents": docs,
	})
}

// PutKnowledge replaces clinic knowledge.
// PUT /portal/orgs/{orgID}/knowledge
func (h *PortalKnowledgeHandler) PutKnowledge(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}
	if h.repo == nil {
		jsonError(w, "knowledge disabled", http.StatusServiceUnavailable)
		return
	}

	replacer, ok := h.repo.(conversation.KnowledgeReplacer)
	if !ok {
		jsonError(w, "knowledge editing not configured", http.StatusServiceUnavailable)
		return
	}

	var payload struct {
		Documents json.RawMessage `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	documents, err := conversation.ParseKnowledgePayload(payload.Documents)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := conversation.ValidateKnowledgeDocuments(documents); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := replacer.ReplaceDocuments(r.Context(), orgID, documents); err != nil {
		h.logger.Error("failed to replace knowledge", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if versioner, ok := h.repo.(conversation.KnowledgeVersioner); ok {
		version, err := versioner.GetVersion(r.Context(), orgID)
		if err != nil {
			h.logger.Error("failed to read knowledge version", "org_id", orgID, "error", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := versioner.SetVersion(r.Context(), orgID, version+1); err != nil {
			h.logger.Error("failed to bump knowledge version", "org_id", orgID, "error", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	h.logAudit(r, orgID, compliance.EventKnowledgeUpdated, len(documents))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"org_id":    orgID,
		"documents": len(documents),
		"status":    "stored",
	})
}

// KnowledgePage serves the portal knowledge editor UI.
func (h *PortalKnowledgeHandler) KnowledgePage(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(knowledgePageHTML))
}

func (h *PortalKnowledgeHandler) logAudit(r *http.Request, orgID string, event compliance.AuditEventType, docCount int) {
	if h.audit == nil {
		return
	}
	actorType, actorEmail := auditActor(r)
	detailsJSON, _ := json.Marshal(map[string]any{
		"actor_type":  actorType,
		"actor_email": actorEmail,
		"documents":   docCount,
		"method":      r.Method,
		"path":        r.URL.Path,
	})
	_ = h.audit.LogEvent(r.Context(), compliance.AuditEvent{
		EventType: event,
		OrgID:     orgID,
		Details:   detailsJSON,
	})
}

func auditActor(r *http.Request) (string, string) {
	if claims, ok := httpmiddleware.CognitoClaimsFromContext(r.Context()); ok && claims != nil {
		return "cognito", claims.Email
	}
	if claims, ok := httpmiddleware.AdminClaimsFromContext(r.Context()); ok {
		return "admin_jwt", claims.Subject
	}
	return "unknown", ""
}

const knowledgePageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Clinic Knowledge</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f2ee;
      --panel: #ffffff;
      --ink: #2b2a28;
      --muted: #6a5f55;
      --accent: #c96e4b;
      --accent-dark: #a85a3c;
      --border: #e6dcd5;
    }
    body {
      font-family: "Source Serif 4", "Georgia", serif;
      margin: 0;
      padding: 32px;
      background: radial-gradient(circle at top, #fff3ea, var(--bg));
      color: var(--ink);
    }
    .wrap {
      max-width: 960px;
      margin: 0 auto;
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 24px;
      box-shadow: 0 12px 30px rgba(0,0,0,0.08);
    }
    h1 {
      font-size: 28px;
      margin: 0 0 8px;
    }
    p {
      margin: 0 0 16px;
      color: var(--muted);
    }
    textarea {
      width: 100%;
      min-height: 320px;
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 12px;
      font-family: "JetBrains Mono", monospace;
      font-size: 13px;
      background: #faf8f6;
      color: var(--ink);
      resize: vertical;
    }
    .actions {
      display: flex;
      gap: 12px;
      margin-top: 16px;
    }
    button {
      border: none;
      border-radius: 999px;
      padding: 10px 18px;
      font-weight: 600;
      cursor: pointer;
      background: var(--accent);
      color: white;
    }
    button:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .secondary {
      background: transparent;
      color: var(--accent-dark);
      border: 1px solid var(--accent-dark);
    }
    .status {
      margin-top: 12px;
      font-size: 13px;
      color: var(--muted);
    }
    .warning {
      margin-top: 16px;
      padding: 12px;
      background: #fff1e8;
      border: 1px solid #f0c8b3;
      border-radius: 12px;
      font-size: 13px;
    }
  </style>
</head>
<body>
  <div class="wrap">
    <h1>Clinic Knowledge</h1>
    <p>Review and update the knowledge the AI uses for this clinic.</p>
    <textarea id="knowledge" disabled></textarea>
    <div class="actions">
      <button id="edit" class="secondary">Edit</button>
      <button id="save" disabled>Save</button>
    </div>
    <div class="status" id="status"></div>
    <div class="warning">
      Note: Do not include any patient-specific information (PHI).
    </div>
  </div>
  <script>
    const textarea = document.getElementById("knowledge");
    const editBtn = document.getElementById("edit");
    const saveBtn = document.getElementById("save");
    const status = document.getElementById("status");
    const orgId = window.location.pathname.split("/").slice(-2)[0];
    async function loadKnowledge() {
      status.textContent = "Loading...";
      const resp = await fetch("/portal/orgs/" + orgId + "/knowledge");
      if (!resp.ok) {
        status.textContent = "Failed to load knowledge.";
        return;
      }
      const data = await resp.json();
      textarea.value = JSON.stringify(data.documents || [], null, 2);
      status.textContent = "Loaded.";
    }
    editBtn.addEventListener("click", () => {
      textarea.disabled = false;
      saveBtn.disabled = false;
      status.textContent = "Editing enabled.";
    });
    saveBtn.addEventListener("click", async () => {
      status.textContent = "Saving...";
      let parsed;
      try {
        parsed = JSON.parse(textarea.value);
      } catch (err) {
        status.textContent = "Invalid JSON. Fix and try again.";
        return;
      }
      const resp = await fetch("/portal/orgs/" + orgId + "/knowledge", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ documents: parsed })
      });
      if (!resp.ok) {
        const errText = await resp.text();
        status.textContent = errText || "Failed to save.";
        return;
      }
      textarea.disabled = true;
      saveBtn.disabled = true;
      status.textContent = "Saved.";
    });
    loadKnowledge();
  </script>
</body>
</html>`
