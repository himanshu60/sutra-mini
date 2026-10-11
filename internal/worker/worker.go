// Package worker pushes pending vouchers to the accounting API.
//
// Delivery is at-least-once: a worker can die after the API accepted a voucher but before
// the database recorded it, and no transaction spans both systems. Every attempt for a
// voucher sends the same client_ref, so the API recognises a repeat and answers 409 with
// the original external_ref, which the worker stores. The voucher is posted once.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/himanshu60/sutra-mini/internal/store"
)

// dbTimeout bounds each database call the worker makes.
const dbTimeout = 10 * time.Second

// Config controls one worker process.
type Config struct {
	ID           string        // lease owner name; must be unique per running worker
	Concurrency  int           // vouchers pushed in parallel
	PollInterval time.Duration // wait when nothing is due
	Lease        time.Duration // must be longer than the HTTP timeout
	MaxAttempts  int           // automatic attempts before a voucher is marked failed
	BackoffBase  time.Duration // wait after the first failed attempt, doubled each time
	BackoffMax   time.Duration
}

// Worker claims, pushes and records vouchers.
type Worker struct {
	store  *store.Store
	client *Client
	cfg    Config
}

// New returns a worker.
func New(st *store.Store, client *Client, cfg Config) *Worker {
	return &Worker{store: st, client: client, cfg: cfg}
}

// Run pushes vouchers with cfg.Concurrency independent loops, so one slow push never
// holds up the others. When ctx is cancelled it stops claiming, lets pushes already in
// flight finish and record their result, then returns.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.cfg.Concurrency {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := w.ProcessOne(context.WithoutCancel(ctx))
		if err != nil {
			slog.Error("process voucher", "worker", w.cfg.ID, "err", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(w.cfg.PollInterval):
		}
	}
}

// ProcessOne claims one due voucher, pushes it and records the outcome.
// It reports false when no voucher was due.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	v, ok, err := w.store.ClaimVoucher(claimCtx, w.cfg.ID, w.cfg.Lease)
	cancel()
	if err != nil || !ok {
		return false, err
	}
	log := slog.With("worker", w.cfg.ID, "voucher", v.ID, "client_ref", v.ClientRef, "attempt", v.Attempts)

	ref, duplicate, pushErr := w.client.Push(ctx, v)

	recordCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if pushErr != nil {
		return true, w.recordFailure(recordCtx, log, v, pushErr)
	}
	// If this write is lost (crash, DB down), the lease expires, the voucher is pushed
	// again with the same client_ref, and the 409 below recovers the same reference.
	if err := w.store.MarkPushed(recordCtx, v.ID, ref); err != nil {
		return true, err
	}
	if duplicate {
		log.Info("already in accounting: 409 reconciled", "external_ref", ref)
	} else {
		log.Info("pushed", "external_ref", ref)
	}
	return true, nil
}

func (w *Worker) recordFailure(ctx context.Context, log *slog.Logger, v store.Voucher, pushErr error) error {
	var permanent *PermanentError
	if errors.As(pushErr, &permanent) || v.Attempts >= w.cfg.MaxAttempts {
		reason := fmt.Sprintf("gave up after %d attempts: %v", v.Attempts, pushErr)
		applied, err := w.store.MarkFailed(ctx, v.ID, w.cfg.ID, reason)
		if err != nil {
			return err
		}
		if applied {
			log.Warn("voucher failed", "reason", reason)
		} else {
			log.Warn("lease lost before recording failure", "err", pushErr)
		}
		return nil
	}

	delay := backoff(v.Attempts, w.cfg.BackoffBase, w.cfg.BackoffMax)
	applied, err := w.store.MarkRetry(ctx, v.ID, w.cfg.ID, pushErr.Error(), delay)
	if err != nil {
		return err
	}
	if applied {
		log.Info("attempt failed, will retry", "in", delay.Round(time.Millisecond), "err", pushErr)
	} else {
		log.Warn("lease lost before recording failure", "err", pushErr)
	}
	return nil
}

// backoff is the wait before the next attempt: base doubled for every attempt so far,
// capped at limit. Half of it is random so failed vouchers do not all retry in step.
func backoff(attempt int, base, limit time.Duration) time.Duration {
	d := limit
	if shift := attempt - 1; shift < 30 {
		if b := base << shift; b > 0 && b < limit {
			d = b
		}
	}
	return d/2 + rand.N(d/2+1)
}
