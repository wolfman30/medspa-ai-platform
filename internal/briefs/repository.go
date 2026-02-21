package briefs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MorningBrief represents a stored morning brief.
type MorningBrief struct {
	ID        int       `json:"id"`
	Date      string    `json:"date"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Summary   *string   `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PostgresBriefsRepository provides Postgres-backed brief storage.
type PostgresBriefsRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresBriefsRepository creates a new repository.
func NewPostgresBriefsRepository(pool *pgxpool.Pool) *PostgresBriefsRepository {
	return &PostgresBriefsRepository{pool: pool}
}

// List returns all briefs, newest first.
func (r *PostgresBriefsRepository) List(ctx context.Context) ([]MorningBrief, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, date, title, content, summary, created_at, updated_at
		 FROM morning_briefs ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var briefs []MorningBrief
	for rows.Next() {
		var b MorningBrief
		var d time.Time
		if err := rows.Scan(&b.ID, &d, &b.Title, &b.Content, &b.Summary, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Date = d.Format("2006-01-02")
		briefs = append(briefs, b)
	}
	return briefs, rows.Err()
}

// GetByDate returns a single brief by date string (YYYY-MM-DD).
func (r *PostgresBriefsRepository) GetByDate(ctx context.Context, date string) (*MorningBrief, error) {
	var b MorningBrief
	var d time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT id, date, title, content, summary, created_at, updated_at
		 FROM morning_briefs WHERE date = $1`, date).
		Scan(&b.ID, &d, &b.Title, &b.Content, &b.Summary, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	b.Date = d.Format("2006-01-02")
	return &b, nil
}

// Upsert inserts or updates a brief by date.
func (r *PostgresBriefsRepository) Upsert(ctx context.Context, date, title, content string, summary *string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO morning_briefs (date, title, content, summary, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (date) DO UPDATE SET
		   title = EXCLUDED.title,
		   content = EXCLUDED.content,
		   summary = COALESCE(EXCLUDED.summary, morning_briefs.summary),
		   updated_at = now()`,
		date, title, content, summary)
	return err
}
