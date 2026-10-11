package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/himanshu60/sutra-mini/internal/store"
)

const maxKeyLen = 255

type receiptRequest struct {
	IssueID    int64  `json:"issue_id"`
	Qty        int32  `json:"qty"`
	ReceivedOn string `json:"received_on"`
}

// createReceipt handles POST /jobwork/receipts.
func (s *server) createReceipt(w http.ResponseWriter, r *http.Request) {
	in, err := parseReceipt(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Echo the key so a caller that did not send one can still replay safely.
	w.Header().Set("Idempotency-Key", in.IdempotencyKey)

	rec, replayed, err := s.store.CreateReceipt(r.Context(), in)
	var over *store.OvershootError
	switch {
	case errors.As(err, &over):
		writeJSON(w, http.StatusConflict, map[string]any{"error": over.Error(), "outstanding": over.Outstanding})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "issue not found")
	case errors.Is(err, store.ErrKeyReused):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case err != nil:
		internalError(w, r, err)
	default:
		if replayed {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		writeJSON(w, http.StatusCreated, rec)
	}
}

func parseReceipt(w http.ResponseWriter, r *http.Request) (store.NewReceipt, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = rand.Text()
	}
	if len(key) > maxKeyLen {
		return store.NewReceipt{}, fmt.Errorf("the Idempotency-Key header must be at most %d characters", maxKeyLen)
	}

	var req receiptRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return store.NewReceipt{}, fmt.Errorf("invalid JSON body: %v", err)
	}
	if req.IssueID <= 0 {
		return store.NewReceipt{}, errors.New("issue_id must be a positive integer")
	}
	if req.Qty <= 0 {
		return store.NewReceipt{}, errors.New("qty must be a positive integer")
	}
	day, err := time.Parse(time.DateOnly, req.ReceivedOn)
	if err != nil {
		return store.NewReceipt{}, errors.New("received_on must be a date like 2026-10-11")
	}

	return store.NewReceipt{
		IssueID:        req.IssueID,
		Qty:            int(req.Qty),
		ReceivedOn:     day,
		IdempotencyKey: key,
	}, nil
}
