// T062: apply invariant guarantees — atomicity, verify-before-promote, resumability, idempotency.
package contract

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

func TestApplyGuarantees_Idempotency(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())
	workdir := t.TempDir()
	copyDir(t, fixture.StaleDir, filepath.Join(workdir, "pocketdb"))

	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]
	server := testhelpers.NewChunkStoreServer(t)
	data, err := os.ReadFile(filepath.Join(fixture.CanonicalDir, "blocks", "00000000.dat"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	server.AddChunk(hash, data)

	pb := testhelpers.NewPlanBuilder("mhash-idem", 3001)
	pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	opts := apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&bytes.Buffer{}, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	}

	// First run.
	code1, _ := apply.Run(context.Background(), opts)
	if code1 != exitcode.Success {
		t.Fatalf("1st run: exit code = %d, want 0", code1)
	}
	count1 := server.RequestCount()

	// Second run — same plan, same pocketdb (now canonical).
	code2, _ := apply.Run(context.Background(), opts)
	if code2 != exitcode.Success {
		t.Errorf("2nd run: exit code = %d, want 0 (EC-006 idempotency)", code2)
	}
	count2 := server.RequestCount()
	if count2 != count1 {
		t.Errorf("2nd run made %d requests (total=%d), want 0 new requests", count2-count1, count2)
	}
}

func TestApplyGuarantees_VerifyBeforePromote(t *testing.T) {
	// T036 verifies the invariant at the unit-test level. This contract test
	// verifies it at the integration level: a wrong-hash chunk is never
	// visible in the live pocketdb tree.
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())
	workdir := t.TempDir()
	copyDir(t, fixture.StaleDir, filepath.Join(workdir, "pocketdb"))

	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]
	// Server returns wrong bytes (FaultBadBytes) — chunk will be hash-gate rejected.
	server := testhelpers.NewChunkStoreServer(t)
	server.SetFaultMode(testhelpers.FaultBadBytes)
	// Add a chunk entry so it's "registered" but server returns bad bytes.
	data, err := os.ReadFile(filepath.Join(fixture.CanonicalDir, "blocks", "00000000.dat"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	server.AddChunk(hash, data)

	pb := testhelpers.NewPlanBuilder("mhash-gate", 3002)
	pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	// Either NetworkBudgetExhausted (retry budget gone) or RollbackCompleted —
	// neither is Success.
	if code == exitcode.Success {
		t.Errorf("exit code = 0 (Success), but server served wrong bytes — promotion must not have happened without hash match")
	}
	// Live file must still be the stale version (not partially modified).
	staleData, _ := os.ReadFile(filepath.Join(fixture.StaleDir, "blocks", "00000000.dat"))
	liveData, _ := os.ReadFile(filepath.Join(workdir, "pocketdb", "blocks", "00000000.dat"))
	if !bytes.Equal(staleData, liveData) {
		t.Error("live pocketdb/blocks/00000000.dat was modified despite failed hash gate")
	}
}
