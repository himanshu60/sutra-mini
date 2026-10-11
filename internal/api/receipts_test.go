package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/himanshu60/sutra-mini/internal/api"
	"github.com/himanshu60/sutra-mini/internal/store"
	"github.com/himanshu60/sutra-mini/internal/testdb"
)

// Test A: many receipts for one issue arrive at the same moment; together they ask for more
// than was issued. The database must accept only what fits and leave no trace of the rest.
func TestConcurrentReceiptsNeverOvershoot(t *testing.T) {
	pool, url := setup(t)
	issueID := createIssue(t, pool, 10)

	const requests, qtyEach = 10, 3 // 30 asked for, 10 issued: exactly 3 can succeed
	results := fireTogether(requests, func(i int) (response, error) {
		return postReceipt(url, fmt.Sprintf("overshoot-%d", i), issueID, qtyEach)
	})

	created, conflicts := 0, 0
	for _, r := range results {
		switch r.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("unexpected response %d: %s", r.status, r.body)
		}
	}
	if created != 3 || conflicts != 7 {
		t.Errorf("got %d created and %d conflicts, want 3 and 7", created, conflicts)
	}

	issued := count(t, pool, `SELECT qty FROM jobwork_issues WHERE id = $1`, issueID)
	received := count(t, pool, `SELECT received_qty FROM jobwork_issues WHERE id = $1`, issueID)
	sumReceipts := count(t, pool, `SELECT COALESCE(SUM(qty), 0) FROM jobwork_receipts WHERE issue_id = $1`, issueID)
	receipts := count(t, pool, `SELECT count(*) FROM jobwork_receipts WHERE issue_id = $1`, issueID)
	vouchers := count(t, pool, `
		SELECT count(*) FROM vouchers v JOIN jobwork_receipts r ON v.type = 'jobwork_receipt' AND v.source_id = r.id
		WHERE r.issue_id = $1`, issueID)

	if sumReceipts > issued {
		t.Fatalf("OVERSHOOT: %d received against %d issued", sumReceipts, issued)
	}
	if sumReceipts != created*qtyEach || received != sumReceipts {
		t.Errorf("received_qty=%d, sum of receipts=%d, want both %d", received, sumReceipts, created*qtyEach)
	}
	if receipts != created || vouchers != created {
		t.Errorf("%d receipts and %d vouchers for %d successful requests (rejected ones must write nothing)", receipts, vouchers, created)
	}
	if n := count(t, pool, `SELECT count(*) FROM vouchers`); n != created {
		t.Errorf("%d vouchers in total, want %d", n, created)
	}
}

// Test B: replaying an Idempotency-Key returns the original result and writes nothing.
func TestReplayedKeyCreatesNothing(t *testing.T) {
	pool, url := setup(t)
	issueID := createIssue(t, pool, 10)

	first, err := postReceipt(url, "replay-key", issueID, 4)
	if err != nil || first.status != http.StatusCreated {
		t.Fatalf("first request: %v %d %s", err, first.status, first.body)
	}
	second, err := postReceipt(url, "replay-key", issueID, 4)
	if err != nil || second.status != http.StatusCreated {
		t.Fatalf("replay: %v %d %s", err, second.status, second.body)
	}
	if second.body != first.body {
		t.Errorf("replay returned a different result:\n first: %s\nreplay: %s", first.body, second.body)
	}
	if !second.replayed || first.replayed {
		t.Errorf("Idempotent-Replayed header: first=%v replay=%v, want false then true", first.replayed, second.replayed)
	}
	assertOneReceipt(t, pool, issueID, 4)
}

// Same key sent by many clients at once: still one receipt, and everyone gets the same answer.
func TestConcurrentSameKeyCreatesOneReceipt(t *testing.T) {
	pool, url := setup(t)
	issueID := createIssue(t, pool, 10)

	results := fireTogether(10, func(int) (response, error) {
		return postReceipt(url, "same-key", issueID, 4)
	})
	for _, r := range results {
		if r.status != http.StatusCreated || r.body != results[0].body {
			t.Errorf("got %d %s, want 201 %s", r.status, r.body, results[0].body)
		}
	}
	assertOneReceipt(t, pool, issueID, 4)
}

// A key reused for a different request is refused, not silently replayed.
func TestReusedKeyWithDifferentBodyIsRejected(t *testing.T) {
	pool, url := setup(t)
	issueID := createIssue(t, pool, 10)

	if r, err := postReceipt(url, "reused-key", issueID, 4); err != nil || r.status != http.StatusCreated {
		t.Fatalf("first request: %v %d %s", err, r.status, r.body)
	}
	r, err := postReceipt(url, "reused-key", issueID, 5)
	if err != nil || r.status != http.StatusUnprocessableEntity {
		t.Fatalf("reused key: got %v %d %s, want 422", err, r.status, r.body)
	}
	assertOneReceipt(t, pool, issueID, 4)
}

type response struct {
	status   int
	body     string
	replayed bool
}

// setup returns an isolated database and an API server running against it.
func setup(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	pool := testdb.New(t)
	srv := httptest.NewServer(api.NewHandler(store.New(pool)))
	t.Cleanup(srv.Close)
	return pool, srv.URL
}

// createIssue adds an issue of qty pieces for the seeded party 1 and SKU 1.
func createIssue(t *testing.T, pool *pgxpool.Pool, qty int) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO jobwork_issues (party_id, sku_id, qty, rate_paise, issued_on)
		VALUES (1, 1, $1, 2500, '2026-10-01') RETURNING id`, qty).Scan(&id)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

func postReceipt(baseURL, key string, issueID int64, qty int) (response, error) {
	body := fmt.Sprintf(`{"issue_id":%d,"qty":%d,"received_on":"2026-10-11"}`, issueID, qty)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/jobwork/receipts", strings.NewReader(body))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, err
	}
	return response{status: res.StatusCode, body: string(b), replayed: res.Header.Get("Idempotent-Replayed") == "true"}, nil
}

// fireTogether starts n calls that all wait on one barrier, releases them at once,
// and returns their results in call order. A call that errors is recorded as status 0.
func fireTogether(n int, call func(i int) (response, error)) []response {
	results := make([]response, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			r, err := call(i)
			if err != nil {
				r = response{body: err.Error()}
			}
			results[i] = r
		})
	}
	close(start)
	wg.Wait()
	return results
}

func assertOneReceipt(t *testing.T, pool *pgxpool.Pool, issueID int64, qty int) {
	t.Helper()
	if n := count(t, pool, `SELECT count(*) FROM jobwork_receipts`); n != 1 {
		t.Errorf("%d receipts, want 1", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM vouchers`); n != 1 {
		t.Errorf("%d vouchers, want 1", n)
	}
	if n := count(t, pool, `SELECT received_qty FROM jobwork_issues WHERE id = $1`, issueID); n != qty {
		t.Errorf("received_qty = %d, want %d", n, qty)
	}
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}
