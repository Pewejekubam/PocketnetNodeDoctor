package fetch

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// basDelays holds the exponential base delays for attempts 0..4.
var baseDelays = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
	2000 * time.Millisecond,
	4000 * time.Millisecond,
}

// ComputeBackoff returns the jittered backoff duration for the given attempt (0-indexed).
// Base delays: attempt 0→250ms, 1→500ms, 2→1000ms, 3→2000ms, 4→4000ms.
// Applies ±25% uniform jitter: result in [base*0.75, base*1.25].
// Uses math/rand (not crypto/rand — speed matters, not security).
func ComputeBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(baseDelays) {
		attempt = len(baseDelays) - 1
	}
	base := baseDelays[attempt]
	// jitter in [-0.25, +0.25] of base
	jitter := time.Duration(float64(base) * (rand.Float64()*0.5 - 0.25))
	return base + jitter
}

// FetchWithRetry wraps Fetch with a 5-attempt retry loop using exponential backoff.
// On each retry (attempt > 0), logs to logger:
// "[apply] retrying <path>[:<offset>] (attempt N/5, backoff Xms)"
// Returns nil on first success, or the last error after 5 failed attempts.
func FetchWithRetry(ctx context.Context, chunkURL, dstPath, logLabel string, transport http.RoundTripper, policy httptransfer.TransferPolicy, logger interface {
	Info(format string, args ...any)
}) error {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := ComputeBackoff(attempt)
			logger.Info("[apply] retrying %s (attempt %d/%d, backoff %dms)",
				logLabel, attempt+1, maxAttempts, backoff.Milliseconds())
			select {
			case <-ctx.Done():
				return fmt.Errorf("fetch: context cancelled during backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}
		if err := Fetch(ctx, chunkURL, dstPath, transport, policy); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
