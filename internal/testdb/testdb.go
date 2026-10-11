// Package testdb gives each integration test its own freshly migrated schema
// on TEST_DATABASE_URL, dropped again when the test ends.
package testdb

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/himanshu60/sutra-mini/internal/db"
)

// New returns a pool whose search_path is a new schema holding the full migrated database.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := testURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	schema := "test_" + strings.ToLower(rand.Text())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 20 // enough for every concurrent request in a test to hold its own transaction
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close) // runs before the schema is dropped

	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return pool
}

func testURL(t *testing.T) string {
	t.Helper()
	loadRepoDotEnv(t)
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		// Fail rather than skip: a skipped package still prints "ok", which reads as a pass.
		t.Fatal("TEST_DATABASE_URL is not set: these tests need a throwaway Postgres (see README)")
	}
	return url
}

// loadRepoDotEnv loads .env from the repo root, so `go test ./...` works without exporting variables.
func loadRepoDotEnv(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
	if err := godotenv.Load(filepath.Join(dir, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("load .env: %v", err)
	}
}
