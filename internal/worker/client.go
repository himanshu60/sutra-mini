package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/himanshu60/sutra-mini/internal/store"
)

// PermanentError is a response that retrying will not fix, such as a 400.
type PermanentError struct {
	Status int
	Body   string
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("accounting API rejected the voucher with %d: %s", e.Status, e.Body)
}

// Client posts vouchers to the accounting API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client whose every request, connect to last byte, ends within timeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

type pushRequest struct {
	ClientRef   string `json:"client_ref"`
	Type        string `json:"type"`
	SourceID    int64  `json:"source_id"`
	AmountPaise int64  `json:"amount_paise"`
}

type pushResponse struct {
	ExternalRef string `json:"external_ref"`
	Error       string `json:"error"`
}

// Push sends one voucher and returns the accounting system's reference for it.
// duplicate is true when the API already had this client_ref (HTTP 409) and answered
// with the reference it gave the first time.
func (c *Client) Push(ctx context.Context, v store.Voucher) (externalRef string, duplicate bool, err error) {
	body, err := json.Marshal(pushRequest{
		ClientRef:   v.ClientRef,
		Type:        v.Type,
		SourceID:    v.SourceID,
		AmountPaise: v.AmountPaise,
	})
	if err != nil {
		return "", false, fmt.Errorf("encode voucher: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/vouchers", bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("post voucher: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", false, fmt.Errorf("read response: %w", err)
	}

	switch {
	case res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusConflict:
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return "", false, fmt.Errorf("accounting API returned %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	default:
		return "", false, &PermanentError{Status: res.StatusCode, Body: strings.TrimSpace(string(raw))}
	}

	var out pushResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.ExternalRef == "" {
		return "", false, fmt.Errorf("accounting API returned %d without an external_ref: %q", res.StatusCode, raw)
	}
	return out.ExternalRef, res.StatusCode == http.StatusConflict, nil
}
