package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
)

// SelfHashMismatchError signals a tampered or stale plan (CSC002-001).
type SelfHashMismatchError struct {
	Computed string
	Embedded string
}

func (e *SelfHashMismatchError) Error() string {
	return fmt.Sprintf("plan self_hash mismatch: computed %s, embedded %s", e.Computed, e.Embedded)
}

// ComputeSelfHash strips self_hash from the plan, canonform-serializes the
// result, and returns the lowercase 64-hex SHA-256 of that payload.
func ComputeSelfHash(p Plan) (string, error) {
	canon, err := marshalWithoutSelfHash(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// VerifySelfHash recomputes the self-hash and compares to the embedded value.
func VerifySelfHash(p Plan) error {
	computed, err := ComputeSelfHash(p)
	if err != nil {
		return err
	}
	if computed != p.SelfHash {
		return &SelfHashMismatchError{Computed: computed, Embedded: p.SelfHash}
	}
	return nil
}

// marshalWithoutSelfHash produces canonical-form bytes of p with the
// self_hash field physically removed (not present, not empty-string).
func marshalWithoutSelfHash(p Plan) ([]byte, error) {
	tmp, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("self_hash: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(tmp))
	dec.UseNumber()
	var generic map[string]any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("self_hash: re-decode: %w", err)
	}
	delete(generic, "self_hash")
	return canonform.Marshal(generic)
}
