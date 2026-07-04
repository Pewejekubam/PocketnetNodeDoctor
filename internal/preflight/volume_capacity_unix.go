//go:build linux || darwin

package preflight

import "golang.org/x/sys/unix"

func statfsImpl(path string) (free, total uint64, err error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, 0, err
	}
	free = uint64(fs.Bavail) * uint64(fs.Bsize)
	total = uint64(fs.Blocks) * uint64(fs.Bsize)
	return free, total, nil
}
