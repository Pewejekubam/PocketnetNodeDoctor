//go:build windows

package preflight

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func permissionProbeImpl(path string) (writable, mountedReadOnly bool, err error) {
	// Best-effort writability probe: try to open a temp file in the dir.
	// Read-only-volume detection via GetVolumeInformation FILE_READ_ONLY_VOLUME flag.
	root := filepath.VolumeName(path) + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, false, err
	}
	var (
		volNameBuf [windows.MAX_PATH + 1]uint16
		volSerial  uint32
		maxCompLen uint32
		fsFlags    uint32
		fsNameBuf  [windows.MAX_PATH + 1]uint16
	)
	if err := windows.GetVolumeInformation(rootPtr, &volNameBuf[0], uint32(len(volNameBuf)), &volSerial, &maxCompLen, &fsFlags, &fsNameBuf[0], uint32(len(fsNameBuf))); err != nil {
		return false, false, err
	}
	const FILE_READ_ONLY_VOLUME = 0x00080000
	mountedReadOnly = (fsFlags & FILE_READ_ONLY_VOLUME) != 0
	writable = !mountedReadOnly
	return writable, mountedReadOnly, nil
}
