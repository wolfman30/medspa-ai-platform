package instagram

import (
	"context"
	"database/sql"
	"fmt"
)

// DBOrgResolver resolves org IDs from Instagram page IDs using the database.
type DBOrgResolver struct {
	db *sql.DB
}

// NewDBOrgResolver creates an OrgResolver backed by the instagram_page_mappings table.
func NewDBOrgResolver(db *sql.DB) *DBOrgResolver {
	return &DBOrgResolver{db: db}
}

// ResolveByInstagramPageID looks up the org ID for the given Instagram page ID.
func (r *DBOrgResolver) ResolveByInstagramPageID(ctx context.Context, pageID string) (string, error) {
	var orgID string
	err := r.db.QueryRowContext(ctx,
		`SELECT org_id FROM instagram_page_mappings WHERE page_id = $1`, pageID,
	).Scan(&orgID)
	if err != nil {
		return "", fmt.Errorf("resolve org by instagram page %s: %w", pageID, err)
	}
	return orgID, nil
}

// DBIdentityStore implements IdentityStore backed by patient_instagram_identities.
type DBIdentityStore struct {
	db *sql.DB
}

// NewDBIdentityStore creates an IdentityStore backed by the database.
func NewDBIdentityStore(db *sql.DB) *DBIdentityStore {
	return &DBIdentityStore{db: db}
}

// LinkInstagramToPhone links an Instagram sender ID to a phone number for cross-channel identity.
func (s *DBIdentityStore) LinkInstagramToPhone(ctx context.Context, orgID, igSenderID, phoneE164 string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO patient_instagram_identities (instagram_scoped_id, org_id, phone)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (instagram_scoped_id, org_id)
		 DO UPDATE SET phone = EXCLUDED.phone`,
		igSenderID, orgID, phoneE164,
	)
	if err != nil {
		return fmt.Errorf("link instagram to phone: %w", err)
	}
	return nil
}

// FindPatientByInstagramID looks up a patient by their Instagram sender ID.
func (s *DBIdentityStore) FindPatientByInstagramID(ctx context.Context, orgID, igSenderID string) (*PatientIdentity, error) {
	var p PatientIdentity
	err := s.db.QueryRowContext(ctx,
		`SELECT instagram_scoped_id, org_id, COALESCE(phone, '') FROM patient_instagram_identities
		 WHERE instagram_scoped_id = $1 AND org_id = $2`,
		igSenderID, orgID,
	).Scan(&p.ChannelID, &p.OrgID, &p.PatientID)
	if err != nil {
		return nil, fmt.Errorf("find patient by instagram id: %w", err)
	}
	p.ChannelType = "instagram"
	return &p, nil
}

// FindPatientByPhone looks up a patient by phone number.
func (s *DBIdentityStore) FindPatientByPhone(ctx context.Context, orgID, phoneE164 string) (*PatientIdentity, error) {
	var p PatientIdentity
	err := s.db.QueryRowContext(ctx,
		`SELECT instagram_scoped_id, org_id, COALESCE(phone, '') FROM patient_instagram_identities
		 WHERE phone = $1 AND org_id = $2`,
		phoneE164, orgID,
	).Scan(&p.ChannelID, &p.OrgID, &p.PatientID)
	if err != nil {
		return nil, fmt.Errorf("find patient by phone: %w", err)
	}
	p.ChannelType = "instagram"
	return &p, nil
}
