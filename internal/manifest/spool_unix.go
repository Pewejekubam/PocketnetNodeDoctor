//go:build linux || darwin

package manifest

import "golang.org/x/sys/unix"

// freeBytes returns the bytes available to an unprivileged writer at path's
// filesystem. Mirrors internal/preflight/volume_capacity_unix.go (DA3).
func freeBytes(path string) (uint64, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, err
	}
	return uint64(fs.Bavail) * uint64(fs.Bsize), nil
}
