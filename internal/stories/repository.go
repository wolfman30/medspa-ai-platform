package stories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, status, priority, label string) ([]Story, error) {
	query := `
		SELECT s.id, s.title, s.description, s.status, s.priority, s.labels,
		       s.parent_id, s.assigned_to, s.created_at, s.updated_at, s.completed_at,
		       (SELECT COUNT(*) FROM stories c WHERE c.parent_id = s.id) AS sub_task_count
		FROM stories s WHERE 1=1`
	args := []interface{}{}
	n := 0

	if status != "" {
		n++
		query += fmt.Sprintf(" AND s.status = $%d", n)
		args = append(args, status)
	}
	if priority != "" {
		n++
		query += fmt.Sprintf(" AND s.priority = $%d", n)
		args = append(args, priority)
	}
	if label != "" {
		n++
		query += fmt.Sprintf(" AND $%d = ANY(s.labels)", n)
		args = append(args, label)
	}

	query += " ORDER BY s.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing stories: %w", err)
	}
	defer rows.Close()

	out := []Story{}
	for rows.Next() {
		var s Story
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.Status, &s.Priority,
			pq.Array(&s.Labels), &s.ParentID, &s.AssignedTo,
			&s.CreatedAt, &s.UpdatedAt, &s.CompletedAt, &s.SubTaskCount); err != nil {
			return nil, fmt.Errorf("scanning story: %w", err)
		}
		if s.Labels == nil {
			s.Labels = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (*Story, error) {
	var s Story
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.title, s.description, s.status, s.priority, s.labels,
		       s.parent_id, s.assigned_to, s.created_at, s.updated_at, s.completed_at,
		       (SELECT COUNT(*) FROM stories c WHERE c.parent_id = s.id) AS sub_task_count
		FROM stories s WHERE s.id = $1`, id).Scan(
		&s.ID, &s.Title, &s.Description, &s.Status, &s.Priority,
		pq.Array(&s.Labels), &s.ParentID, &s.AssignedTo,
		&s.CreatedAt, &s.UpdatedAt, &s.CompletedAt, &s.SubTaskCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting story %s: %w", id, err)
	}
	if s.Labels == nil {
		s.Labels = []string{}
	}
	return &s, nil
}

func (r *Repository) GetSubTasks(ctx context.Context, parentID string) ([]Story, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.title, s.description, s.status, s.priority, s.labels,
		       s.parent_id, s.assigned_to, s.created_at, s.updated_at, s.completed_at,
		       (SELECT COUNT(*) FROM stories c WHERE c.parent_id = s.id) AS sub_task_count
		FROM stories s WHERE s.parent_id = $1
		ORDER BY s.created_at ASC`, parentID)
	if err != nil {
		return nil, fmt.Errorf("listing sub-tasks for %s: %w", parentID, err)
	}
	defer rows.Close()

	out := []Story{}
	for rows.Next() {
		var s Story
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.Status, &s.Priority,
			pq.Array(&s.Labels), &s.ParentID, &s.AssignedTo,
			&s.CreatedAt, &s.UpdatedAt, &s.CompletedAt, &s.SubTaskCount); err != nil {
			return nil, fmt.Errorf("scanning sub-task: %w", err)
		}
		if s.Labels == nil {
			s.Labels = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) Create(ctx context.Context, req CreateStoryRequest) (*Story, error) {
	if req.Status == "" {
		req.Status = "backlog"
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	if req.Labels == nil {
		req.Labels = []string{}
	}

	var s Story
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO stories (title, description, status, priority, labels, parent_id, assigned_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, title, description, status, priority, labels, parent_id, assigned_to,
		          created_at, updated_at, completed_at`,
		req.Title, req.Description, req.Status, req.Priority,
		pq.Array(req.Labels), req.ParentID, req.AssignedTo).Scan(
		&s.ID, &s.Title, &s.Description, &s.Status, &s.Priority,
		pq.Array(&s.Labels), &s.ParentID, &s.AssignedTo,
		&s.CreatedAt, &s.UpdatedAt, &s.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("creating story: %w", err)
	}
	if s.Labels == nil {
		s.Labels = []string{}
	}
	return &s, nil
}

func (r *Repository) Update(ctx context.Context, id string, req UpdateStoryRequest) (*Story, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}

	title := existing.Title
	description := existing.Description
	status := existing.Status
	priority := existing.Priority
	labels := existing.Labels
	parentID := existing.ParentID
	assignedTo := existing.AssignedTo

	if req.Title != nil {
		title = *req.Title
	}
	if req.Description != nil {
		description = *req.Description
	}
	if req.Status != nil {
		status = *req.Status
	}
	if req.Priority != nil {
		priority = *req.Priority
	}
	if req.Labels != nil {
		labels = req.Labels
	}
	if req.ParentID != nil {
		if *req.ParentID == "" {
			parentID = nil
		} else {
			parentID = req.ParentID
		}
	}
	if req.AssignedTo != nil {
		assignedTo = *req.AssignedTo
	}

	var completedAt *time.Time
	if status == "done" && existing.Status != "done" {
		now := time.Now()
		completedAt = &now
	} else if status == "done" {
		completedAt = existing.CompletedAt
	}

	var s Story
	err = r.db.QueryRowContext(ctx, `
		UPDATE stories SET title=$1, description=$2, status=$3, priority=$4, labels=$5,
		       parent_id=$6, assigned_to=$7, updated_at=NOW(), completed_at=$8
		WHERE id=$9
		RETURNING id, title, description, status, priority, labels, parent_id, assigned_to,
		          created_at, updated_at, completed_at`,
		title, description, status, priority, pq.Array(labels),
		parentID, assignedTo, completedAt, id).Scan(
		&s.ID, &s.Title, &s.Description, &s.Status, &s.Priority,
		pq.Array(&s.Labels), &s.ParentID, &s.AssignedTo,
		&s.CreatedAt, &s.UpdatedAt, &s.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("updating story %s: %w", id, err)
	}
	s.SubTaskCount = existing.SubTaskCount
	if s.Labels == nil {
		s.Labels = []string{}
	}
	return &s, nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM stories WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("deleting story %s: %w", id, err)
	}
	return nil
}
