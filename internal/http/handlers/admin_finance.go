package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ---------------------------------------------------------------------------
// S3 abstraction (enables testing with a mock)
// ---------------------------------------------------------------------------

// S3Client is the subset of the S3 API we need.
type S3Client interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// ---------------------------------------------------------------------------
// In-memory TTL cache for Plaid responses
// ---------------------------------------------------------------------------

type cacheEntry struct {
	data      any
	expiresAt time.Time
}

type plaidCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

func newPlaidCache(ttl time.Duration) *plaidCache {
	return &plaidCache{entries: make(map[string]cacheEntry), ttl: ttl}
}

func (c *plaidCache) get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.data, true
}

func (c *plaidCache) set(key string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{data: data, expiresAt: time.Now().Add(c.ttl)}
}

// ---------------------------------------------------------------------------
// Config & handler
// ---------------------------------------------------------------------------

type PlaidConfig struct {
	BaseURL     string
	ClientID    string
	Secret      string
	AccessToken string
}

type AdminFinanceHandler struct {
	logger      *logging.Logger
	httpClient  *http.Client
	plaidURL    string
	clientID    string
	secret      string
	accessToken string
	s3Client    S3Client
	s3Bucket    string
	s3Key       string
	cache       *plaidCache
}

type BudgetCategory struct {
	Label     string  `json:"label"`
	Allocated float64 `json:"allocated"`
}

type BudgetFile struct {
	Month      string                    `json:"month"`
	Categories map[string]BudgetCategory `json:"categories"`
}

type BudgetCategorySummary struct {
	Label     string  `json:"label"`
	Allocated float64 `json:"allocated"`
	Spent     float64 `json:"spent"`
	Remaining float64 `json:"remaining"`
}

func NewAdminFinanceHandler(logger *logging.Logger, s3Client S3Client, s3Bucket string, plaid PlaidConfig) *AdminFinanceHandler {
	if logger == nil {
		logger = logging.Default()
	}
	if plaid.BaseURL == "" {
		plaid.BaseURL = "https://production.plaid.com"
	}
	if s3Bucket == "" {
		s3Bucket = "aiwolf-training-data-development"
	}

	return &AdminFinanceHandler{
		logger:      logger,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		plaidURL:    plaid.BaseURL,
		clientID:    plaid.ClientID,
		secret:      plaid.Secret,
		accessToken: plaid.AccessToken,
		s3Client:    s3Client,
		s3Bucket:    s3Bucket,
		s3Key:       "finance/budget.json",
		cache:       newPlaidCache(5 * time.Minute),
	}
}

func (h *AdminFinanceHandler) GetBalances(w http.ResponseWriter, r *http.Request) {
	if h.accessToken == "" {
		h.writeError(w, http.StatusServiceUnavailable, "PLAID_ACCESS_TOKEN not configured")
		return
	}

	// Check cache
	if cached, ok := h.cache.get("balances"); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	var plaidResp struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
			Name      string `json:"name"`
			Subtype   string `json:"subtype"`
			Type      string `json:"type"`
			Balances  struct {
				Available *float64 `json:"available"`
				Current   *float64 `json:"current"`
				ISO       string   `json:"iso_currency_code"`
			} `json:"balances"`
		} `json:"accounts"`
	}

	if err := h.plaidPost(r, "/accounts/balance/get", map[string]any{
		"access_token": h.accessToken,
	}, &plaidResp); err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to fetch balances")
		return
	}

	result := map[string]any{"accounts": plaidResp.Accounts}
	h.cache.set("balances", result)
	writeJSON(w, http.StatusOK, result)
}

func (h *AdminFinanceHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)

	transactions, spentByCategory, err := h.fetchTransactionsAndSpent(r, start, end)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to fetch transactions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"days":              days,
		"transactions":      transactions,
		"spent_by_category": spentByCategory,
	})
}

func (h *AdminFinanceHandler) GetBudget(w http.ResponseWriter, r *http.Request) {
	budget, err := h.readBudget(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to read budget")
		return
	}

	// For budget, only fetch current month transactions
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := now

	_, spentByCategory, err := h.fetchTransactionsAndSpent(r, monthStart, end)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "failed to fetch transactions")
		return
	}

	merged := make(map[string]BudgetCategorySummary, len(budget.Categories))
	totalAllocated := 0.0
	totalSpent := 0.0
	for key, cat := range budget.Categories {
		spent := spentByCategory[key]
		remaining := cat.Allocated - spent
		merged[key] = BudgetCategorySummary{
			Label:     cat.Label,
			Allocated: cat.Allocated,
			Spent:     spent,
			Remaining: remaining,
		}
		totalAllocated += cat.Allocated
		totalSpent += spent
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"month": budget.Month,
		"totals": map[string]float64{
			"allocated": totalAllocated,
			"spent":     totalSpent,
			"remaining": totalAllocated - totalSpent,
		},
		"categories": merged,
	})
}

func (h *AdminFinanceHandler) PutBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Month      string `json:"month"`
		Categories map[string]struct {
			Label     string  `json:"label"`
			Allocated float64 `json:"allocated"`
		} `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Categories) == 0 {
		h.writeError(w, http.StatusBadRequest, "categories are required")
		return
	}

	month := strings.TrimSpace(req.Month)
	if month == "" {
		month = time.Now().UTC().Format("2006-01")
	}

	budget := BudgetFile{Month: month, Categories: make(map[string]BudgetCategory, len(req.Categories))}
	for key, cat := range req.Categories {
		budget.Categories[key] = BudgetCategory{Label: cat.Label, Allocated: cat.Allocated}
	}

	if err := h.writeBudget(r.Context(), budget); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to save budget")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type plaidTransaction struct {
	TransactionID           string                `json:"transaction_id"`
	Date                    string                `json:"date"`
	Amount                  float64               `json:"amount"`
	Name                    string                `json:"name"`
	PersonalFinanceCategory *plaidFinanceCategory `json:"personal_finance_category,omitempty"`
}

type plaidFinanceCategory struct {
	Primary  string `json:"primary"`
	Detailed string `json:"detailed"`
}

type plaidTransactionsResponse struct {
	Transactions []plaidTransaction `json:"transactions"`
	Total        int                `json:"total_transactions"`
}

func (h *AdminFinanceHandler) fetchTransactionsAndSpent(r *http.Request, start, end time.Time) ([]plaidTransaction, map[string]float64, error) {
	if h.accessToken == "" {
		return nil, nil, fmt.Errorf("plaid access token not configured")
	}

	cacheKey := fmt.Sprintf("transactions:%s:%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	if cached, ok := h.cache.get(cacheKey); ok {
		entry := cached.(cachedTransactions)
		return entry.transactions, entry.spent, nil
	}

	all := make([]plaidTransaction, 0)
	offset := 0
	count := 200
	total := 0
	for {
		var plaidResp plaidTransactionsResponse
		err := h.plaidPost(r, "/transactions/get", map[string]any{
			"access_token": h.accessToken,
			"start_date":   start.Format("2006-01-02"),
			"end_date":     end.Format("2006-01-02"),
			"options": map[string]any{
				"count":  count,
				"offset": offset,
			},
		}, &plaidResp)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, plaidResp.Transactions...)
		total = plaidResp.Total
		offset += len(plaidResp.Transactions)
		if offset >= total || len(plaidResp.Transactions) == 0 {
			break
		}
	}

	spentByCategory := map[string]float64{}
	for _, tx := range all {
		cat := "UNCATEGORIZED"
		if tx.PersonalFinanceCategory != nil && strings.TrimSpace(tx.PersonalFinanceCategory.Primary) != "" {
			cat = tx.PersonalFinanceCategory.Primary
		}
		if tx.Amount > 0 {
			spentByCategory[cat] += tx.Amount
		}
	}

	h.cache.set(cacheKey, cachedTransactions{transactions: all, spent: spentByCategory})
	return all, spentByCategory, nil
}

type cachedTransactions struct {
	transactions []plaidTransaction
	spent        map[string]float64
}

func (h *AdminFinanceHandler) plaidPost(r *http.Request, path string, body map[string]any, out any) error {
	// Plaid docs: client_id and secret go in the request body
	body["client_id"] = h.clientID
	body["secret"] = h.secret

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("plaidPost %s: marshal body: %w", path, err)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.plaidURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("plaidPost %s: create request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("plaid request failed", "path", path, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.logger.Error("plaid non-200 response", "path", path, "status", resp.StatusCode, "body", string(b))
		return fmt.Errorf("plaid status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("plaidPost %s: decode response: %w", path, err)
	}
	return nil
}

func (h *AdminFinanceHandler) readBudget(ctx context.Context) (BudgetFile, error) {
	out, err := h.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(h.s3Bucket),
		Key:    aws.String(h.s3Key),
	})
	if err != nil {
		// If not found, create default
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			defaultBudget := defaultBudgetFile()
			if wErr := h.writeBudget(ctx, defaultBudget); wErr != nil {
				return BudgetFile{}, wErr
			}
			return defaultBudget, nil
		}
		return BudgetFile{}, err
	}
	defer out.Body.Close()

	var budget BudgetFile
	if err := json.NewDecoder(out.Body).Decode(&budget); err != nil {
		return BudgetFile{}, err
	}
	if budget.Categories == nil {
		budget.Categories = map[string]BudgetCategory{}
	}
	return budget, nil
}

// isS3NotFound checks if the error indicates the object doesn't exist.
func isS3NotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	return errors.As(err, &nsk)
}

func (h *AdminFinanceHandler) writeBudget(ctx context.Context, budget BudgetFile) error {
	b, err := json.MarshalIndent(budget, "", "  ")
	if err != nil {
		return fmt.Errorf("writeBudget: marshal: %w", err)
	}
	if _, err = h.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(h.s3Bucket),
		Key:         aws.String(h.s3Key),
		Body:        bytes.NewReader(b),
		ContentType: aws.String("application/json"),
	}); err != nil {
		return fmt.Errorf("writeBudget: put s3 object: %w", err)
	}
	return nil
}

func defaultBudgetFile() BudgetFile {
	return BudgetFile{
		Month: time.Now().UTC().Format("2006-01"),
		Categories: map[string]BudgetCategory{
			"FOOD_AND_DRINK":      {Label: "Food & Groceries", Allocated: 800},
			"TRANSPORTATION":      {Label: "Gas & Transport", Allocated: 150},
			"GENERAL_SERVICES":    {Label: "Business & Services", Allocated: 400},
			"ENTERTAINMENT":       {Label: "Entertainment", Allocated: 100},
			"GENERAL_MERCHANDISE": {Label: "Shopping", Allocated: 200},
			"PERSONAL_CARE":       {Label: "Personal Care", Allocated: 50},
			"LOAN_PAYMENTS":       {Label: "Debt Payments", Allocated: 3500},
			"RENT_AND_UTILITIES":  {Label: "Rent & Utilities", Allocated: 2000},
		},
	}
}

func (h *AdminFinanceHandler) writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
