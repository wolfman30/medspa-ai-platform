package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type stubOnboardingDB struct {
	called    bool
	lastQuery string
	lastArgs  []any
	rowErr    error
}

func (s *stubOnboardingDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	s.called = true
	s.lastQuery = sql
	s.lastArgs = args
	return stubOnboardingRow{err: s.rowErr}
}

type stubOnboardingRow struct {
	err error
}

func (r stubOnboardingRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if id, ok := dest[0].(*string); ok {
			*id = "ok"
		}
	}
	return nil
}

func TestAdminOnboardingCreateClinicUpsertsOrganization(t *testing.T) {
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := clinic.NewStore(redisClient)
	db := &stubOnboardingDB{}

	handler := NewAdminOnboardingHandler(AdminOnboardingConfig{
		DB:          db,
		Redis:       redisClient,
		ClinicStore: store,
		Logger:      logging.Default(),
	})

	payload := `{"name":"Glow Clinic","email":"info@glow.test","phone":"+15551234567","timezone":"America/Chicago"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/clinics", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	handler.CreateClinic(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}
	if !db.called {
		t.Fatalf("expected organizations upsert to be called")
	}
	var resp CreateClinicResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OrgID == "" {
		t.Fatalf("expected org_id to be set")
	}
	if len(db.lastArgs) < 5 {
		t.Fatalf("expected upsert args, got %d", len(db.lastArgs))
	}
	if db.lastArgs[0] != resp.OrgID {
		t.Fatalf("expected org_id %s, got %v", resp.OrgID, db.lastArgs[0])
	}
	if db.lastArgs[1] != "Glow Clinic" {
		t.Fatalf("expected name Glow Clinic, got %v", db.lastArgs[1])
	}
	if db.lastArgs[2] != "+15551234567" {
		t.Fatalf("expected phone +15551234567, got %v", db.lastArgs[2])
	}
	if db.lastArgs[3] != "info@glow.test" {
		t.Fatalf("expected email info@glow.test, got %v", db.lastArgs[3])
	}
	if db.lastArgs[4] != "America/Chicago" {
		t.Fatalf("expected timezone America/Chicago, got %v", db.lastArgs[4])
	}

	cfg, err := store.Get(context.Background(), resp.OrgID)
	if err != nil {
		t.Fatalf("failed to load clinic config: %v", err)
	}
	if cfg.Name != "Glow Clinic" {
		t.Fatalf("expected clinic name Glow Clinic, got %s", cfg.Name)
	}
}

func TestAdminOnboardingCreateClinicUpsertFails(t *testing.T) {
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := clinic.NewStore(redisClient)
	db := &stubOnboardingDB{rowErr: errors.New("db down")}

	handler := NewAdminOnboardingHandler(AdminOnboardingConfig{
		DB:          db,
		Redis:       redisClient,
		ClinicStore: store,
		Logger:      logging.Default(),
	})

	payload := `{"name":"Glow Clinic"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/clinics", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	handler.CreateClinic(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
	if len(mr.Keys()) != 0 {
		t.Fatalf("expected no redis keys when upsert fails")
	}
}
