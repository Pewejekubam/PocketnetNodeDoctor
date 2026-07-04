//go:build linux

package preflight

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func permissionProbeImpl(path string) (writable, mountedReadOnly bool, err error) {
	// access(W_OK) probe
	if accessErr := unix.Access(path, unix.W_OK); accessErr == nil {
		writable = true
	} else {
		writable = false
	}

	// mount-flag check via /proc/mounts: find the longest mount-point prefix
	// of `path`, look for "ro" in its mount options.
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		// fall through; still report writable
		return writable, false, nil
	}
	f, openErr := os.Open("/proc/mounts")
	if openErr != nil {
		return writable, false, nil
	}
	defer f.Close()
	var bestPrefix string
	var bestRO bool
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 4 {
			continue
		}
		mp := fields[1]
		opts := fields[3]
		if strings.HasPrefix(abs, mp) && len(mp) > len(bestPrefix) {
			bestPrefix = mp
			bestRO = optionsContainRO(opts)
		}
	}
	mountedReadOnly = bestRO
	return writable, mountedReadOnly, nil
}

func optionsContainRO(opts string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == "ro" {
			return true
		}
	}
	return false
}
