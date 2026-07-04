package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// TrustRootMismatchError signals manifest-trust-root divergence (EC-008).
type TrustRootMismatchError struct {
	Computed string
	Expected string
}

func (e *TrustRootMismatchError) Error() string {
	return fmt.Sprintf("manifest trust-root mismatch: computed %s, expected %s", e.Computed, e.Expected)
}

// Verify hashes the raw manifest bytes and compares the SHA-256 to pinnedHash.
//
// The canonical server (rig-helper, chunk-001 store) always emits
// canonical-form JSON, so SHA-256(raw bytes) == SHA-256(canonform bytes).
// Hashing raw bytes directly avoids the 2× memory overhead of
// parse → canonform re-serialize → SHA-256 that otherwise materializes
// a second full copy of a large manifest in memory.
func Verify(b []byte, pinnedHash string) error {
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	if got != pinnedHash {
		return &TrustRootMismatchError{Computed: got, Expected: pinnedHash}
	}
	return nil
}

// IsTrustRootMismatch reports whether err is a TrustRootMismatchError.
func IsTrustRootMismatch(err error) bool {
	var tr *TrustRootMismatchError
	return errors.As(err, &tr)
}
