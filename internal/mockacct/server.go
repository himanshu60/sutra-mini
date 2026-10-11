// Package mockacct is a deliberately unreliable stand-in for the accounting API.
// It keeps vouchers in memory only, so restarting it forgets every client_ref it has seen.
package mockacct

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// Outcome is what the mock does with a voucher it has not seen before.
type Outcome int

const (
	OK   Outcome = iota // record it and answer 201
	Fail                // answer 500 and record nothing
	Hang                // record it, wait HangDuration, then answer 201
)

// Config sets how the mock misbehaves.
type Config struct {
	FailRate     float64 // share of new vouchers answered with 500
	HangRate     float64 // share of new vouchers answered only after HangDuration
	HangDuration time.Duration
	Decide       func() Outcome // replaces the random roll when set; tests use it
}

// Server holds the vouchers the mock has accepted.
type Server struct {
	cfg        Config
	mu         sync.Mutex
	refs       map[string]string // client_ref -> external_ref
	duplicates int
}

// New returns an empty mock.
func New(cfg Config) *Server {
	return &Server{cfg: cfg, refs: make(map[string]string)}
}

// Handler serves POST /api/vouchers.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/vouchers", s.createVoucher)
	return mux
}

// Stats reports how many vouchers are recorded and how many duplicates were refused.
func (s *Server) Stats() (recorded, duplicates int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refs), s.duplicates
}

type voucherRequest struct {
	ClientRef   string `json:"client_ref"`
	AmountPaise int64  `json:"amount_paise"`
}

func (s *Server) createVoucher(w http.ResponseWriter, r *http.Request) {
	var req voucherRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.ClientRef == "" || req.AmountPaise < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_ref is required and amount_paise must not be negative"})
		return
	}

	ref, duplicate, outcome := s.accept(req.ClientRef)
	switch {
	case duplicate:
		slog.Info("duplicate refused", "client_ref", req.ClientRef, "external_ref", ref)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate client_ref", "external_ref": ref})
	case outcome == Fail:
		slog.Info("simulated 500", "client_ref", req.ClientRef)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "simulated failure"})
	default:
		if outcome == Hang {
			// Already recorded: like a real server, it has committed even if the caller gives up.
			slog.Info("recorded, hanging", "client_ref", req.ClientRef, "external_ref", ref, "for", s.cfg.HangDuration)
			time.Sleep(s.cfg.HangDuration)
		}
		slog.Info("created", "client_ref", req.ClientRef, "external_ref", ref, "amount_paise", req.AmountPaise)
		writeJSON(w, http.StatusCreated, map[string]string{"external_ref": ref})
	}
}

// accept decides one request under the lock, so two requests with the same client_ref can
// never both be recorded. Duplicates are checked before the random failure is rolled.
func (s *Server) accept(clientRef string) (externalRef string, duplicate bool, outcome Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref, ok := s.refs[clientRef]; ok {
		s.duplicates++
		return ref, true, OK
	}
	outcome = s.decide()
	if outcome == Fail {
		return "", false, Fail
	}
	ref := fmt.Sprintf("ACC-%06d", len(s.refs)+1)
	s.refs[clientRef] = ref
	return ref, false, outcome
}

func (s *Server) decide() Outcome {
	if s.cfg.Decide != nil {
		return s.cfg.Decide()
	}
	roll := rand.Float64()
	switch {
	case roll < s.cfg.FailRate:
		return Fail
	case roll < s.cfg.FailRate+s.cfg.HangRate:
		return Hang
	default:
		return OK
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}
