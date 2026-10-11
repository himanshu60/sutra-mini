// Package store holds the SQL. Rules that must survive concurrent requests are enforced
// here, inside transactions and database constraints, not in Go memory.
package store

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound means the referenced row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrKeyReused means an Idempotency-Key was replayed with a different request body.
	ErrKeyReused = errors.New("idempotency key already used with a different request")
)

// OvershootError means a receipt would make total received exceed the issued quantity.
type OvershootError struct {
	Outstanding int
}

func (e *OvershootError) Error() string {
	return fmt.Sprintf("receipt exceeds outstanding quantity (%d outstanding)", e.Outstanding)
}

// Store runs queries against one connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// pgCode returns the SQLSTATE of a Postgres error, or "" for any other error.
func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
