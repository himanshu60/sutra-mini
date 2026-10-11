package api

import (
	"errors"
	"net/http"

	"github.com/himanshu60/sutra-mini/internal/store"
)

// listVouchers handles GET /vouchers?status=pending|pushed|failed (status is optional).
func (s *server) listVouchers(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !store.ValidStatus(status) {
		writeError(w, http.StatusBadRequest, "status must be pending, pushed or failed")
		return
	}
	vouchers, err := s.store.ListVouchers(r.Context(), status)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, vouchers)
}

// retryVoucher handles POST /vouchers/{id}/retry.
func (s *server) retryVoucher(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.store.RetryVoucher(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "voucher not found")
	case errors.Is(err, store.ErrNotRetryable):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, v)
	}
}
