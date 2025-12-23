package clinic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type stubDashboardRepo struct {
	cohort []MissedCallCohortDay
	err    error

	gotOrg   string
	gotStart time.Time
	gotEnd   time.Time
}

func (s *stubDashboardRepo) MissedCallCohortByDay(_ context.Context, orgID string, start, end time.Time) ([]MissedCallCohortDay, error) {
	s.gotOrg = orgID
	s.gotStart = start
	s.gotEnd = end
	return s.cohort, s.err
}

type stubGatherer struct {
	families []*dto.MetricFamily
	err      error
}

func (s stubGatherer) Gather() ([]*dto.MetricFamily, error) {
	return s.families, s.err
}

func TestDashboardHandler_FillsMissingDaysAndCalculatesConversion(t *testing.T) {
	orgID := "default-org"
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)

	repo := &stubDashboardRepo{
		cohort: []MissedCallCohortDay{
			{Day: start, DayLabel: "2025-01-01", MissedCallLeads: 2, PaidLeads: 1},
			{Day: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), DayLabel: "2025-01-03", MissedCallLeads: 1, PaidLeads: 0},
		},
	}

	familyName := "medspa_conversation_llm_latency_seconds"
	metricType := dto.MetricType_HISTOGRAM
	modelLabel := "model"
	statusLabel := "status"
	ok := "ok"

	gatherer := stubGatherer{
		families: []*dto.MetricFamily{
			{
				Name: &familyName,
				Type: &metricType,
				Metric: []*dto.Metric{
					{
						Label: []*dto.LabelPair{
							{Name: &modelLabel, Value: ptrString("test")},
							{Name: &statusLabel, Value: &ok},
						},
						Histogram: &dto.Histogram{
							SampleCount: ptrUint64(10),
							Bucket: []*dto.Bucket{
								{UpperBound: ptrFloat64(1.0), CumulativeCount: ptrUint64(5)},
								{UpperBound: ptrFloat64(2.0), CumulativeCount: ptrUint64(9)},
								{UpperBound: ptrFloat64(3.0), CumulativeCount: ptrUint64(10)},
							},
						},
					},
				},
			},
		},
	}

	handler := NewDashboardHandler(repo, gatherer, logging.Default())

	r := chi.NewRouter()
	r.Get("/admin/clinics/{orgID}/dashboard", handler.GetDashboard)

	req := httptest.NewRequest(http.MethodGet, "/admin/clinics/"+orgID+"/dashboard?start=2025-01-01T00:00:00Z&end=2025-01-04T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ClinicDashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.OrgID != orgID {
		t.Fatalf("org_id = %q, want %q", resp.OrgID, orgID)
	}
	if resp.MissedCallLeads != 3 {
		t.Fatalf("missed_call_leads = %d, want 3", resp.MissedCallLeads)
	}
	if resp.MissedCallPaidLeads != 1 {
		t.Fatalf("missed_call_paid_leads = %d, want 1", resp.MissedCallPaidLeads)
	}
	if resp.MissedCallConversionPct < 33.3 || resp.MissedCallConversionPct > 33.4 {
		t.Fatalf("missed_call_conversion_pct = %f, want ~33.33", resp.MissedCallConversionPct)
	}

	if len(resp.Daily) != 3 {
		t.Fatalf("daily length = %d, want 3", len(resp.Daily))
	}
	if resp.Daily[1].DayLabel != "2025-01-02" || resp.Daily[1].MissedCallLeads != 0 || resp.Daily[1].PaidLeads != 0 {
		t.Fatalf("expected missing day 2025-01-02 to be filled with zeros, got %#v", resp.Daily[1])
	}

	if resp.LLMLatency.Total != 10 {
		t.Fatalf("llm_latency.total = %d, want 10", resp.LLMLatency.Total)
	}
	if resp.LLMLatency.P90Ms < 1999 || resp.LLMLatency.P90Ms > 2001 {
		t.Fatalf("llm_latency.p90_ms = %f, want ~2000", resp.LLMLatency.P90Ms)
	}
	if resp.LLMLatency.P95Ms < 2499 || resp.LLMLatency.P95Ms > 2501 {
		t.Fatalf("llm_latency.p95_ms = %f, want ~2500", resp.LLMLatency.P95Ms)
	}

	// Ensure handler uses passed window.
	if repo.gotOrg != orgID || !repo.gotStart.Equal(start) || !repo.gotEnd.Equal(end) {
		t.Fatalf("repo called with (%q, %s, %s); want (%q, %s, %s)", repo.gotOrg, repo.gotStart, repo.gotEnd, orgID, start, end)
	}
}

func TestSnapshotLLMLatency_NoMetrics(t *testing.T) {
	lat := snapshotLLMLatency(stubGatherer{families: nil})
	if lat.Total != 0 {
		t.Fatalf("expected total=0, got %d", lat.Total)
	}
}

var _ prometheus.Gatherer = stubGatherer{}

func ptrString(v string) *string { return &v }

func ptrUint64(v uint64) *uint64 { return &v }

func ptrFloat64(v float64) *float64 { return &v }

