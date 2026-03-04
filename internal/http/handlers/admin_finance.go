package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

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
	budgetPath  string
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

func NewAdminFinanceHandler(logger *logging.Logger, budgetPath string, plaid PlaidConfig) *AdminFinanceHandler {
	if logger == nil {
		logger = logging.Default()
	}
	if plaid.BaseURL == "" {
		plaid.BaseURL = "https://production.plaid.com"
	}
	if budgetPath == "" {
		budgetPath = filepath.Join("data", "budget.json")
	}

	return &AdminFinanceHandler{
		logger:      logger,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		plaidURL:    plaid.BaseURL,
		clientID:    plaid.ClientID,
		secret:      plaid.Secret,
		accessToken: plaid.AccessToken,
		budgetPath:  budgetPath,
	}
}

func (h *AdminFinanceHandler) GetBalances(w http.ResponseWriter, r *http.Request) {
	if h.accessToken == "" {
		h.writeError(w, http.StatusServiceUnavailable, "PLAID_ACCESS_TOKEN not configured")
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

	writeJSON(w, http.StatusOK, map[string]any{"accounts": plaidResp.Accounts})
}

func (h *AdminFinanceHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	transactions, spentByCategory, err := h.fetchTransactionsAndSpent(r, days)
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
	budget, err := h.readBudget()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to read budget")
		return
	}

	_, spentByCategory, err := h.fetchTransactionsAndSpent(r, 90)
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

	if err := h.writeBudget(budget); err != nil {
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

func (h *AdminFinanceHandler) fetchTransactionsAndSpent(r *http.Request, days int) ([]plaidTransaction, map[string]float64, error) {
	if h.accessToken == "" {
		return nil, nil, fmt.Errorf("plaid access token not configured")
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)
	monthStart := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)

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
		txDate, err := time.Parse("2006-01-02", tx.Date)
		if err != nil {
			continue
		}
		if txDate.Before(monthStart) {
			continue
		}
		if tx.Amount > 0 {
			spentByCategory[cat] += tx.Amount
		}
	}

	return all, spentByCategory, nil
}

func (h *AdminFinanceHandler) plaidPost(r *http.Request, path string, body map[string]any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.plaidURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PLAID-CLIENT-ID", h.clientID)
	req.Header.Set("PLAID-SECRET", h.secret)

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

	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *AdminFinanceHandler) readBudget() (BudgetFile, error) {
	if _, err := os.Stat(h.budgetPath); os.IsNotExist(err) {
		defaultBudget := defaultBudgetFile()
		if err := h.writeBudget(defaultBudget); err != nil {
			return BudgetFile{}, err
		}
		return defaultBudget, nil
	}

	b, err := os.ReadFile(h.budgetPath)
	if err != nil {
		return BudgetFile{}, err
	}
	var budget BudgetFile
	if err := json.Unmarshal(b, &budget); err != nil {
		return BudgetFile{}, err
	}
	if budget.Categories == nil {
		budget.Categories = map[string]BudgetCategory{}
	}
	return budget, nil
}

func (h *AdminFinanceHandler) writeBudget(budget BudgetFile) error {
	if err := os.MkdirAll(filepath.Dir(h.budgetPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(budget, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.budgetPath, append(b, '\n'), 0o644)
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
