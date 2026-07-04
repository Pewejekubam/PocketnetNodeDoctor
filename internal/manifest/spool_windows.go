//go:build windows

package manifest

import "golang.org/x/sys/windows"

// freeBytes returns the bytes available to an unprivileged writer at path's
// volume. Mirrors internal/preflight/volume_capacity_windows.go (DA3).
func freeBytes(path string) (uint64, error) {
	var freeAvail, totalBytes, totalFree uint64
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, err
	}
	return freeAvail, nil
}
