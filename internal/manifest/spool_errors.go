package manifest

import (
	"errors"
	"fmt"
)

// SpoolCapacityError signals a pre-download free-disk shortfall or a mid-spool
// ENOSPC. It maps to exitcode.Capacity (5) — the existing insufficient-disk
// allocation; no new exit code is introduced (FR-002, FR-005).
type SpoolCapacityError struct {
	Dir            string
	RequiredBytes  uint64 // declared Content-Length + 64 MiB margin
	ManifestBytes  uint64 // declared Content-Length
	AvailableBytes uint64
}

func (e *SpoolCapacityError) Error() string {
	return fmt.Sprintf("manifest spool: insufficient free disk at %q: need %d bytes (manifest %d + 64 MiB margin), have %d",
		e.Dir, e.RequiredBytes, e.ManifestBytes, e.AvailableBytes)
}

// IsSpoolCapacity reports whether err is (or wraps) a *SpoolCapacityError.
func IsSpoolCapacity(err error) bool {
	var sc *SpoolCapacityError
	return errors.As(err, &sc)
}

// ManifestFetchError signals a response with no declared body size, or a body
// exceeding its declared size. Terminal this chunk; maps to
// exitcode.GenericError (1).
type ManifestFetchError struct {
	Reason       string // "no-content-length" | "over-declared-length"
	DeclaredSize int64  // -1 when absent
}

func (e *ManifestFetchError) Error() string {
	switch e.Reason {
	case "no-content-length":
		return "manifest fetch: response has no declared Content-Length; refusing to spool"
	case "over-declared-length":
		return fmt.Sprintf("manifest fetch: body exceeds declared Content-Length of %d bytes; aborting", e.DeclaredSize)
	default:
		return fmt.Sprintf("manifest fetch: %s", e.Reason)
	}
}

// ManifestInvalidPathError signals an FR-003 path-hygiene rejection. Terminal;
// maps to exitcode.GenericError (1).
type ManifestInvalidPathError struct {
	Path   string
	Reason string
}

func (e *ManifestInvalidPathError) Error() string {
	return fmt.Sprintf("manifest entry path %q rejected: %s", e.Path, e.Reason)
}

// IsManifestInvalidPath reports whether err is (or wraps) a
// *ManifestInvalidPathError.
func IsManifestInvalidPath(err error) bool {
	var ip *ManifestInvalidPathError
	return errors.As(err, &ip)
}
