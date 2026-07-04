//go:build windows

package preflight

import "golang.org/x/sys/windows"

func statfsImpl(path string) (free, total uint64, err error) {
	var freeBytes, totalBytes, totalFree uint64
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytes, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return freeBytes, totalBytes, nil
}
