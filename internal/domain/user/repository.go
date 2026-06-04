package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when no snapshot exists for a user id.
var ErrNotFound = errors.New("user snapshot not found")

// Repository persists user snapshots in Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a Repository over the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Upsert inserts the snapshot or updates every field on id conflict — the
// equivalent of chat-service's GORM OnConflict{UpdateAll: true}.
func (r *Repository) Upsert(ctx context.Context, s *Snapshot) error {
	var ts *time.Time
	if !s.UpdatedAt.IsZero() {
		ts = &s.UpdatedAt
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_snapshots (id, email, fullname, username, avatar, role, updated_at)
		VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF($6, ''), 'user'), COALESCE($7, now()))
		ON CONFLICT (id) DO UPDATE SET
			email      = EXCLUDED.email,
			fullname   = EXCLUDED.fullname,
			username   = EXCLUDED.username,
			avatar     = EXCLUDED.avatar,
			role       = EXCLUDED.role,
			updated_at = EXCLUDED.updated_at`,
		s.ID, s.Email, s.Fullname, s.Username, s.Avatar, s.Role, ts)
	if err != nil {
		return fmt.Errorf("upsert user snapshot %s: %w", s.ID, err)
	}
	return nil
}

// Get returns the snapshot for a user id, or ErrNotFound.
func (r *Repository) Get(ctx context.Context, id string) (*Snapshot, error) {
	var s Snapshot
	err := r.pool.QueryRow(ctx, `
		SELECT id, email, fullname, username, avatar, role, updated_at
		FROM user_snapshots WHERE id = $1`, id,
	).Scan(&s.ID, &s.Email, &s.Fullname, &s.Username, &s.Avatar, &s.Role, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user snapshot %s: %w", id, err)
	}
	return &s, nil
}
