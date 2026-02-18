package prospects

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]Prospect, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, clinic_name, owner_name, owner_title, location, phone, email, website,
		       emr, status, configured, telnyx_number, ten_dlc, sms_working, org_id,
		       services_count, providers, next_action, notes, created_at, updated_at
		FROM prospects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Prospect
	for rows.Next() {
		var p Prospect
		if err := rows.Scan(&p.ID, &p.ClinicName, &p.OwnerName, &p.OwnerTitle, &p.Location,
			&p.Phone, &p.Email, &p.Website, &p.EMR, &p.Status, &p.Configured,
			&p.TelnyxNumber, &p.TenDLC, &p.SMSWorking, &p.OrgID, &p.ServicesCount,
			pq.Array(&p.Providers), &p.NextAction, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if p.Providers == nil {
			p.Providers = []string{}
		}
		out = append(out, p)
	}
	if out == nil {
		out = []Prospect{}
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (*Prospect, error) {
	var p Prospect
	err := r.db.QueryRowContext(ctx, `
		SELECT id, clinic_name, owner_name, owner_title, location, phone, email, website,
		       emr, status, configured, telnyx_number, ten_dlc, sms_working, org_id,
		       services_count, providers, next_action, notes, created_at, updated_at
		FROM prospects WHERE id = $1`, id).Scan(
		&p.ID, &p.ClinicName, &p.OwnerName, &p.OwnerTitle, &p.Location,
		&p.Phone, &p.Email, &p.Website, &p.EMR, &p.Status, &p.Configured,
		&p.TelnyxNumber, &p.TenDLC, &p.SMSWorking, &p.OrgID, &p.ServicesCount,
		pq.Array(&p.Providers), &p.NextAction, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.Providers == nil {
		p.Providers = []string{}
	}

	events, err := r.ListEvents(ctx, id)
	if err != nil {
		return nil, err
	}
	p.Timeline = events
	return &p, nil
}

func (r *Repository) Upsert(ctx context.Context, p *Prospect) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO prospects (id, clinic_name, owner_name, owner_title, location, phone, email, website,
		    emr, status, configured, telnyx_number, ten_dlc, sms_working, org_id,
		    services_count, providers, next_action, notes, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20)
		ON CONFLICT (id) DO UPDATE SET
		    clinic_name=EXCLUDED.clinic_name, owner_name=EXCLUDED.owner_name, owner_title=EXCLUDED.owner_title,
		    location=EXCLUDED.location, phone=EXCLUDED.phone, email=EXCLUDED.email, website=EXCLUDED.website,
		    emr=EXCLUDED.emr, status=EXCLUDED.status, configured=EXCLUDED.configured,
		    telnyx_number=EXCLUDED.telnyx_number, ten_dlc=EXCLUDED.ten_dlc, sms_working=EXCLUDED.sms_working,
		    org_id=EXCLUDED.org_id, services_count=EXCLUDED.services_count, providers=EXCLUDED.providers,
		    next_action=EXCLUDED.next_action, notes=EXCLUDED.notes, updated_at=$20`,
		p.ID, p.ClinicName, p.OwnerName, p.OwnerTitle, p.Location, p.Phone, p.Email, p.Website,
		p.EMR, p.Status, p.Configured, p.TelnyxNumber, p.TenDLC, p.SMSWorking, p.OrgID,
		p.ServicesCount, pq.Array(p.Providers), p.NextAction, p.Notes, now)
	return err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM prospects WHERE id = $1`, id)
	return err
}

func (r *Repository) AddEvent(ctx context.Context, e *Event) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO prospect_events (prospect_id, event_type, event_date, note)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		e.ProspectID, e.Type, e.Date, e.Note).Scan(&e.ID)
}

func (r *Repository) ListEvents(ctx context.Context, prospectID string) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, prospect_id, event_type, event_date, note
		FROM prospect_events WHERE prospect_id = $1 ORDER BY event_date ASC`, prospectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ProspectID, &e.Type, &e.Date, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []Event{}
	}
	return out, rows.Err()
}
