package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// StructuredKnowledgeHandler handles structured knowledge CRUD and Moxie sync.
type StructuredKnowledgeHandler struct {
	skStore       *conversation.StructuredKnowledgeStore
	clinicStore   *clinic.Store
	knowledgeRepo conversation.KnowledgeRepository
	logger        *logging.Logger
}

// NewStructuredKnowledgeHandler creates a new handler.
func NewStructuredKnowledgeHandler(
	skStore *conversation.StructuredKnowledgeStore,
	clinicStore *clinic.Store,
	knowledgeRepo conversation.KnowledgeRepository,
	logger *logging.Logger,
) *StructuredKnowledgeHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &StructuredKnowledgeHandler{
		skStore:       skStore,
		clinicStore:   clinicStore,
		knowledgeRepo: knowledgeRepo,
		logger:        logger,
	}
}

// GetStructuredKnowledge returns the structured knowledge for a clinic.
func (h *StructuredKnowledgeHandler) GetStructuredKnowledge(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	sk, err := h.skStore.GetStructured(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get structured knowledge", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sk == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no structured knowledge found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sk)
}

// PutStructuredKnowledge replaces structured knowledge and auto-derives config.
func (h *StructuredKnowledgeHandler) PutStructuredKnowledge(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	var sk conversation.StructuredKnowledge
	if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate
	if len(sk.Sections.Services.Items) == 0 {
		jsonError(w, "services section must have at least 1 item", http.StatusBadRequest)
		return
	}
	if sk.Sections.Policies.Cancellation == "" {
		jsonError(w, "cancellation policy is required", http.StatusBadRequest)
		return
	}
	if sk.Sections.Policies.Deposit == "" {
		jsonError(w, "deposit policy is required", http.StatusBadRequest)
		return
	}

	sk.OrgID = orgID
	sk.UpdatedAt = time.Now().UTC()

	// Get existing version and bump
	existing, err := h.skStore.GetStructured(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get existing structured knowledge", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		sk.Version = existing.Version + 1
	} else {
		sk.Version = 1
	}

	// Derive config
	cfg, err := h.clinicStore.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	conversation.DeriveConfigFromKnowledge(&sk, cfg)

	// Flatten for RAG
	ragDocs := conversation.FlattenKnowledgeForRAG(&sk)

	// Save everything
	if err := h.skStore.SetStructured(r.Context(), orgID, &sk); err != nil {
		h.logger.Error("failed to save structured knowledge", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.clinicStore.Set(r.Context(), cfg); err != nil {
		h.logger.Error("failed to save clinic config", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if replacer, ok := h.knowledgeRepo.(conversation.KnowledgeReplacer); ok {
		if err := replacer.ReplaceDocuments(r.Context(), orgID, ragDocs); err != nil {
			h.logger.Error("failed to replace RAG docs", "org_id", orgID, "error", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	// Bump knowledge version
	if versioner, ok := h.knowledgeRepo.(conversation.KnowledgeVersioner); ok {
		ver, _ := versioner.GetVersion(r.Context(), orgID)
		_ = versioner.SetVersion(r.Context(), orgID, ver+1)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "saved",
		"version": sk.Version,
		"org_id":  orgID,
	})
}

// SyncMoxie fetches services and providers from the Moxie booking page.
func (h *StructuredKnowledgeHandler) SyncMoxie(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	cfg, err := h.clinicStore.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "org_id", orgID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if cfg.MoxieConfig == nil || cfg.MoxieConfig.MedspaSlug == "" {
		jsonError(w, "moxie_config.medspa_slug not configured", http.StatusBadRequest)
		return
	}

	slug := cfg.MoxieConfig.MedspaSlug

	// Extract buildId from the booking page HTML
	buildID, err := extractMoxieBuildID(slug)
	if err != nil {
		h.logger.Error("failed to extract Moxie buildId", "slug", slug, "error", err)
		jsonError(w, "failed to fetch Moxie booking page: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Fetch the Next.js data JSON
	dataURL := fmt.Sprintf("https://app.joinmoxie.com/_next/data/%s/booking/%s.json", buildID, slug)
	resp, err := http.Get(dataURL)
	if err != nil {
		h.logger.Error("failed to fetch Moxie data", "url", dataURL, "error", err)
		jsonError(w, "failed to fetch Moxie data", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.logger.Error("Moxie data returned non-200", "status", resp.StatusCode, "url", dataURL)
		jsonError(w, fmt.Sprintf("Moxie returned status %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, "failed to read Moxie response", http.StatusBadGateway)
		return
	}

	sk, err := parseMoxieBookingJSON(body, orgID)
	if err != nil {
		h.logger.Error("failed to parse Moxie JSON", "error", err)
		jsonError(w, "failed to parse Moxie data: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Auto-derive clinic config (service_price_text, menu items, etc.) from synced knowledge.
	conversation.DeriveConfigFromKnowledge(sk, cfg)
	if err := h.clinicStore.Set(r.Context(), cfg); err != nil {
		h.logger.Error("failed to auto-save derived config after Moxie sync", "org_id", orgID, "error", err)
		// Non-fatal — still return the knowledge
	} else {
		h.logger.Info("auto-derived clinic config from Moxie sync", "org_id", orgID,
			"services", len(sk.Sections.Services.Items),
			"price_entries", len(cfg.ServicePriceText))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sk)
}

var (
	buildIDURLRegex  = regexp.MustCompile(`/_next/data/([^/]+)/`)
	buildIDJSONRegex = regexp.MustCompile(`"buildId"\s*:\s*"([^"]+)"`)
)

func extractMoxieBuildID(slug string) (string, error) {
	pageURL := fmt.Sprintf("https://app.joinmoxie.com/booking/%s", slug)
	resp, err := http.Get(pageURL)
	if err != nil {
		return "", fmt.Errorf("fetch booking page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read booking page: %w", err)
	}

	// Try URL pattern first: /_next/data/{buildId}/
	if matches := buildIDURLRegex.FindSubmatch(body); len(matches) >= 2 {
		return string(matches[1]), nil
	}
	// Fallback: __NEXT_DATA__ JSON blob contains "buildId":"xxx"
	if matches := buildIDJSONRegex.FindSubmatch(body); len(matches) >= 2 {
		return string(matches[1]), nil
	}
	return "", fmt.Errorf("buildId not found in page HTML")
}

// parseMoxieBookingJSON parses the Moxie Next.js data JSON into StructuredKnowledge.
func parseMoxieBookingJSON(data []byte, orgID string) (*conversation.StructuredKnowledge, error) {
	var raw struct {
		PageProps struct {
			MedspaInfo struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				UserMedspas []struct {
					ID   string `json:"id"`
					User struct {
						ID        string `json:"id"`
						FirstName string `json:"firstName"`
						LastName  string `json:"lastName"`
					} `json:"user"`
				} `json:"userMedspas"`
				ServiceCategories []struct {
					Name                   string `json:"name"`
					MedspaServiceMenuItems []struct {
						ID                              string `json:"id"`
						Name                            string `json:"name"`
						Price                           string `json:"price"`
						Description                     string `json:"description"`
						DurationInMinutes               int    `json:"durationInMinutes"`
						IsVariablePricing               bool   `json:"isVariablePricing"`
						IsAddon                         bool   `json:"isAddon"`
						ServiceMenuAdditionalPublicInfo struct {
							EligibleProvidersDetails []struct {
								UserMedspa struct {
									ID   string `json:"id"`
									User struct {
										ID        string `json:"id"`
										FirstName string `json:"firstName"`
										LastName  string `json:"lastName"`
									} `json:"user"`
								} `json:"userMedspa"`
								CustomDuration int `json:"customDuration"`
							} `json:"eligibleProvidersDetails"`
						} `json:"serviceMenuAdditionalPublicInfo"`
					} `json:"medspaServiceMenuItems"`
				} `json:"serviceCategories"`
			} `json:"medspaInfo"`
		} `json:"pageProps"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal moxie JSON: %w", err)
	}

	sk := &conversation.StructuredKnowledge{
		OrgID:     orgID,
		UpdatedAt: time.Now().UTC(),
	}

	// Initialize slices to avoid null in JSON
	sk.Sections.Services.Items = []conversation.ServiceItem{}
	sk.Sections.Providers.Items = []conversation.ProviderItem{}
	sk.Sections.Policies.BookingPolicies = []string{}

	// Build provider map from userMedspas
	providerMap := map[string]string{} // userMedspaID -> "FirstName LastName"
	for _, um := range raw.PageProps.MedspaInfo.UserMedspas {
		name := strings.TrimSpace(um.User.FirstName + " " + um.User.LastName)
		providerMap[um.ID] = name
	}

	order := 1
	for _, cat := range raw.PageProps.MedspaInfo.ServiceCategories {
		if len(cat.MedspaServiceMenuItems) == 0 {
			continue
		}
		for _, item := range cat.MedspaServiceMenuItems {
			var providerIDs []string
			for _, ep := range item.ServiceMenuAdditionalPublicInfo.EligibleProvidersDetails {
				providerIDs = append(providerIDs, ep.UserMedspa.ID)
			}
			if providerIDs == nil {
				providerIDs = []string{}
			}

			priceType := "fixed"
			if item.IsVariablePricing {
				priceType = "variable"
			} else if item.Price == "0.00" {
				priceType = "free"
			}

			priceDisplay := "$" + item.Price
			if priceType == "variable" {
				priceDisplay = "Priced per treatment — your provider will give you an exact quote at your appointment"
			} else if priceType == "free" {
				priceDisplay = "Free"
			}

			duration := item.DurationInMinutes
			// Use custom duration from first provider if available
			if len(item.ServiceMenuAdditionalPublicInfo.EligibleProvidersDetails) > 0 {
				cd := item.ServiceMenuAdditionalPublicInfo.EligibleProvidersDetails[0].CustomDuration
				if cd > 0 {
					duration = cd
				}
			}

			si := conversation.ServiceItem{
				ID:              item.ID,
				Name:            strings.TrimSpace(item.Name),
				Category:        cat.Name,
				Price:           priceDisplay,
				PriceType:       priceType,
				DurationMinutes: duration,
				Description:     strings.TrimSpace(item.Description),
				ProviderIDs:     providerIDs,
				BookingID:       item.ID,
				Aliases:         []string{},
				IsAddon:         item.IsAddon,
				Order:           order,
			}
			sk.Sections.Services.Items = append(sk.Sections.Services.Items, si)
			order++
		}
	}

	// Build providers from the userMedspas list
	for i, um := range raw.PageProps.MedspaInfo.UserMedspas {
		name := strings.TrimSpace(um.User.FirstName + " " + um.User.LastName)
		pi := conversation.ProviderItem{
			ID:          um.ID,
			Name:        name,
			Order:       i + 1,
			Specialties: []string{},
		}
		sk.Sections.Providers.Items = append(sk.Sections.Providers.Items, pi)
	}

	return sk, nil
}
