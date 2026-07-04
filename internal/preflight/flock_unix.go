//go:build linux || darwin

package preflight

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// flockProbe attempts a non-blocking exclusive advisory lock on path. Returns
// (true, nil) if the lock could NOT be acquired (foreign lock present);
// (false, nil) if it was acquired (and immediately released); (false, err)
// on operational error. Missing-file is treated as "not locked".
func flockProbe(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// Permission denied / read-only mount: not a foreign lock — let the
		// permission predicate trip on these. Return "not locked".
		return false, nil
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return true, nil
		}
		return false, err
	}
	// Got the lock; release immediately.
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false, nil
}
