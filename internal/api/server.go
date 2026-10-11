// Package api is the HTTP layer: handlers decode and validate input, call the store,
// and turn store errors into status codes.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/himanshu60/sutra-mini/internal/store"
)

// requestTimeout bounds every request's database work.
const requestTimeout = 10 * time.Second

type server struct {
	store *store.Store
}

// NewHandler returns the routes of the Sutra API.
func NewHandler(st *store.Store) http.Handler {
	s := &server{store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobwork/receipts", s.createReceipt)
	mux.HandleFunc("GET /issues/{id}", s.getIssue)
	mux.HandleFunc("GET /vouchers", s.listVouchers)
	mux.HandleFunc("POST /vouchers/{id}/retry", s.retryVoucher)
	return withTimeout(mux, requestTimeout)
}

func withTimeout(h http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs the real cause and returns a generic 500.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// pathID reads a positive integer path parameter.
func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New(name + " must be a positive integer")
	}
	return id, nil
}
