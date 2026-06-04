package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a pgx connection pool and verifies connectivity. When schema is
// non-empty, every connection ensures that schema exists and pins it as the
// search_path, so all queries and migrations are isolated within it.
func New(ctx context.Context, databaseURL, schema string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute

	if schema != "" {
		ident := quoteIdent(schema)
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+ident); err != nil {
				return fmt.Errorf("ensure schema %s: %w", schema, err)
			}
			if _, err := conn.Exec(ctx, "SET search_path TO "+ident+", public"); err != nil {
				return fmt.Errorf("set search_path %s: %w", schema, err)
			}
			return nil
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// quoteIdent double-quotes a SQL identifier so names with hyphens or other
// special characters (e.g. "thoth-ai") are treated literally and safely.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
