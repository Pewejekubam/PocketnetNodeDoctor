// T032: apply wrong-byte rollback — a server that returns wrong bytes for one
// chunk causes verification failure, triggering rollback. The live pocketdb
// must be restored to the stale pre-apply state.
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

func TestApply_WrongByteRollback(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build working directory: copy pocketdb-stale/ into workdir/pocketdb/
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Snapshot stale file contents for rollback verification
	staleSnapshots := snapshotDir(t, pocketdbWork)

	// Chunk store server: serve canonical bytes for all files.
	server := testhelpers.NewChunkStoreServer(t)
	for relPath, hash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(hash, data)
	}

	// Build plan with whole-file divergences for all files
	pb := testhelpers.NewPlanBuilder("test-manifest-hash", 12345)
	pb.WithPocketDBPath(pocketdbWork)
	for relPath, hash := range fixture.CanonicalHashes {
		pb.AddWholeFileDivergence(relPath, hash)
	}
	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	// InjectHashMismatchAfterPromoteForTest forces verify.VerifyHashes to return
	// an error after all promotions succeed, triggering rollback.
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:                              planPath,
		Parallel:                              2,
		Logger:                                stderrlog.NewWith(&stderr, false),
		Transport:                             server.Transport(),
		ChunkStoreBaseURL:                     server.URL(),
		InjectHashMismatchAfterPromoteForTest: true,
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = RollbackCompleted (10)
	if code != exitcode.RollbackCompleted {
		t.Errorf("exit code = %d, want %d (RollbackCompleted)", code, exitcode.RollbackCompleted)
	}

	// Assert: stderr contains the rollback message
	stderrStr := stderr.String()
	const wantMsg = "[apply] verification failed — rolling back to pre-apply state"
	if !strings.Contains(stderrStr, wantMsg) {
		t.Errorf("stderr does not contain %q\nstderr: %s", wantMsg, stderrStr)
	}

	// Assert: every file in workdir/pocketdb/ still matches the stale fixture
	for relPath, staleContent := range staleSnapshots {
		livePath := filepath.Join(pocketdbWork, relPath)
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s after rollback: %v", livePath, err)
			continue
		}
		if !bytes.Equal(liveData, staleContent) {
			t.Errorf("file %s: content after rollback does not match stale snapshot", relPath)
		}
	}
}

// snapshotDir walks dir and records a byte copy of every regular file,
// keyed by its path relative to dir (using filepath.FromSlash separators).
func snapshotDir(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir %s: %v", dir, err)
	}
	return result
}
