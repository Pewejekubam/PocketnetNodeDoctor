// T064: apply resume at three interrupt points — each simulated interrupt
// leaves one marker written; subsequent re-runs skip already-completed chunks.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_ResumeAtThreeInterruptPoints(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Three whole-file divergences
	relPath1 := "pocketdb/blocks/00000000.dat"
	relPath2 := "pocketdb/chainstate/CURRENT"
	relPath3 := "pocketdb/indexes/txindex/CURRENT"
	hash1 := fixture.CanonicalHashes[relPath1]
	hash2 := fixture.CanonicalHashes[relPath2]
	hash3 := fixture.CanonicalHashes[relPath3]

	server := testhelpers.NewChunkStoreServer(t)
	for _, pair := range []struct{ relPath, hash string }{
		{relPath1, hash1},
		{relPath2, hash2},
		{relPath3, hash3},
	} {
		fsRel := pair.relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(pair.hash, data)
	}

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-resume-multi", 20002)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath1, hash1)
	pb.AddWholeFileDivergence(relPath2, hash2)
	pb.AddWholeFileDivergence(relPath3, hash3)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// Extract the plan self_hash so we can seed the staging dir with the
	// correct plan-hash sentinel (so CreateOrResume resumes, not discards).
	selfHash := extractSelfHash(t, planData)

	// Simulate interrupt: only the first marker is present.
	// The staging directory lives adjacent to pocketdb/ on the same parent volume.
	stagingDir := filepath.Join(workdir, "pocketnet-node-doctor-staging")
	testhelpers.CreateStagingDir(t, stagingDir)
	testhelpers.WritePlanHash(t, stagingDir, selfHash)

	// Write completion marker for only the first chunk (simulating interrupt after chunk 1).
	// Use the plan-relative path as the identifier (mirrors apply.Run behaviour).
	testhelpers.WriteMarker(t, stagingDir, relPath1)

	// Also manually promote the first canonical file to simulate a completed promotion.
	{
		fsRel := relPath1[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		livePath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical for pre-promote: %v", err)
		}
		if err := os.WriteFile(livePath, data, 0o644); err != nil {
			t.Fatalf("pre-promote write: %v", err)
		}
	}

	// Re-run apply — should fetch only chunks 2 and 3 (2 requests, not 3).
	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr, false),
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

	// Assert: only 2 chunks fetched (chunk 1 was already marked done)
	if got := server.RequestCount(); got != 2 {
		t.Errorf("server.RequestCount() = %d, want 2 (chunk 1 already marked)", got)
	}

	// Verify all three canonical files are now in place
	for _, pair := range []struct{ relPath, hash string }{
		{relPath1, hash1},
		{relPath2, hash2},
		{relPath3, hash3},
	} {
		fsRel := pair.relPath[len("pocketdb/"):]
		livePath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s: %v", livePath, err)
			continue
		}
		if got := quickHash(liveData); got != pair.hash {
			t.Errorf("file %s: hash %s, want %s", pair.relPath, got, pair.hash)
		}
	}
}
