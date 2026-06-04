package cv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a CV does not exist or belongs to another user.
var ErrNotFound = errors.New("cv not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CountByUser returns how many CVs a user has (for quota enforcement).
func (r *Repository) CountByUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM cvs WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// Create inserts a CV row with a caller-generated ID.
func (r *Repository) Create(ctx context.Context, c CV) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cvs (id, user_id, filename, storage_key, file_size, mime_type, sha256, status)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		c.ID, c.UserID, c.Filename, c.StorageKey, c.FileSize, c.MimeType, c.SHA256, c.Status,
	)
	if err != nil {
		return fmt.Errorf("create cv: %w", err)
	}
	return nil
}

const fullColumns = `id::text, user_id, filename, storage_key, file_size,
	COALESCE(mime_type, ''), COALESCE(sha256, ''), status,
	COALESCE(extracted_text, ''), parsed_data, is_default, created_at, updated_at`

// Get fetches a CV scoped to a user (full row, including extracted text).
func (r *Repository) Get(ctx context.Context, id, userID string) (*CV, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+fullColumns+` FROM cvs WHERE id = $1 AND user_id = $2`, id, userID)
	return scanCV(row)
}

// GetByID fetches a CV without a user scope (worker use only).
func (r *Repository) GetByID(ctx context.Context, id string) (*CV, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+fullColumns+` FROM cvs WHERE id = $1`, id)
	return scanCV(row)
}

// GetDefault returns the user's default CV, or the most recent if none is set.
func (r *Repository) GetDefault(ctx context.Context, userID string) (*CV, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+fullColumns+` FROM cvs WHERE user_id = $1
		 ORDER BY is_default DESC, created_at DESC LIMIT 1`, userID)
	return scanCV(row)
}

// List returns a user's CVs, newest first. Lightweight: omits extracted_text.
func (r *Repository) List(ctx context.Context, userID string) ([]CV, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, user_id, filename, file_size, COALESCE(mime_type, ''),
		        COALESCE(sha256, ''), status, is_default, created_at, updated_at
		 FROM cvs WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list cvs: %w", err)
	}
	defer rows.Close()

	var out []CV
	for rows.Next() {
		var c CV
		if err := rows.Scan(&c.ID, &c.UserID, &c.Filename, &c.FileSize, &c.MimeType,
			&c.SHA256, &c.Status, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetStatus updates only the status.
func (r *Repository) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE cvs SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

// SetProcessed stores extraction output and a terminal status.
func (r *Repository) SetProcessed(ctx context.Context, id, extractedText string, parsed json.RawMessage, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE cvs SET extracted_text = $2, parsed_data = $3, status = $4, updated_at = now()
		 WHERE id = $1`,
		id, extractedText, nullableJSON(parsed), status)
	return err
}

// SetDefault makes one CV the user's default and clears the flag on the rest.
func (r *Repository) SetDefault(ctx context.Context, id, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE cvs SET is_default = true, updated_at = now() WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cvs SET is_default = false, updated_at = now()
		 WHERE user_id = $1 AND id <> $2 AND is_default`, userID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Delete removes a CV row and returns its storage key so the caller can delete
// the object.
func (r *Repository) Delete(ctx context.Context, id, userID string) (string, error) {
	var key string
	err := r.pool.QueryRow(ctx,
		`DELETE FROM cvs WHERE id = $1 AND user_id = $2 RETURNING storage_key`,
		id, userID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("delete cv: %w", err)
	}
	return key, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCV(row rowScanner) (*CV, error) {
	var (
		c         CV
		parsedRaw []byte
	)
	err := row.Scan(&c.ID, &c.UserID, &c.Filename, &c.StorageKey, &c.FileSize,
		&c.MimeType, &c.SHA256, &c.Status, &c.ExtractedText, &parsedRaw,
		&c.IsDefault, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan cv: %w", err)
	}
	if len(parsedRaw) > 0 {
		c.ParsedData = json.RawMessage(parsedRaw)
	}
	return &c, nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}
