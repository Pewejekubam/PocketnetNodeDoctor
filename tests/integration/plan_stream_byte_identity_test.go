// plan_stream_byte_identity_test.go — SC-003 byte identity (011-010 chunk-1,
// task T004).
//
// The streaming diagnose binary's plan.json over each fixture class must match
// the committed merge-base reference byte-for-byte (path fields normalised);
// the emitted self_hash is verified independently.
//
// Fixtures are materialised in a tempdir from the deterministic psFixtureSpec
// (the committed input spec) rather than read from opaque committed blobs, so
// the fetch_full class's intentionally-absent local pocketdb has no
// git-drops-empty-dir hazard.
//
// This is a golden-reference regression guard, not a red-first test: the refs
// were captured from the merge-base binary, and this branch's production tree is
// byte-identical to that merge-base, so it is GREEN today. It becomes
// load-bearing the moment the streaming emitter lands — any byte-level canonform
// drift in streamed emission fails here (SC-003).
package integration

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

func TestPlanStream_ByteIdentity(t *testing.T) {
	for _, class := range psFixtureClasses {
		class := class
		t.Run(class, func(t *testing.T) {
			manifestBody, pinned, pocketdbDir := psMaterializeFixture(t, t.TempDir(), class)

			code, err, stderr, _, raw := mtRunDiagnose(t, manifestBody, pinned, pocketdbDir)
			if err != nil || code != exitcode.Success {
				t.Fatalf("diagnose code=%d err=%v stderr=%q", code, err, stderr)
			}
			if raw == nil {
				t.Fatalf("no plan.json emitted")
			}

			got := mtNormalizePlan(t, raw)
			want := mustReadFile(t, filepath.Join(psRefsDir(), class+".plan.json"))
			if !bytes.Equal(got, want) {
				t.Errorf("plan.json diverged from committed SC-003 reference (normalised)\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}

			// self_hash (normalised away above) must be self-consistent on the
			// actual emitted plan — proves emission didn't corrupt it.
			p, perr := plan.Unmarshal(raw)
			if perr != nil {
				t.Fatalf("parse emitted plan: %v", perr)
			}
			if err := plan.VerifySelfHash(p); err != nil {
				t.Errorf("self-hash verify on emitted plan: %v", err)
			}
		})
	}
}
