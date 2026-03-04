package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock S3
// ---------------------------------------------------------------------------

type mockS3 struct {
	data map[string][]byte
}

func newMockS3() *mockS3 {
	return &mockS3{data: make(map[string][]byte)}
}

func (m *mockS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := *in.Key
	b, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("NoSuchKey: %s", key)
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(b)),
	}, nil
}

func (m *mockS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	m.data[*in.Key] = b
	return &s3.PutObjectOutput{}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestHandler(s3c S3Client) *AdminFinanceHandler {
	return NewAdminFinanceHandler(nil, s3c, "test-bucket", PlaidConfig{
		BaseURL:     "http://plaid.test",
		ClientID:    "test-client",
		Secret:      "test-secret",
		AccessToken: "test-token",
	})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFinance_ReadBudgetDefault(t *testing.T) {
	ms := newMockS3()
	h := newTestHandler(ms)

	budget, err := h.readBudget(context.Background())
	require.NoError(t, err)
	assert.Equal(t, time.Now().UTC().Format("2006-01"), budget.Month)
	assert.NotEmpty(t, budget.Categories)

	// Should have been persisted to S3
	assert.Contains(t, ms.data, "finance/budget.json")
}

func TestFinance_WriteThenReadBudget(t *testing.T) {
	ms := newMockS3()
	h := newTestHandler(ms)

	budget := BudgetFile{
		Month: "2026-03",
		Categories: map[string]BudgetCategory{
			"FOOD": {Label: "Food", Allocated: 500},
		},
	}
	err := h.writeBudget(context.Background(), budget)
	require.NoError(t, err)

	got, err := h.readBudget(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2026-03", got.Month)
	assert.Equal(t, 500.0, got.Categories["FOOD"].Allocated)
}

func TestFinance_DefaultBudgetFileDynamicMonth(t *testing.T) {
	b := defaultBudgetFile()
	expected := time.Now().UTC().Format("2006-01")
	assert.Equal(t, expected, b.Month)
}

func TestFinance_PutBudgetValidation(t *testing.T) {
	ms := newMockS3()
	h := newTestHandler(ms)

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"invalid json", `{bad`, http.StatusBadRequest},
		{"empty categories", `{"categories":{}}`, http.StatusBadRequest},
		{"valid", `{"month":"2026-03","categories":{"FOOD":{"label":"Food","allocated":100}}}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/budget", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			h.PutBudget(w, req)
			assert.Equal(t, tt.status, w.Code)
		})
	}
}

func TestFinance_SpendingAggregation(t *testing.T) {
	// Test the aggregation logic in fetchTransactionsAndSpent by
	// spinning up a fake Plaid server.
	fakePlaid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth is in body, not headers
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test-client", body["client_id"])
		assert.Equal(t, "test-secret", body["secret"])
		assert.Empty(t, r.Header.Get("PLAID-CLIENT-ID"), "client_id should be in body, not header")

		resp := plaidTransactionsResponse{
			Transactions: []plaidTransaction{
				{TransactionID: "1", Date: time.Now().UTC().Format("2006-01-02"), Amount: 50, Name: "Grocery", PersonalFinanceCategory: &plaidFinanceCategory{Primary: "FOOD"}},
				{TransactionID: "2", Date: time.Now().UTC().Format("2006-01-02"), Amount: 25, Name: "Gas", PersonalFinanceCategory: &plaidFinanceCategory{Primary: "TRANSPORT"}},
				{TransactionID: "3", Date: time.Now().UTC().Format("2006-01-02"), Amount: -10, Name: "Refund", PersonalFinanceCategory: &plaidFinanceCategory{Primary: "FOOD"}},
				{TransactionID: "4", Date: time.Now().UTC().Format("2006-01-02"), Amount: 30, Name: "Unknown", PersonalFinanceCategory: nil},
			},
			Total: 4,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer fakePlaid.Close()

	h := NewAdminFinanceHandler(nil, newMockS3(), "test-bucket", PlaidConfig{
		BaseURL:     fakePlaid.URL,
		ClientID:    "test-client",
		Secret:      "test-secret",
		AccessToken: "test-token",
	})

	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	txs, spent, err := h.fetchTransactionsAndSpent(req, start, now)
	require.NoError(t, err)

	assert.Len(t, txs, 4)
	assert.Equal(t, 50.0, spent["FOOD"]) // negative amount excluded
	assert.Equal(t, 25.0, spent["TRANSPORT"])
	assert.Equal(t, 30.0, spent["UNCATEGORIZED"])
}

func TestFinance_CacheTTL(t *testing.T) {
	c := newPlaidCache(50 * time.Millisecond)
	c.set("key", "value")

	v, ok := c.get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", v)

	time.Sleep(60 * time.Millisecond)
	_, ok = c.get("key")
	assert.False(t, ok, "cache entry should have expired")
}
