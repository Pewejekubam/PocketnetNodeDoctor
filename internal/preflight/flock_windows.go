//go:build windows

package preflight

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func flockProbe(path string) (bool, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return false, nil
		}
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return true, nil
		}
		return false, err
	}
	defer windows.CloseHandle(h)
	const LOCKFILE_EXCLUSIVE_LOCK = 0x00000002
	const LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
	var overlapped os.NewFile
	_ = overlapped
	ovl := windows.Overlapped{}
	if err := windows.LockFileEx(h, LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ovl); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return true, nil
		}
		return false, err
	}
	_ = windows.UnlockFileEx(h, 0, 1, 0, &ovl)
	return false, nil
}
