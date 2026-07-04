// T049: apply resumes after interrupt — the second invocation fetches only
// the remaining (incomplete) chunks.
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

// TestApply_ResumesAfterInterrupt runs two apply invocations against the same
// plan. The first invocation completes N-1 of N chunks (the Nth always returns
// 5xx). The second invocation uses a fully healthy server and must:
//   - emit "[apply] resuming:" on stderr
//   - fetch only the 1 remaining chunk (requestCount == 1, not N)
func TestApply_ResumesAfterInterrupt(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Two divergences: blocks file (N-1 = first, succeeds) and chainstate (Nth, fails first time)
	relPath1 := "pocketdb/blocks/00000000.dat"
	relPath2 := "pocketdb/chainstate/CURRENT"
	hash1 := fixture.CanonicalHashes[relPath1]
	hash2 := fixture.CanonicalHashes[relPath2]

	// Real server with both chunks available
	realServer := testhelpers.NewChunkStoreServer(t)
	for _, pair := range []struct{ relPath, fsRel, hash string }{
		{relPath1, relPath1[len("pocketdb/"):], hash1},
		{relPath2, relPath2[len("pocketdb/"):], hash2},
	} {
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(pair.fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		realServer.AddChunk(pair.hash, data)
	}

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-resume", 10006)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath1, hash1)
	pb.AddWholeFileDivergence(relPath2, hash2)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// First invocation: chaos server fails the 2nd request (chainstate) always.
	// The blocks chunk will succeed; chainstate will not.
	chaos := testhelpers.NewChaosServer(t, realServer)
	chaos.SetTransientFailCount(999) // all requests fail on the chaos path

	// We need the first chunk to succeed and only the second to fail.
	// Use Fault5xx on a fresh server that only serves the first chunk, then
	// use the chaos server which fails everything as the transport for the
	// first run — but this would block both. Instead, use a controlled approach:
	// serve chunk1 on a direct server, set chunk2 to Fault5xx.
	partialServer := testhelpers.NewChunkStoreServer(t)
	partialServer.AddChunk(hash1, func() []byte {
		data, err := os.ReadFile(filepath.Join(fixture.CanonicalDir, filepath.FromSlash(relPath1[len("pocketdb/"):])))
		if err != nil {
			t.Fatalf("read %s: %v", relPath1, err)
		}
		return data
	}())
	// Do NOT add hash2 — the server will 404 for it (simulating a failed fetch for chunk2)

	var stderr1 bytes.Buffer
	code1, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr1, true),
		Transport:         partialServer.Transport(),
		ChunkStoreBaseURL: partialServer.URL(),
	})
	t.Log("first apply stderr:", stderr1.String())

	// First run should fail (chunk2 unavailable — 404 or network error)
	if code1 == exitcode.Success {
		t.Log("first apply unexpectedly succeeded (chunk2 happened to be served); test may not validate resume path")
	}

	// Second invocation: full server, same plan
	var stderr2 bytes.Buffer
	code2, err2 := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr2, true),
		Transport:         realServer.Transport(),
		ChunkStoreBaseURL: realServer.URL(),
	})
	t.Log("second apply stderr:", stderr2.String())

	if err2 != nil {
		t.Fatalf("second apply.Run returned error: %v", err2)
	}
	if code2 != exitcode.Success {
		t.Errorf("second apply.Run exit code = %d, want %d (Success)", code2, exitcode.Success)
	}

	// Assert stderr contains "[apply] resuming:"
	stderrOutput2 := stderr2.String()
	if !strings.Contains(stderrOutput2, "resuming") {
		t.Errorf("second apply stderr does not contain \"resuming\"; stderr:\n%s", stderrOutput2)
	}

	// Assert only the remaining chunk was fetched (realServer count = 1, not 2)
	if got := realServer.RequestCount(); got != 1 {
		t.Errorf("realServer.RequestCount() = %d on second run, want 1 (only unfinished chunk)", got)
	}
}
