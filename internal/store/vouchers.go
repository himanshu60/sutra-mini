package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Voucher statuses.
const (
	StatusPending = "pending"
	StatusPushed  = "pushed"
	StatusFailed  = "failed"
)

// ErrNotRetryable means a manual retry was asked for a voucher that has not failed.
var ErrNotRetryable = errors.New("only failed vouchers can be retried")

// Voucher is the accounting consequence of a receipt, and its delivery state.
type Voucher struct {
	ID            int64      `db:"id" json:"id"`
	Type          string     `db:"type" json:"type"`
	SourceID      int64      `db:"source_id" json:"source_id"`
	AmountPaise   int64      `db:"amount_paise" json:"amount_paise"`
	Status        string     `db:"status" json:"status"`
	Attempts      int        `db:"attempts" json:"attempts"`
	LastError     *string    `db:"last_error" json:"last_error"`
	ClientRef     string     `db:"client_ref" json:"client_ref"`
	ExternalRef   *string    `db:"external_ref" json:"external_ref"`
	NextAttemptAt time.Time  `db:"next_attempt_at" json:"next_attempt_at"`
	LockedBy      *string    `db:"locked_by" json:"locked_by"`
	LockedUntil   *time.Time `db:"locked_until" json:"locked_until"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

const voucherColumns = `id, type, source_id, amount_paise, status, attempts, last_error, client_ref::text AS client_ref,
	external_ref, next_attempt_at, locked_by, locked_until, created_at, updated_at`

// ValidStatus reports whether s is a voucher status.
func ValidStatus(s string) bool {
	return s == StatusPending || s == StatusPushed || s == StatusFailed
}

// ListVouchers returns the newest vouchers, all of them or only those with status.
func (s *Store) ListVouchers(ctx context.Context, status string) ([]Voucher, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+voucherColumns+` FROM vouchers
		WHERE $1 = '' OR status = $1
		ORDER BY id DESC
		LIMIT 500`, status)
	if err != nil {
		return nil, fmt.Errorf("list vouchers: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Voucher])
}

// RetryVoucher sends a failed voucher back to the worker for a fresh round of attempts.
// It never creates a voucher: the same row, with the same client_ref, is pushed again.
func (s *Store) RetryVoucher(ctx context.Context, id int64) (Voucher, error) {
	v, err := s.oneVoucher(ctx, `
		UPDATE vouchers
		SET status = 'pending', attempts = 0, next_attempt_at = now(),
		    locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND status = 'failed'
		RETURNING `+voucherColumns, id)
	if !errors.Is(err, pgx.ErrNoRows) {
		return v, err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM vouchers WHERE id = $1)`, id).Scan(&exists); err != nil {
		return Voucher{}, fmt.Errorf("check voucher %d: %w", id, err)
	}
	if !exists {
		return Voucher{}, ErrNotFound
	}
	return Voucher{}, ErrNotRetryable
}

// ClaimVoucher leases the next due voucher to worker. ok is false when none is due.
//
// SKIP LOCKED keeps two workers claiming at the same moment off the same row. The lease
// (locked_until) is what keeps them off it afterwards, while the push is in flight;
// once it expires, for example because the worker died, the voucher can be claimed again.
// attempts counts claims, so a worker that crashes mid-push still uses up an attempt.
func (s *Store) ClaimVoucher(ctx context.Context, worker string, lease time.Duration) (v Voucher, ok bool, err error) {
	v, err = s.oneVoucher(ctx, `
		UPDATE vouchers
		SET locked_by = $1, locked_until = now() + make_interval(secs => $2),
		    attempts = attempts + 1, updated_at = now()
		WHERE id = (
			SELECT id FROM vouchers
			WHERE status = 'pending' AND next_attempt_at <= now()
			  AND (locked_until IS NULL OR locked_until < now())
			ORDER BY next_attempt_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+voucherColumns, worker, lease.Seconds())
	if errors.Is(err, pgx.ErrNoRows) {
		return Voucher{}, false, nil
	}
	if err != nil {
		return Voucher{}, false, err
	}
	return v, true, nil
}

// MarkPushed records the accounting system's reference. It is not fenced by the lease:
// a confirmed external_ref is the truth whichever worker learned it.
func (s *Store) MarkPushed(ctx context.Context, id int64, externalRef string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE vouchers
		SET status = 'pushed', external_ref = $2, last_error = NULL,
		    locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND status <> 'pushed'`, id, externalRef)
	if err != nil {
		return fmt.Errorf("mark voucher %d pushed: %w", id, err)
	}
	return nil
}

// MarkRetry releases a claimed voucher to be tried again after delay.
// It reports false if worker no longer holds the lease, in which case nothing changes.
func (s *Store) MarkRetry(ctx context.Context, id int64, worker, lastErr string, delay time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE vouchers
		SET last_error = $3, next_attempt_at = now() + make_interval(secs => $4),
		    locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'pending'`, id, worker, lastErr, delay.Seconds())
	if err != nil {
		return false, fmt.Errorf("schedule retry of voucher %d: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

// MarkFailed stops automatic attempts until someone retries the voucher by hand.
// It reports false if worker no longer holds the lease, in which case nothing changes.
func (s *Store) MarkFailed(ctx context.Context, id int64, worker, reason string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE vouchers
		SET status = 'failed', last_error = $3,
		    locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'pending'`, id, worker, reason)
	if err != nil {
		return false, fmt.Errorf("mark voucher %d failed: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) oneVoucher(ctx context.Context, sql string, args ...any) (Voucher, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return Voucher{}, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Voucher])
}
