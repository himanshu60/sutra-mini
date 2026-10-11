package worker_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/himanshu60/sutra-mini/internal/mockacct"
	"github.com/himanshu60/sutra-mini/internal/store"
	"github.com/himanshu60/sutra-mini/internal/testdb"
	"github.com/himanshu60/sutra-mini/internal/worker"
)

// The worker died after accounting accepted the voucher but before the database heard
// about it. The next worker must recover the original reference through the 409, not post twice.
func TestCrashAfterPushIsReconciledThrough409(t *testing.T) {
	e := setup(t, always(mockacct.OK), 0)
	v := e.voucher(e.addVouchers(1)[0])

	ref, _, err := worker.NewClient(e.url, time.Second).Push(context.Background(), v) // the dead worker's push
	if err != nil {
		t.Fatalf("seed push: %v", err)
	}
	e.exec(`UPDATE vouchers SET locked_by = 'dead-worker', locked_until = now() - interval '1 second', attempts = 1 WHERE id = $1`, v.ID)

	e.processOne(e.config("w1"), time.Second)

	got := e.voucher(v.ID)
	if got.Status != store.StatusPushed || got.ExternalRef == nil || *got.ExternalRef != ref {
		t.Fatalf("got status %s external_ref %v, want pushed %s", got.Status, got.ExternalRef, ref)
	}
	if recorded, dups := e.mock.Stats(); recorded != 1 || dups != 1 {
		t.Fatalf("accounting has %d entries and refused %d duplicates, want 1 and 1", recorded, dups)
	}
}

// A voucher leased to a live worker is left alone.
func TestLiveLeaseIsNotClaimed(t *testing.T) {
	e := setup(t, always(mockacct.OK), 0)
	id := e.addVouchers(1)[0]
	e.exec(`UPDATE vouchers SET locked_by = 'other-worker', locked_until = now() + interval '1 minute' WHERE id = $1`, id)

	worked, err := e.newWorker(e.config("w1"), time.Second).ProcessOne(context.Background())
	if err != nil || worked {
		t.Fatalf("ProcessOne = %v, %v; want false, nil", worked, err)
	}
	if recorded, _ := e.mock.Stats(); recorded != 0 {
		t.Fatalf("leased voucher was pushed")
	}
}

// A 500 schedules a retry after a backoff; the retry then succeeds.
func TestServerErrorIsRetriedAfterBackoff(t *testing.T) {
	e := setup(t, sequence(mockacct.Fail), 0)
	id := e.addVouchers(1)[0]
	cfg := e.config("w1")
	cfg.BackoffBase, cfg.BackoffMax = time.Minute, time.Minute

	e.processOne(cfg, time.Second)
	v := e.voucher(id)
	if v.Status != store.StatusPending || v.Attempts != 1 || v.LastError == nil || !strings.Contains(*v.LastError, "500") || v.LockedBy != nil {
		t.Fatalf("after 500: %+v", v)
	}
	if wait := time.Until(v.NextAttemptAt); wait < 25*time.Second {
		t.Fatalf("next attempt in %v, want a backoff of at least 30s", wait)
	}
	if worked, _ := e.newWorker(cfg, time.Second).ProcessOne(context.Background()); worked {
		t.Fatal("voucher was claimed again before its backoff ended")
	}

	e.exec(`UPDATE vouchers SET next_attempt_at = now() WHERE id = $1`, id) // skip the wait
	e.processOne(cfg, time.Second)
	if v := e.voucher(id); v.Status != store.StatusPushed || v.Attempts != 2 || v.LastError != nil {
		t.Fatalf("after retry: %+v", v)
	}
}

// A voucher that keeps failing ends failed with a reason; a manual retry pushes the same voucher.
func TestFailsAfterMaxAttemptsThenManualRetry(t *testing.T) {
	var outcome atomic.Int32 // mockacct.Fail until switched
	outcome.Store(int32(mockacct.Fail))
	e := setup(t, func() mockacct.Outcome { return mockacct.Outcome(outcome.Load()) }, 0)
	id := e.addVouchers(1)[0]
	cfg := e.config("w1")
	cfg.MaxAttempts = 3

	for range 3 {
		e.processOne(cfg, time.Second)
	}
	v := e.voucher(id)
	if v.Status != store.StatusFailed || v.LastError == nil || !strings.HasPrefix(*v.LastError, "gave up after 3 attempts") {
		t.Fatalf("after 3 failures: status %s, last_error %v", v.Status, v.LastError)
	}
	if worked, _ := e.newWorker(cfg, time.Second).ProcessOne(context.Background()); worked {
		t.Fatal("failed voucher was claimed automatically")
	}

	if _, err := e.store.RetryVoucher(context.Background(), id); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := e.store.RetryVoucher(context.Background(), id); !errors.Is(err, store.ErrNotRetryable) {
		t.Fatalf("second retry of a pending voucher: got %v, want ErrNotRetryable", err)
	}
	outcome.Store(int32(mockacct.OK))
	e.processOne(cfg, time.Second)

	if v := e.voucher(id); v.Status != store.StatusPushed || v.Attempts != 1 {
		t.Fatalf("after manual retry: status %s, attempts %d; want pushed, 1", v.Status, v.Attempts)
	}
	if n := e.count(`SELECT count(*) FROM vouchers`); n != 1 {
		t.Fatalf("%d vouchers, want 1: a retry must not create a voucher", n)
	}
}

// Two workers drain the same queue through failures and slow answers. Each voucher reaches
// accounting once and no worker ever sends a voucher another worker holds (no 409s at all).
func TestTwoWorkersPushEachVoucherOnce(t *testing.T) {
	var calls atomic.Int64
	e := setup(t, func() mockacct.Outcome {
		switch n := calls.Add(1); {
		case n%5 == 0:
			return mockacct.Fail
		case n%7 == 0:
			return mockacct.Hang
		}
		return mockacct.OK
	}, 50*time.Millisecond)
	const n = 30
	e.addVouchers(n)

	stop := e.run(e.newWorker(e.config("w1"), 2*time.Second), e.newWorker(e.config("w2"), 2*time.Second))
	e.waitFor(15*time.Second, func() bool { return e.count(`SELECT count(*) FROM vouchers WHERE status = 'pushed'`) == n })
	stop()

	if recorded, dups := e.mock.Stats(); recorded != n || dups != 0 {
		t.Fatalf("accounting has %d entries and refused %d duplicates, want %d and 0", recorded, dups, n)
	}
	if got := e.count(`SELECT count(DISTINCT external_ref) FROM vouchers`); got != n {
		t.Fatalf("%d distinct external refs, want %d", got, n)
	}
}

// One push hanging does not stop the others.
func TestSlowPushDoesNotBlockOthers(t *testing.T) {
	e := setup(t, sequence(mockacct.Hang), 2*time.Second)
	e.addVouchers(5)

	stop := e.run(e.newWorker(e.config("w1"), 5*time.Second))
	defer stop()
	start := time.Now()
	e.waitFor(1500*time.Millisecond, func() bool { return e.count(`SELECT count(*) FROM vouchers WHERE status = 'pushed'`) == 4 })
	if hanging := e.count(`SELECT count(*) FROM vouchers WHERE status = 'pending' AND locked_by IS NOT NULL`); hanging != 1 {
		t.Fatalf("%d vouchers in flight, want the 1 hanging", hanging)
	}
	t.Logf("4 vouchers pushed in %v while one hung", time.Since(start).Round(time.Millisecond))
	e.waitFor(5*time.Second, func() bool { return e.count(`SELECT count(*) FROM vouchers WHERE status = 'pushed'`) == 5 })
}

// The client gives up before a slow answer arrives, but accounting did record the voucher.
// The retry gets a 409 and stores the original reference.
func TestTimeoutThenReconciled(t *testing.T) {
	e := setup(t, sequence(mockacct.Hang), time.Second)
	id := e.addVouchers(1)[0]
	cfg := e.config("w1")

	e.processOne(cfg, 200*time.Millisecond)
	v := e.voucher(id)
	if v.Status != store.StatusPending || v.LastError == nil || !strings.Contains(*v.LastError, "Timeout") {
		t.Fatalf("after timeout: status %s, last_error %v", v.Status, v.LastError)
	}

	e.processOne(cfg, 200*time.Millisecond)
	v = e.voucher(id)
	want, recorded := e.mock.Ref(v.ClientRef)
	if !recorded || v.Status != store.StatusPushed || v.ExternalRef == nil || *v.ExternalRef != want {
		t.Fatalf("after retry: status %s, external_ref %v; want pushed %q", v.Status, v.ExternalRef, want)
	}
	if recorded, dups := e.mock.Stats(); recorded != 1 || dups != 1 {
		t.Fatalf("accounting has %d entries and refused %d duplicates, want 1 and 1", recorded, dups)
	}
}

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	store *store.Store
	mock  *mockacct.Server
	url   string
}

func setup(t *testing.T, decide func() mockacct.Outcome, hang time.Duration) *env {
	t.Helper()
	pool := testdb.New(t)
	mock := mockacct.New(mockacct.Config{Decide: decide, HangDuration: hang})
	ts := httptest.NewServer(mock.Handler())
	t.Cleanup(ts.Close)
	return &env{t: t, pool: pool, store: store.New(pool), mock: mock, url: ts.URL}
}

// config has no backoff, so a failed voucher is due again at once.
func (e *env) config(id string) worker.Config {
	return worker.Config{ID: id, Concurrency: 4, PollInterval: 10 * time.Millisecond, Lease: 15 * time.Second, MaxAttempts: 10}
}

func (e *env) newWorker(cfg worker.Config, httpTimeout time.Duration) *worker.Worker {
	return worker.New(e.store, worker.NewClient(e.url, httpTimeout), cfg)
}

// processOne runs one claim-push-record cycle and fails the test if nothing was claimed.
func (e *env) processOne(cfg worker.Config, httpTimeout time.Duration) {
	e.t.Helper()
	worked, err := e.newWorker(cfg, httpTimeout).ProcessOne(context.Background())
	if err != nil || !worked {
		e.t.Fatalf("ProcessOne = %v, %v; want true, nil", worked, err)
	}
}

// run starts the workers and returns a function that stops them and waits for them to finish.
func (e *env) run(workers ...*worker.Worker) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Go(func() { w.Run(ctx) })
	}
	return func() { cancel(); wg.Wait() }
}

func (e *env) addVouchers(n int) []int64 {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		INSERT INTO vouchers (type, source_id, amount_paise)
		SELECT 'jobwork_receipt', g, 1000 FROM generate_series(1, $1) g
		RETURNING id`, n)
	if err != nil {
		e.t.Fatalf("add vouchers: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			e.t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		e.t.Fatalf("add vouchers: %v", err)
	}
	return ids
}

func (e *env) voucher(id int64) store.Voucher {
	e.t.Helper()
	all, err := e.store.ListVouchers(context.Background(), "")
	if err != nil {
		e.t.Fatalf("list vouchers: %v", err)
	}
	for _, v := range all {
		if v.ID == id {
			return v
		}
	}
	e.t.Fatalf("voucher %d not found", id)
	return store.Voucher{}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (e *env) count(sql string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

func (e *env) waitFor(timeout time.Duration, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("condition not met within %v", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func always(o mockacct.Outcome) func() mockacct.Outcome {
	return func() mockacct.Outcome { return o }
}

// sequence returns the given outcomes in order, then OK forever.
// The mock calls it under its own lock, so it needs none.
func sequence(outcomes ...mockacct.Outcome) func() mockacct.Outcome {
	return func() mockacct.Outcome {
		if len(outcomes) == 0 {
			return mockacct.OK
		}
		o := outcomes[0]
		outcomes = outcomes[1:]
		return o
	}
}
