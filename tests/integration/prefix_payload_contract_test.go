// prefix_payload_contract_test.go — EC-001 standing gate (011-010 chunk-1, task
// T010).
//
// Pins the emission-format world-claim the streaming self-hash verifier depends
// on: for any tool-emitted (canonform) plan,
//
//	sha256( emitted[:index_of(`,"self_hash":"`)] + "}" ) == self_hash
//
// and self_hash is the lexically last top-level member. If any future top-level
// plan field sorts after "self_hash", or emission stops being canonform, this
// gate fails loudly (contracts/plan-byte-layout.md § Standing guard).
//
// Uses the mixed_kind fixture (both divergence kinds, whole_file published
// before sqlite_pages) so the plan is non-trivial.
package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

func TestPrefixPayloadContract(t *testing.T) {
	manifestBody, pinned, pocketdbDir := psMaterializeFixture(t, t.TempDir(), "mixed_kind")
	code, err, stderr, _, raw := mtRunDiagnose(t, manifestBody, pinned, pocketdbDir)
	if err != nil || code != exitcode.Success {
		t.Fatalf("diagnose code=%d err=%v stderr=%q", code, err, stderr)
	}
	if raw == nil {
		t.Fatalf("no plan.json emitted")
	}

	// Embedded self_hash value.
	var probe struct {
		SelfHash string `json:"self_hash"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal self_hash: %v", err)
	}
	if probe.SelfHash == "" {
		t.Fatalf("plan has empty self_hash")
	}

	// Prefix-payload identity: hash the bytes up to the self_hash member plus a
	// synthetic closing brace, and compare to the embedded value.
	needle := []byte(`,"self_hash":"`)
	idx := bytes.Index(raw, needle)
	if idx < 0 {
		t.Fatalf("plan does not contain the %q member", needle)
	}
	payload := make([]byte, 0, idx+1)
	payload = append(payload, raw[:idx]...)
	payload = append(payload, '}')
	sum := sha256.Sum256(payload)
	computed := hex.EncodeToString(sum[:])
	if computed != probe.SelfHash {
		t.Errorf("prefix-payload identity failed:\n  sha256(emitted[:idx]+\"}\") = %s\n  embedded self_hash        = %s", computed, probe.SelfHash)
	}

	// self_hash is the lexically last top-level member: walk the top-level
	// keys and assert the final one is "self_hash".
	keys := topLevelKeys(t, raw)
	if len(keys) == 0 || keys[len(keys)-1] != "self_hash" {
		t.Errorf("self_hash is not the last top-level member; keys=%v", keys)
	}
	if n := countKey(keys, "self_hash"); n != 1 {
		t.Errorf("self_hash appears %d times as a top-level member, want 1", n)
	}
}

// topLevelKeys returns the top-level object keys of a JSON object in document
// order (values skipped).
func topLevelKeys(t *testing.T, b []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("expected top-level object, got %v (err %v)", tok, err)
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("read key: %v", err)
		}
		keys = append(keys, keyTok.(string))
		// Skip the value.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("skip value for %q: %v", keyTok, err)
		}
	}
	return keys
}

func countKey(keys []string, k string) int {
	n := 0
	for _, x := range keys {
		if x == k {
			n++
		}
	}
	return n
}
