// T050: ComputeBackoff returns exponential base delays with ±25% jitter.
package fetch_test

import (
	"testing"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/fetch"
)

func TestRetryBackoffSequence(t *testing.T) {
	// Expected base delays for attempts 0..4
	baseDurations := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1000 * time.Millisecond,
		2000 * time.Millisecond,
		4000 * time.Millisecond,
	}

	const iterations = 100
	const jitterFactor = 0.25

	for attempt, base := range baseDurations {
		low := time.Duration(float64(base) * (1 - jitterFactor))
		high := time.Duration(float64(base) * (1 + jitterFactor))

		for i := 0; i < iterations; i++ {
			got := fetch.ComputeBackoff(attempt)
			if got < low || got > high {
				t.Errorf("attempt %d iter %d: ComputeBackoff = %v, want [%v, %v]",
					attempt, i, got, low, high)
			}
		}
	}
}
