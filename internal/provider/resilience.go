package provider

import (
	"context"
	"math"
	"time"
)

// ClassifyHTTPError maps an HTTP status + body to a typed provider error shared
// by all providers, so error handling is identical regardless of which model is
// selected. 401/403 → AuthError (the key is bad). 429/503/5xx → retryable
// APIError (transient). Any other 4xx → permanent APIError (402 Insufficient
// Balance, 400 Bad Request, 404, …) that must not be retried.
func ClassifyHTTPError(providerName string, status int, body []byte) error {
	if status == 401 || status == 403 {
		return &AuthError{Provider: providerName, Status: status}
	}
	retryable := status == 429 || status == 503 || status >= 500
	return &APIError{Provider: providerName, Status: status, Body: string(body), Retryable: retryable}
}

// StreamWithRetry runs attempt(i) — one streaming attempt that emits chunks to
// ch and returns an error for setup/HTTP failures — with exponential backoff.
// Only transient errors retry: AuthError and non-retryable APIError fail fast.
// On final failure it emits a single terminal ChunkError. Retries are silent.
//
// This is the shared resilience path for every provider, so a transient 429/503
// is recovered transparently no matter which model backs the turn.
func StreamWithRetry(ctx context.Context, ch chan<- Chunk, attempt func(attempt int) error) {
	if ctx == nil {
		ctx = context.Background()
	}
	const maxRetries = 2
	baseDelay := 1 * time.Second
	maxDelay := 8 * time.Second

	for i := 0; i <= maxRetries; i++ {
		if ctx.Err() != nil {
			ch <- Chunk{Type: ChunkError, Err: ctx.Err()}
			return
		}

		err := attempt(i)
		if err == nil {
			return // success
		}

		// Don't retry auth errors (401/403) — the key is bad.
		if ae, ok := err.(*AuthError); ok {
			ch <- Chunk{Type: ChunkError, Err: ae}
			return
		}
		// Don't retry permanent API errors (402, 400, 404, …) — fail fast.
		if apiErr, ok := err.(*APIError); ok && !apiErr.Retryable {
			ch <- Chunk{Type: ChunkError, Err: apiErr}
			return
		}

		if i == maxRetries {
			ch <- Chunk{Type: ChunkError, Err: err}
			return
		}

		delay := time.Duration(math.Min(float64(baseDelay)*math.Pow(2, float64(i)), float64(maxDelay)))
		select {
		case <-ctx.Done():
			ch <- Chunk{Type: ChunkError, Err: ctx.Err()}
			return
		case <-time.After(delay):
		}
	}
}
