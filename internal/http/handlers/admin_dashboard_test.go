package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestListOrganizations_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewAdminDashboardHandler(db, logging.Default())

	// Mock the query that returns organizations (in alphabetical order by name as SQL would return)
	rows := sqlmock.NewRows([]string{"id", "name", "owner_email", "created_at"}).
		AddRow("org-789", "AI Wolf Solutions", nil, time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)).
		AddRow("org-456", "Forever 22 Med Spa", "owner@forever22.com", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)).
		AddRow("org-123", "Glow Medspa", "owner@glow.com", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	mock.ExpectQuery("SELECT id, name, owner_email, created_at FROM organizations ORDER BY name ASC").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/admin/orgs", nil)
	rec := httptest.NewRecorder()

	handler.ListOrganizations(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListOrganizationsResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 3, resp.Total)
	assert.Len(t, resp.Organizations, 3)

	// Check first org (alphabetical order - AI Wolf Solutions)
	assert.Equal(t, "org-789", resp.Organizations[0].ID)
	assert.Equal(t, "AI Wolf Solutions", resp.Organizations[0].Name)
	assert.Nil(t, resp.Organizations[0].OwnerEmail)

	// Check second org (Forever 22 Med Spa)
	assert.Equal(t, "org-456", resp.Organizations[1].ID)
	assert.Equal(t, "Forever 22 Med Spa", resp.Organizations[1].Name)
	require.NotNil(t, resp.Organizations[1].OwnerEmail)
	assert.Equal(t, "owner@forever22.com", *resp.Organizations[1].OwnerEmail)

	// Check third org (Glow Medspa)
	assert.Equal(t, "org-123", resp.Organizations[2].ID)
	assert.Equal(t, "Glow Medspa", resp.Organizations[2].Name)
	require.NotNil(t, resp.Organizations[2].OwnerEmail)
	assert.Equal(t, "owner@glow.com", *resp.Organizations[2].OwnerEmail)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListOrganizations_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewAdminDashboardHandler(db, logging.Default())

	// Return empty result set
	rows := sqlmock.NewRows([]string{"id", "name", "owner_email", "created_at"})
	mock.ExpectQuery("SELECT id, name, owner_email, created_at FROM organizations ORDER BY name ASC").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/admin/orgs", nil)
	rec := httptest.NewRecorder()

	handler.ListOrganizations(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListOrganizationsResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 0, resp.Total)
	// Organizations should be nil or empty slice
	assert.Empty(t, resp.Organizations)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListOrganizations_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewAdminDashboardHandler(db, logging.Default())

	mock.ExpectQuery("SELECT id, name, owner_email, created_at FROM organizations ORDER BY name ASC").
		WillReturnError(assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/admin/orgs", nil)
	rec := httptest.NewRecorder()

	handler.ListOrganizations(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListOrganizations_NullOwnerEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewAdminDashboardHandler(db, logging.Default())

	// Test that null owner_email is handled correctly
	rows := sqlmock.NewRows([]string{"id", "name", "owner_email", "created_at"}).
		AddRow("org-123", "Test Org", nil, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	mock.ExpectQuery("SELECT id, name, owner_email, created_at FROM organizations ORDER BY name ASC").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/admin/orgs", nil)
	rec := httptest.NewRecorder()

	handler.ListOrganizations(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListOrganizationsResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Total)
	assert.Len(t, resp.Organizations, 1)
	assert.Equal(t, "org-123", resp.Organizations[0].ID)
	assert.Equal(t, "Test Org", resp.Organizations[0].Name)
	assert.Nil(t, resp.Organizations[0].OwnerEmail)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListOrganizations_CreatedAtFormat(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewAdminDashboardHandler(db, logging.Default())

	testTime := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "name", "owner_email", "created_at"}).
		AddRow("org-123", "Test Org", "test@example.com", testTime)

	mock.ExpectQuery("SELECT id, name, owner_email, created_at FROM organizations ORDER BY name ASC").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/admin/orgs", nil)
	rec := httptest.NewRecorder()

	handler.ListOrganizations(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListOrganizationsResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	// Verify the timestamp is in RFC3339 format
	assert.Equal(t, "2025-06-15T14:30:45Z", resp.Organizations[0].CreatedAt)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}
