// T051: apply skips chunks that already have completion markers from a prior run.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_CrossRunSkipsMarked(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	relPath1 := "pocketdb/blocks/00000000.dat"
	relPath2 := "pocketdb/chainstate/CURRENT"
	hash1 := fixture.CanonicalHashes[relPath1]
	hash2 := fixture.CanonicalHashes[relPath2]

	// Build plan with 2 divergences
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-crossrun", 10007)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath1, hash1)
	pb.AddWholeFileDivergence(relPath2, hash2)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// Extract the self_hash from the plan so we can seed the staging dir
	// with the correct plan-hash sentinel (so CreateOrResume resumes).
	selfHash := extractSelfHash(t, planData)

	// Staging directory lives adjacent to pocketdb/ on the same parent volume
	stagingDir := filepath.Join(workdir, "pocketnet-node-doctor-staging")
	testhelpers.CreateStagingDir(t, stagingDir)
	testhelpers.WritePlanHash(t, stagingDir, selfHash)

	// Manually write completion markers for BOTH divergences using plan-relative
	// paths as identifiers (mirrors staging.WriteMarker behaviour in apply.Run).
	testhelpers.WriteMarker(t, stagingDir, relPath1)
	testhelpers.WriteMarker(t, stagingDir, relPath2)

	// Chunk store server — should NOT be contacted
	server := testhelpers.NewChunkStoreServer(t)
	// Add chunks anyway so a fetch would succeed if incorrectly attempted
	for _, pair := range []struct{ relPath, hash string }{
		{relPath1, hash1},
		{relPath2, hash2},
	} {
		fsRel := pair.relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(pair.hash, data)
	}

	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          2,
		Logger:            stderrlog.NewWith(&stderr, true),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	if err != nil {
		t.Fatalf("apply.Run returned error: %v", err)
	}
	if code != exitcode.Success {
		t.Errorf("apply.Run exit code = %d, want %d (Success)", code, exitcode.Success)
	}

	// Assert no HTTP requests were made
	if got := server.RequestCount(); got != 0 {
		t.Errorf("server.RequestCount() = %d, want 0 — all chunks were already marked complete", got)
	}
}

// extractSelfHash parses plan JSON bytes and returns the self_hash field.
func extractSelfHash(t testing.TB, planData []byte) string {
	t.Helper()
	var m struct {
		SelfHash string `json:"self_hash"`
	}
	if err := json.Unmarshal(planData, &m); err != nil {
		t.Fatalf("extractSelfHash: %v", err)
	}
	if m.SelfHash == "" {
		t.Fatal("extractSelfHash: self_hash is empty")
	}
	return m.SelfHash
}
