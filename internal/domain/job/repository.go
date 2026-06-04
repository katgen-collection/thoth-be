package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a saved job does not exist or belongs to another
// user.
var ErrNotFound = errors.New("saved job not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const columns = `id::text, user_id, title, COALESCE(company, ''), COALESCE(location, ''),
	COALESCE(description, ''), COALESCE(apply_link, ''), source, status,
	COALESCE(notes, ''), applied_at, created_at, updated_at`

// Create inserts a saved job and returns the stored row.
func (r *Repository) Create(ctx context.Context, j SavedJob) (*SavedJob, error) {
	if j.Status == "" {
		j.Status = StatusSaved
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO saved_jobs (user_id, title, company, location, description, apply_link, source, status, notes)
		 VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), $7, $8, NULLIF($9,''))
		 RETURNING `+columns,
		j.UserID, j.Title, j.Company, j.Location, j.Description, j.ApplyLink,
		nullableJSON(j.Source), j.Status, j.Notes,
	)
	return scanJob(row)
}

// List returns a user's saved jobs, newest first, optionally filtered by status.
func (r *Repository) List(ctx context.Context, userID, statusFilter string) ([]SavedJob, error) {
	query := `SELECT ` + columns + ` FROM saved_jobs WHERE user_id = $1`
	args := []any{userID}
	if statusFilter != "" {
		query += ` AND status = $2`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list saved jobs: %w", err)
	}
	defer rows.Close()

	var out []SavedJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// Get fetches one saved job scoped to a user.
func (r *Repository) Get(ctx context.Context, id, userID string) (*SavedJob, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+columns+` FROM saved_jobs WHERE id = $1 AND user_id = $2`, id, userID)
	return scanJob(row)
}

// UpdateStatus changes the tracker status. Moving to "applied" stamps
// applied_at if not already set.
func (r *Repository) UpdateStatus(ctx context.Context, id, userID, status string) (*SavedJob, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE saved_jobs
		 SET status = $3,
		     applied_at = CASE WHEN $3 = 'applied' AND applied_at IS NULL THEN now() ELSE applied_at END,
		     updated_at = now()
		 WHERE id = $1 AND user_id = $2
		 RETURNING `+columns,
		id, userID, status)
	j, err := scanJob(row)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	return j, err
}

// Delete removes a saved job.
func (r *Repository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM saved_jobs WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete saved job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*SavedJob, error) {
	var (
		j         SavedJob
		sourceRaw []byte
	)
	err := row.Scan(&j.ID, &j.UserID, &j.Title, &j.Company, &j.Location,
		&j.Description, &j.ApplyLink, &sourceRaw, &j.Status, &j.Notes,
		&j.AppliedAt, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan saved job: %w", err)
	}
	if len(sourceRaw) > 0 {
		j.Source = json.RawMessage(sourceRaw)
	}
	return &j, nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}
