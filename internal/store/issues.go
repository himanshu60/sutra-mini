package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Issue is fabric sent to a job-worker and how much of it has come back.
type Issue struct {
	ID          int64 `json:"id"`
	PartyID     int64 `json:"party_id"`
	SKUID       int64 `json:"sku_id"`
	Issued      int   `json:"issued"`
	Received    int   `json:"received"`
	Outstanding int   `json:"outstanding"`
}

// GetIssue returns one issue. received is the total kept in step with every receipt.
func (s *Store) GetIssue(ctx context.Context, id int64) (Issue, error) {
	var is Issue
	err := s.pool.QueryRow(ctx, `
		SELECT id, party_id, sku_id, qty, received_qty
		FROM jobwork_issues WHERE id = $1`, id).
		Scan(&is.ID, &is.PartyID, &is.SKUID, &is.Issued, &is.Received)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("get issue %d: %w", id, err)
	}
	is.Outstanding = is.Issued - is.Received
	return is, nil
}
