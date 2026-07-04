// trust_boundary_equivalence_test.go — US-002 (SC-004) byte-equivalence for the
// 009-007 manifest trust-boundary chunk. The post-change binary's plan.json
// over each committed fixture class must match the committed reference (path
// fields normalised); self_hash is verified independently.
package integration

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

func TestTrustBoundary_SC004_ByteEquivalence(t *testing.T) {
	for _, class := range mtFixtureClasses {
		class := class
		t.Run(class, func(t *testing.T) {
			manifestBody := mustReadFile(t, filepath.Join(mtFixturesDir(), class, "manifest.json"))
			pinned := mtWholeFileHash(manifestBody) // sha256 hex over the served body
			pocketdbDir := filepath.Join(mtFixturesDir(), class, "pocketdb")

			code, err, stderr, _, raw := mtRunDiagnose(t, manifestBody, pinned, pocketdbDir)
			if err != nil || code != exitcode.Success {
				t.Fatalf("diagnose code=%d err=%v stderr=%q", code, err, stderr)
			}
			if raw == nil {
				t.Fatalf("no plan.json emitted")
			}

			got := mtNormalizePlan(t, raw)
			want := mustReadFile(t, filepath.Join(mtRefsDir(), class+".plan.json"))
			if !bytes.Equal(got, want) {
				t.Errorf("plan.json diverged from committed reference (normalised)\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}

			// self_hash (normalised away above) must still be self-consistent
			// on the actual emitted plan — proves the rewrite didn't corrupt it.
			p, perr := plan.Unmarshal(raw)
			if perr != nil {
				t.Fatalf("parse emitted plan: %v", perr)
			}
			if err := plan.VerifySelfHash(p); err != nil {
				t.Errorf("self-hash verify on post-change plan: %v", err)
			}
		})
	}
}
