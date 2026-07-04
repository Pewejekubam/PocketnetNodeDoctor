package manifest

import (
	"errors"
	"fmt"
)

// FormatVersionUnrecognizedError signals an unsupported manifest format_version
// (CSC002-002, exit code 7).
type FormatVersionUnrecognizedError struct {
	Got        int
	Recognized int
}

func (e *FormatVersionUnrecognizedError) Error() string {
	return fmt.Sprintf("manifest format_version %d not recognized; this build of pocketnet-node-doctor recognizes %d", e.Got, e.Recognized)
}

// CheckFormatVersion returns nil if m.FormatVersion == 1 (the only version
// this chunk recognizes); otherwise FormatVersionUnrecognizedError.
func CheckFormatVersion(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest: nil")
	}
	return CheckFormatVersionValue(m.FormatVersion)
}

// CheckFormatVersionValue is the value-taking companion to CheckFormatVersion.
// Validates a format_version int directly (e.g., from ManifestHeader returned
// by FetchAndProcess).
func CheckFormatVersionValue(v int) error {
	if v != 1 {
		return &FormatVersionUnrecognizedError{Got: v, Recognized: 1}
	}
	return nil
}

// IsFormatVersionUnrecognized reports whether err is a
// FormatVersionUnrecognizedError.
func IsFormatVersionUnrecognized(err error) bool {
	var fv *FormatVersionUnrecognizedError
	return errors.As(err, &fv)
}
