package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a search job does not exist (or belongs to
// another user).
var ErrNotFound = errors.New("search job not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new pending search job and returns its generated ID.
func (r *Repository) Create(ctx context.Context, userID, query string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO search_jobs (user_id, query, status, progress)
		 VALUES ($1, $2, $3, 0) RETURNING id::text`,
		userID, query, StatusPending,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create search job: %w", err)
	}
	return id, nil
}

// Get fetches a search job scoped to a user.
func (r *Repository) Get(ctx context.Context, id, userID string) (*SearchJob, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id::text, user_id, query, status, progress,
		        extracted_params, result, COALESCE(error, ''), created_at, updated_at
		 FROM search_jobs WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	return scanJob(row)
}

// List returns a user's search jobs ordered newest-first, plus the total count.
func (r *Repository) List(ctx context.Context, userID string, limit, offset int) ([]SearchJob, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM search_jobs WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count search jobs: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id::text, user_id, query, status, progress,
		        extracted_params, result, COALESCE(error, ''), created_at, updated_at
		 FROM search_jobs WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list search jobs: %w", err)
	}
	defer rows.Close()

	var jobs []SearchJob
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, total, rows.Err()
}

// SetExtractedParams stores DeepSeek's structured params.
func (r *Repository) SetExtractedParams(ctx context.Context, id string, params ExtractedParams) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE search_jobs SET extracted_params = $2, updated_at = now() WHERE id = $1`,
		id, raw,
	)
	return err
}

// SetStatus updates the status and progress of a job.
func (r *Repository) SetStatus(ctx context.Context, id, status string, progress int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE search_jobs SET status = $2, progress = $3, updated_at = now() WHERE id = $1`,
		id, status, progress,
	)
	return err
}

// SetCompleted marks a job completed and stores the final result list.
func (r *Repository) SetCompleted(ctx context.Context, id string, result []FilteredJob) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE search_jobs
		 SET status = $2, progress = 100, result = $3, updated_at = now()
		 WHERE id = $1`,
		id, StatusCompleted, raw,
	)
	return err
}

// SetFailed marks a job failed with an error message.
func (r *Repository) SetFailed(ctx context.Context, id, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE search_jobs SET status = $2, error = $3, updated_at = now() WHERE id = $1`,
		id, StatusFailed, errMsg,
	)
	return err
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*SearchJob, error) {
	var (
		job        SearchJob
		paramsRaw  []byte
		resultRaw  []byte
	)
	err := row.Scan(
		&job.ID, &job.UserID, &job.Query, &job.Status, &job.Progress,
		&paramsRaw, &resultRaw, &job.Error, &job.CreatedAt, &job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan search job: %w", err)
	}

	if len(paramsRaw) > 0 {
		var p ExtractedParams
		if err := json.Unmarshal(paramsRaw, &p); err == nil {
			job.ExtractedParams = &p
		}
	}
	if len(resultRaw) > 0 {
		_ = json.Unmarshal(resultRaw, &job.Result)
	}
	return &job, nil
}
