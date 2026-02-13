package payments

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// PaymentURLLookup resolves a payment ID to its provider checkout URL.
type PaymentURLLookup interface {
	GetPaymentByID(ctx context.Context, id pgtype.UUID) (interface{ GetProviderLink() string }, error)
}

// RedirectHandler serves short payment URLs that redirect to the provider checkout page.
type RedirectHandler struct {
	repo   *Repository
	logger *logging.Logger
}

// NewRedirectHandler creates a handler for /pay/{id} short URLs.
func NewRedirectHandler(repo *Repository, logger *logging.Logger) *RedirectHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &RedirectHandler{repo: repo, logger: logger}
}

// Handle looks up a payment by short code and redirects to the provider checkout URL.
func (h *RedirectHandler) Handle(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	url, err := h.repo.GetCheckoutURLByShortCode(r.Context(), code)
	if err != nil || url == "" {
		h.logger.Warn("payment redirect: not found", "code", code, "error", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}
