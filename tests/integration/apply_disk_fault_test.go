// T037: apply disk-fault-during-rollback — simulate a rollback-time disk
// error via apply.Options.InjectRollbackFaultForTest. The exit code must be
// RollbackFailed (11) and stderr must name files that could not be restored.
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

func TestApply_DiskFaultDuringRollback(t *testing.T) {
	// Use a single dir for both the fixture and plan.json so apply.Run can
	// derive the correct pocketdb/ path from filepath.Dir(planPath).
	dir := t.TempDir()
	fixture := testhelpers.CreateRollbackFixture(t, dir)

	// Chunk store server — serve correct canonical bytes.
	server := testhelpers.NewChunkStoreServer(t)
	canonBlockData := bytes.Repeat([]byte{0xBB}, 1024)
	server.AddChunk(fixture.CanonicalBlockHash, canonBlockData)

	pb := testhelpers.NewPlanBuilder("test-manifest-hash", 12345)
	pb.WithPocketDBPath(fixture.PocketdbDir)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", fixture.CanonicalBlockHash)
	planData := pb.Build(t)
	// planPath must be in dir so that filepath.Dir(planPath)/pocketdb == dir/pocketdb
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	// InjectRollbackFaultForTest forces a rollback-time rename failure
	// (simulates a disk fault during shadow restore).
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:                   planPath,
		Parallel:                   2,
		Logger:                     stderrlog.NewWith(&stderr, false),
		Transport:                  server.Transport(),
		ChunkStoreBaseURL:          server.URL(),
		InjectRollbackFaultForTest: true,
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = RollbackFailed (11)
	if code != exitcode.RollbackFailed {
		t.Errorf("exit code = %d, want %d (RollbackFailed)", code, exitcode.RollbackFailed)
	}

	// Assert: stderr names files that could not be restored
	stderrStr := stderr.String()
	if strings.TrimSpace(stderrStr) == "" {
		t.Error("stderr is empty; expected it to name unrestored files")
	}
	// At minimum there should be a path or file name mentioned
	if !strings.Contains(stderrStr, "pocketdb") && !strings.Contains(stderrStr, "rollback") {
		t.Errorf("stderr does not mention pocketdb or rollback\nstderr: %s", stderrStr)
	}
}
