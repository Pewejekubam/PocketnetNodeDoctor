// T065: apply integrity-check failure triggers rollback — correct bytes are
// served but an injected SQLite corruption after rename causes integrity_check
// to fail, triggering rollback. The live pocketdb must match stale afterward.
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

func TestApply_IntegrityCheckFailureTriggerRollback(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Snapshot stale contents for rollback verification
	staleSnapshots := snapshotDir(t, pocketdbWork)

	// Serve correct bytes for all canonical files (per-chunk hash checks pass)
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

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-integrity", 20003)
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
	// InjectSQLiteCorruptionAfterRename is a test-only field on apply.Options
	// that overwrites main.sqlite3 with corrupt bytes after all renames but
	// before integrity_check. This field does not exist yet — RED compile.
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:                          planPath,
		Parallel:                          2,
		Logger:                            stderrlog.NewWith(&stderr, false),
		Transport:                         server.Transport(),
		ChunkStoreBaseURL:                 server.URL(),
		InjectSQLiteCorruptionAfterRename: true,
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

	// Assert: pocketdb matches stale after rollback
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
