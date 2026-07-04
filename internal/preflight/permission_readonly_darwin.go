//go:build darwin

package preflight

import "golang.org/x/sys/unix"

func permissionProbeImpl(path string) (writable, mountedReadOnly bool, err error) {
	if accessErr := unix.Access(path, unix.W_OK); accessErr == nil {
		writable = true
	}
	// Mount-flag check on Darwin would use getmntinfo; deferred to a future
	// chunk. For now, rely on the access probe — if a Darwin volume is
	// mounted read-only, access(W_OK) will fail and the predicate trips.
	return writable, false, nil
}
