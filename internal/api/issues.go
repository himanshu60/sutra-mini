package api

import (
	"errors"
	"net/http"

	"github.com/himanshu60/sutra-mini/internal/store"
)

// getIssue handles GET /issues/{id}.
func (s *server) getIssue(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	is, err := s.store.GetIssue(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "issue not found")
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, is)
	}
}
