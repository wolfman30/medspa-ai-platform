package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// PortalDashboardHandler serves customer-facing dashboard metrics.
type PortalDashboardHandler struct {
	db     *sql.DB
	logger *logging.Logger
}

// PortalDashboardResponse contains the core customer metrics.
type PortalDashboardResponse struct {
	OrgID               string  `json:"org_id"`
	PeriodStart         string  `json:"period_start"`
	PeriodEnd           string  `json:"period_end"`
	Conversations       int64   `json:"conversations"`
	SuccessfulDeposits  int64   `json:"successful_deposits"`
	TotalCollectedCents int64   `json:"total_collected_cents"`
	ConversionPct       float64 `json:"conversion_pct"`
}

// NewPortalDashboardHandler creates a new portal dashboard handler.
func NewPortalDashboardHandler(db *sql.DB, logger *logging.Logger) *PortalDashboardHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &PortalDashboardHandler{
		db:     db,
		logger: logger,
	}
}

// GetDashboard returns the portal dashboard metrics.
// GET /portal/orgs/{orgID}/dashboard
// Query params:
//   - start: RFC3339 timestamp (optional, requires end)
//   - end: RFC3339 timestamp (optional, requires start)
//   - phone: optional patient phone filter
func (h *PortalDashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}
	if h.db == nil {
		jsonError(w, "dashboard disabled", http.StatusServiceUnavailable)
		return
	}

	start, end, periodStart, periodEnd, err := parsePortalWindow(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	phoneDigits := phoneDigitsCandidates(r.URL.Query().Get("phone"))

	conversations, err := h.countConversations(r.Context(), orgID, phoneDigits, start, end)
	if err != nil {
		h.logger.Error("failed to count conversations", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	successfulDeposits, err := h.countSuccessfulDeposits(r.Context(), orgID, phoneDigits, start, end)
	if err != nil {
		h.logger.Error("failed to count deposits", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	totalCollectedCents, err := h.sumSuccessfulDepositAmount(r.Context(), orgID, phoneDigits, start, end)
	if err != nil {
		h.logger.Error("failed to sum deposits", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	conversionPct := 0.0
	if conversations > 0 {
		conversionPct = (float64(successfulDeposits) / float64(conversations)) * 100.0
	}

	resp := PortalDashboardResponse{
		OrgID:               orgID,
		PeriodStart:         periodStart,
		PeriodEnd:           periodEnd,
		Conversations:       conversations,
		SuccessfulDeposits:  successfulDeposits,
		TotalCollectedCents: totalCollectedCents,
		ConversionPct:       conversionPct,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *PortalDashboardHandler) countConversations(ctx context.Context, orgID string, phoneDigits []string, start, end *time.Time) (int64, error) {
	if h.hasConversationsForOrg(ctx, orgID) {
		query := `SELECT COUNT(*) FROM conversations WHERE org_id = $1`
		args := []any{orgID}
		argNum := 2

		if start != nil && end != nil {
			query += fmt.Sprintf(" AND started_at >= $%d AND started_at < $%d", argNum, argNum+1)
			args = append(args, *start, *end)
			argNum += 2
		}

		query += appendPhoneDigitsFilter("regexp_replace(phone, '\\\\D', '', 'g')", phoneDigits, &args, &argNum)

		var count int64
		if err := h.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return 0, err
		}
		return count, nil
	}

	query := `SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1`
	args := []any{"sms:" + orgID + ":%"}
	argNum := 2

	if start != nil && end != nil {
		query += fmt.Sprintf(" AND created_at >= $%d AND created_at < $%d", argNum, argNum+1)
		args = append(args, *start, *end)
		argNum += 2
	}

	query += appendPhoneDigitsFilter("regexp_replace(split_part(conversation_id, ':', 3), '\\\\D', '', 'g')", phoneDigits, &args, &argNum)

	var count int64
	if err := h.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (h *PortalDashboardHandler) countSuccessfulDeposits(ctx context.Context, orgID string, phoneDigits []string, start, end *time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM payments p
		LEFT JOIN leads l ON p.lead_id = l.id
		WHERE p.org_id = $1 AND p.status = 'succeeded'
	`
	args := []any{orgID}
	argNum := 2

	if start != nil && end != nil {
		query += fmt.Sprintf(" AND p.created_at >= $%d AND p.created_at < $%d", argNum, argNum+1)
		args = append(args, *start, *end)
		argNum += 2
	}

	query += appendPhoneDigitsFilter("regexp_replace(l.phone, '\\\\D', '', 'g')", phoneDigits, &args, &argNum)

	var count int64
	if err := h.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (h *PortalDashboardHandler) sumSuccessfulDepositAmount(ctx context.Context, orgID string, phoneDigits []string, start, end *time.Time) (int64, error) {
	query := `
		SELECT COALESCE(SUM(p.amount_cents), 0)
		FROM payments p
		LEFT JOIN leads l ON p.lead_id = l.id
		WHERE p.org_id = $1 AND p.status = 'succeeded'
	`
	args := []any{orgID}
	argNum := 2

	if start != nil && end != nil {
		query += fmt.Sprintf(" AND p.created_at >= $%d AND p.created_at < $%d", argNum, argNum+1)
		args = append(args, *start, *end)
		argNum += 2
	}

	query += appendPhoneDigitsFilter("regexp_replace(l.phone, '\\\\D', '', 'g')", phoneDigits, &args, &argNum)

	var total int64
	if err := h.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (h *PortalDashboardHandler) hasConversationsTable(ctx context.Context) bool {
	var exists bool
	h.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'conversations')`,
	).Scan(&exists)
	return exists
}

func (h *PortalDashboardHandler) hasConversationsForOrg(ctx context.Context, orgID string) bool {
	if !h.hasConversationsTable(ctx) {
		return false
	}
	var count int
	h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM conversations WHERE org_id = $1 LIMIT 1`, orgID,
	).Scan(&count)
	return count > 0
}

func parsePortalWindow(r *http.Request) (*time.Time, *time.Time, string, string, error) {
	q := r.URL.Query()
	startRaw := strings.TrimSpace(q.Get("start"))
	endRaw := strings.TrimSpace(q.Get("end"))

	if (startRaw == "") != (endRaw == "") {
		return nil, nil, "", "", fmt.Errorf("both start and end must be provided, or neither")
	}

	if startRaw == "" {
		return nil, nil, "all-time", "now", nil
	}

	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("invalid start time, use RFC3339 format")
	}
	end, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("invalid end time, use RFC3339 format")
	}
	if !end.After(start) {
		return nil, nil, "", "", fmt.Errorf("end must be after start")
	}
	start = start.UTC()
	end = end.UTC()

	return &start, &end, start.Format(time.RFC3339), end.Format(time.RFC3339), nil
}

// IndexPage serves the portal landing page with navigation links.
// GET /portal/orgs/{orgID}
func (h *PortalDashboardHandler) IndexPage(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(portalIndexHTML))
}

const portalIndexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Clinic Portal</title>
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
      min-height: 100vh;
    }
    .wrap {
      max-width: 800px;
      margin: 0 auto;
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 32px;
      box-shadow: 0 12px 30px rgba(0,0,0,0.08);
    }
    h1 {
      font-size: 28px;
      margin: 0 0 8px;
    }
    .subtitle {
      margin: 0 0 32px;
      color: var(--muted);
    }
    .nav-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
    }
    .nav-card {
      display: block;
      padding: 20px;
      background: #faf8f6;
      border: 1px solid var(--border);
      border-radius: 12px;
      text-decoration: none;
      color: var(--ink);
      transition: all 0.2s ease;
    }
    .nav-card:hover {
      border-color: var(--accent);
      box-shadow: 0 4px 12px rgba(201, 110, 75, 0.15);
      transform: translateY(-2px);
    }
    .nav-card h2 {
      font-size: 18px;
      margin: 0 0 8px;
      color: var(--accent-dark);
    }
    .nav-card p {
      font-size: 14px;
      margin: 0;
      color: var(--muted);
    }
    .org-id {
      margin-top: 32px;
      padding-top: 16px;
      border-top: 1px solid var(--border);
      font-size: 12px;
      color: var(--muted);
    }
    .org-id code {
      background: #f0ebe6;
      padding: 2px 6px;
      border-radius: 4px;
      font-family: "JetBrains Mono", monospace;
    }
  </style>
</head>
<body>
  <div class="wrap">
    <h1>Clinic Portal</h1>
    <p class="subtitle">Manage your AI assistant settings and view performance.</p>

    <nav class="nav-grid">
      <a href="knowledge/page" class="nav-card">
        <h2>Knowledge Base</h2>
        <p>View and edit the information your AI uses to answer questions.</p>
      </a>
      <a href="conversations" class="nav-card" onclick="alert('API endpoint - UI coming soon'); return false;">
        <h2>Conversations</h2>
        <p>Review patient conversations handled by the AI.</p>
      </a>
      <a href="deposits" class="nav-card" onclick="alert('API endpoint - UI coming soon'); return false;">
        <h2>Deposits</h2>
        <p>Track deposits collected through booking links.</p>
      </a>
      <a href="dashboard" class="nav-card" onclick="alert('API endpoint - UI coming soon'); return false;">
        <h2>Dashboard</h2>
        <p>View performance metrics and conversion rates.</p>
      </a>
    </nav>

    <div class="org-id">
      Organization ID: <code id="orgId"></code>
    </div>
  </div>
  <script>
    const orgId = window.location.pathname.split("/").filter(Boolean).pop();
    document.getElementById("orgId").textContent = orgId;
  </script>
</body>
</html>`
