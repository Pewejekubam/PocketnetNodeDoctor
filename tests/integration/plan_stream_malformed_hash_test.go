// plan_stream_malformed_hash_test.go — SC-006 (011-010 chunk-1, task T026):
// fuzz-shaped malformed hashes on both the divergence expected_hash and page
// expected_hash yield a typed, entry-naming refusal (exit 1), apply zero
// divergences, and NEVER panic. Regression lock for bead p5m (the merge-base
// panics in the divergenceTaskIter h[0:2] slice on a non-64-char hash).
//
// Load-bearing fixture construction: each plan carries a self_hash recomputed
// over its malformed-hash-bearing bytes (PlanBuilder.Build canonform-serializes
// the malformed value and hashes over it), so the plan PASSES self-hash verify
// (class 3, exit 15) and reaches the class-4 hash-format pass. A stale self_hash
// would fail at the tamper gate and never exercise the p5m path.
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

// malformedHashForms are the fuzz shapes SC-006 requires. "empty" is included
// for pages (whole_file omits an empty expected_hash, tripping the shape rule
// instead — also exit 1).
var malformedHashForms = map[string]string{
	"short":     strings.Repeat("a", 10),
	"non_hex":   strings.Repeat("g", 64),
	"uppercase": strings.Repeat("A", 64),
}

func TestApply_SC006_MalformedWholeFileHash(t *testing.T) {
	for name, badHash := range malformedHashForms {
		t.Run(name, func(t *testing.T) {
			workdir := t.TempDir()
			pb := testhelpers.NewPlanBuilder("mh-manifest", 10012)
			pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
			pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", badHash)
			planData := pb.Build(t) // self_hash computed over the malformed bytes
			planPath := filepath.Join(workdir, "plan.json")
			if err := os.WriteFile(planPath, planData, 0o644); err != nil {
				t.Fatal(err)
			}

			code := applyCode(t, planPath, nil)
			if code != exitcode.GenericError {
				t.Fatalf("%s: exit = %d, want %d (GenericError — typed refusal, not tamper/panic)", name, code, exitcode.GenericError)
			}
			if _, statErr := os.Stat(filepath.Join(workdir, "pocketnet-node-doctor-staging")); statErr == nil {
				t.Errorf("%s: staging dir created — malformed-hash refusal must mutate zero staging", name)
			}
		})
	}
}

func TestApply_SC006_MalformedPageHash(t *testing.T) {
	forms := malformedHashForms
	forms["empty"] = ""
	for name, badHash := range forms {
		t.Run(name, func(t *testing.T) {
			workdir := t.TempDir()
			pb := testhelpers.NewPlanBuilder("mh-manifest", 10013)
			pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
			pb.AddSQLitePageDivergence("pocketdb/main.sqlite3", []plan.Page{
				{Offset: 0, ExpectedHash: badHash},
			})
			planData := pb.Build(t)
			planPath := filepath.Join(workdir, "plan.json")
			if err := os.WriteFile(planPath, planData, 0o644); err != nil {
				t.Fatal(err)
			}

			code := applyCode(t, planPath, nil)
			if code != exitcode.GenericError {
				t.Fatalf("%s: exit = %d, want %d (GenericError — no panic in the h[0:2] slice)", name, code, exitcode.GenericError)
			}
			if _, statErr := os.Stat(filepath.Join(workdir, "pocketnet-node-doctor-staging")); statErr == nil {
				t.Errorf("%s: staging dir created — malformed-hash refusal must mutate zero staging", name)
			}
		})
	}
}
