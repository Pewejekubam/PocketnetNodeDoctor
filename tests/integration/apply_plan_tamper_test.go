// T047: apply rejects a tampered plan (self_hash mismatch) before fetching anything.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_PlanTampered(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Build a valid plan
	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-tamper", 10004)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath, hash)
	validPlanData := pb.Build(t)

	// Mutate the self_hash field: find `"self_hash":"` and replace the next 10 chars with "aaaaaaaaaa"
	marker := []byte(`"self_hash":"`)
	idx := bytes.Index(validPlanData, marker)
	if idx == -1 {
		t.Fatal("could not find self_hash field in plan JSON")
	}
	start := idx + len(marker)
	if start+10 > len(validPlanData) {
		t.Fatal("plan JSON too short after self_hash marker")
	}
	tamperedPlanData := make([]byte, len(validPlanData))
	copy(tamperedPlanData, validPlanData)
	copy(tamperedPlanData[start:start+10], []byte("aaaaaaaaaa"))

	// Write mutated plan
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, tamperedPlanData, 0o644); err != nil {
		t.Fatalf("write tampered plan.json: %v", err)
	}

	// Create a chunk store server (should NOT be contacted)
	server := testhelpers.NewChunkStoreServer(t)

	var stderr bytes.Buffer
	code, runErr := apply.Run(context.Background(), apply.Options{
		PlanPath:  planPath,
		Parallel:  1,
		Logger:    stderrlog.NewWith(&stderr, true),
		Transport: server.Transport(),
	})
	t.Log("apply stderr:", stderr.String())

	if code != exitcode.PlanTampered {
		t.Errorf("apply.Run exit code = %d, want %d (PlanTampered)", code, exitcode.PlanTampered)
	}
	_ = runErr

	// Assert no chunks were fetched
	if got := server.RequestCount(); got != 0 {
		t.Errorf("server.RequestCount() = %d, want 0 — chunk store should not have been contacted", got)
	}

	// Assert staging directory was NOT created
	stagingDir := filepath.Join(workdir, "pocketnet-node-doctor-staging")
	if _, statErr := os.Stat(stagingDir); statErr == nil {
		t.Errorf("staging directory %s exists but should NOT have been created for a tampered plan", stagingDir)
	}

	_ = strings.Contains(stderr.String(), "tamper") // suppress unused import lint
}
