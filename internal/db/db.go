// Package db opens the Postgres pool and applies the embedded migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/himanshu60/sutra-mini/migrations"
)

// Connect opens a connection pool and checks that the database answers.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New("database URL is empty")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies every embedded *.sql file not yet recorded in schema_migrations,
// in filename order, each in its own transaction. It returns the files it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".") // sorted by filename
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var applied []string
	for _, e := range entries {
		if path.Ext(e.Name()) != ".sql" {
			continue
		}
		ok, err := applyMigration(ctx, pool, e.Name())
		if err != nil {
			return applied, err
		}
		if ok {
			applied = append(applied, e.Name())
		}
	}
	return applied, nil
}

// applyMigration records the version first, so a second migrator running at the same
// time blocks on the primary key and then skips the file instead of applying it twice.
func applyMigration(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	sql, err := migrations.FS.ReadFile(name)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", name, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin %s: %w", name, err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, name)
	if err != nil {
		return false, fmt.Errorf("record %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	// No arguments, so pgx uses the simple protocol, which allows several statements.
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return false, fmt.Errorf("apply %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit %s: %w", name, err)
	}
	return true, nil
}
