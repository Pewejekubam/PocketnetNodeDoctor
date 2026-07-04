// T033: apply SQLite-corruption rollback — apply receives correct bytes from
// the server but an injected corruption of main.sqlite3 (simulated via
// apply.Options.InjectSQLiteCorruptionForTest) causes integrity_check to
// fail, triggering rollback. The live pocketdb must be restored.
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

func TestApply_SQLiteCorruptionRollback(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build working directory: copy pocketdb-stale/ into workdir/pocketdb/
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Snapshot stale file contents for rollback verification
	staleSnapshots := snapshotDir(t, pocketdbWork)

	// Chunk store server: serve correct bytes for everything
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
	// InjectSQLiteCorruptionForTest is a test-only field on apply.Options that
	// forces main.sqlite3 to be overwritten with bad bytes after all renames
	// but before integrity_check. This field does not exist yet — RED compile.
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:                      planPath,
		Parallel:                      2,
		Logger:                        stderrlog.NewWith(&stderr, false),
		Transport:                     server.Transport(),
		ChunkStoreBaseURL:             server.URL(),
		InjectSQLiteCorruptionForTest: true,
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = RollbackCompleted (10)
	if code != exitcode.RollbackCompleted {
		t.Errorf("exit code = %d, want %d (RollbackCompleted)", code, exitcode.RollbackCompleted)
	}

	// Assert: stderr mentions integrity_check
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "integrity_check") {
		t.Errorf("stderr does not contain \"integrity_check\"\nstderr: %s", stderrStr)
	}

	// Assert: pocketdb/ matches stale after rollback
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
