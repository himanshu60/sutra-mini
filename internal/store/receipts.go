package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
)

// NewReceipt is a validated request to record goods coming back from a job-worker.
type NewReceipt struct {
	IssueID        int64
	Qty            int
	ReceivedOn     time.Time
	IdempotencyKey string
}

// Receipt is the result of recording a receipt. Every field is fixed once written,
// so a replay of the same Idempotency-Key returns exactly the same value.
type Receipt struct {
	ReceiptID   int64  `json:"receipt_id"`
	IssueID     int64  `json:"issue_id"`
	Qty         int    `json:"qty"`
	ReceivedOn  string `json:"received_on"`
	VoucherID   int64  `json:"voucher_id"`
	AmountPaise int64  `json:"amount_paise"`
	ClientRef   string `json:"client_ref"`
}

// CreateReceipt records a receipt and its voucher in one transaction.
//
// The order of statements is what makes it safe under concurrency:
//  1. Insert the receipt. Its unique idempotency_key claims the key; a concurrent request
//     with the same key waits on that index until this transaction commits or rolls back.
//  2. Add qty to the issue's received_qty only if the total stays within the issued qty.
//     The row lock queues concurrent receipts for one issue, and each re-checks the
//     condition against the latest committed total.
//  3. Insert the voucher. Any error rolls back all three writes.
//
// A replayed key writes nothing and returns the stored receipt with replayed = true.
func (s *Store) CreateReceipt(ctx context.Context, in NewReceipt) (rec Receipt, replayed bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var receiptID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO jobwork_receipts (issue_id, qty, received_on, idempotency_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		in.IssueID, in.Qty, in.ReceivedOn, in.IdempotencyKey).Scan(&receiptID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		rec, err := loadReplay(ctx, tx, in)
		return rec, err == nil, err
	case pgCode(err) == foreignKeyViolation:
		return Receipt{}, false, ErrNotFound
	case err != nil:
		return Receipt{}, false, fmt.Errorf("insert receipt: %w", err)
	}

	var ratePaise int64
	err = tx.QueryRow(ctx, `
		UPDATE jobwork_issues
		SET received_qty = received_qty + $2
		WHERE id = $1 AND received_qty + $2 <= qty
		RETURNING rate_paise`,
		in.IssueID, in.Qty).Scan(&ratePaise)
	// No row means the guard refused the update. A check violation means the
	// received_within_issued constraint, the database's backstop, refused it.
	if errors.Is(err, pgx.ErrNoRows) || pgCode(err) == checkViolation {
		if err := tx.Rollback(ctx); err != nil {
			return Receipt{}, false, fmt.Errorf("rollback: %w", err)
		}
		return Receipt{}, false, s.overshoot(ctx, in.IssueID)
	}
	if err != nil {
		return Receipt{}, false, fmt.Errorf("update received qty: %w", err)
	}

	rec = Receipt{
		ReceiptID:   receiptID,
		IssueID:     in.IssueID,
		Qty:         in.Qty,
		ReceivedOn:  in.ReceivedOn.Format(time.DateOnly),
		AmountPaise: int64(in.Qty) * ratePaise,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO vouchers (type, source_id, amount_paise)
		VALUES ('jobwork_receipt', $1, $2)
		RETURNING id, client_ref::text`,
		receiptID, rec.AmountPaise).Scan(&rec.VoucherID, &rec.ClientRef)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("insert voucher: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Receipt{}, false, fmt.Errorf("commit: %w", err)
	}
	return rec, false, nil
}

// loadReplay returns the receipt already stored under in.IdempotencyKey, or ErrKeyReused
// when that key was first used for a different request.
func loadReplay(ctx context.Context, tx pgx.Tx, in NewReceipt) (Receipt, error) {
	var rec Receipt
	var receivedOn time.Time
	err := tx.QueryRow(ctx, `
		SELECT r.id, r.issue_id, r.qty, r.received_on, v.id, v.amount_paise, v.client_ref::text
		FROM jobwork_receipts r
		JOIN vouchers v ON v.type = 'jobwork_receipt' AND v.source_id = r.id
		WHERE r.idempotency_key = $1`,
		in.IdempotencyKey).Scan(&rec.ReceiptID, &rec.IssueID, &rec.Qty, &receivedOn,
		&rec.VoucherID, &rec.AmountPaise, &rec.ClientRef)
	if err != nil {
		return Receipt{}, fmt.Errorf("load replayed receipt: %w", err)
	}
	rec.ReceivedOn = receivedOn.Format(time.DateOnly)

	if rec.IssueID != in.IssueID || rec.Qty != in.Qty || rec.ReceivedOn != in.ReceivedOn.Format(time.DateOnly) {
		return Receipt{}, ErrKeyReused
	}
	return rec, nil
}

// overshoot reads how much is still outstanding so the caller can be told.
func (s *Store) overshoot(ctx context.Context, issueID int64) error {
	var outstanding int
	err := s.pool.QueryRow(ctx, `SELECT qty - received_qty FROM jobwork_issues WHERE id = $1`, issueID).Scan(&outstanding)
	if err != nil {
		return fmt.Errorf("read outstanding: %w", err)
	}
	return &OvershootError{Outstanding: outstanding}
}
