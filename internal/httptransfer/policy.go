package httptransfer

import (
	"errors"
	"fmt"
	"time"
)

// TransferPolicy holds per-phase network timeouts for HTTP transfers.
type TransferPolicy struct {
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	BodyStallThreshold    time.Duration
}

// DefaultPolicy returns the production transfer policy.
func DefaultPolicy() TransferPolicy {
	return TransferPolicy{
		ConnectTimeout:        30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		BodyStallThreshold:    60 * time.Second,
	}
}

// ErrTransferStalled is the sentinel error wrapped by TransferStalledError.
var ErrTransferStalled = errors.New("transfer stalled")

// TransferStalledError is returned when no payload progress is observed
// within BodyStallThreshold.
type TransferStalledError struct {
	Threshold time.Duration
}

func (e TransferStalledError) Error() string {
	return fmt.Sprintf("network transfer stalled: no payload progress for %s", e.Threshold)
}

func (e TransferStalledError) Unwrap() error {
	return ErrTransferStalled
}

// IsTransferStalled reports whether err or any error in its chain is a
// TransferStalledError.
func IsTransferStalled(err error) bool {
	var t TransferStalledError
	return errors.As(err, &t)
}
