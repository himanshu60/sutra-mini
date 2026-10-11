package mockacct

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A duplicate client_ref must get 409 with the original external_ref even when the
// random roll for that request would have been a failure.
func TestDuplicateIsCheckedBeforeRandomFailure(t *testing.T) {
	rolls := []Outcome{OK, Fail}
	srv := New(Config{Decide: func() Outcome {
		o := rolls[0]
		rolls = rolls[1:]
		return o
	}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	status, first := post(t, ts.URL, "ref-1")
	if status != http.StatusCreated || first == "" {
		t.Fatalf("first post: got %d %q, want 201 with an external_ref", status, first)
	}
	status, again := post(t, ts.URL, "ref-1") // Fail is next in line, but must not be used
	if status != http.StatusConflict || again != first {
		t.Fatalf("duplicate post: got %d %q, want 409 %q", status, again, first)
	}
	if status, _ := post(t, ts.URL, "ref-2"); status != http.StatusInternalServerError {
		t.Fatalf("new ref after duplicate: got %d, want the queued 500", status)
	}
	if recorded, dups := srv.Stats(); recorded != 1 || dups != 1 {
		t.Fatalf("stats: %d recorded, %d duplicates; want 1 and 1", recorded, dups)
	}
}

// Many requests with one client_ref at once: exactly one is recorded, all see the same ref.
func TestConcurrentSameClientRefRecordedOnce(t *testing.T) {
	srv := New(Config{Decide: func() Outcome { return OK }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const n = 20
	statuses, refs := make([]int, n), make([]string, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			statuses[i], refs[i] = post(t, ts.URL, "same-ref")
		})
	}
	close(start)
	wg.Wait()

	created := 0
	for i := range n {
		if statuses[i] == http.StatusCreated {
			created++
		}
		if refs[i] != refs[0] {
			t.Errorf("request %d got external_ref %q, want %q", i, refs[i], refs[0])
		}
	}
	if recorded, _ := srv.Stats(); created != 1 || recorded != 1 {
		t.Fatalf("%d created, %d recorded; want 1 and 1", created, recorded)
	}
}

func post(t *testing.T, url, clientRef string) (int, string) {
	t.Helper()
	res, err := http.Post(url+"/api/vouchers", "application/json",
		strings.NewReader(`{"client_ref":"`+clientRef+`","amount_paise":100}`))
	if err != nil {
		t.Errorf("post: %v", err)
		return 0, ""
	}
	defer res.Body.Close()
	var body struct {
		ExternalRef string `json:"external_ref"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Errorf("decode: %v", err)
	}
	return res.StatusCode, body.ExternalRef
}
