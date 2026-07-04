// T060: apply exit-code contract — each apply-time code emitted under its condition.
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

func TestApplyExitCodes_PlanTampered_Exit15(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())
	workdir := t.TempDir()
	copyDir(t, fixture.StaleDir, filepath.Join(workdir, "pocketdb"))

	pb := testhelpers.NewPlanBuilder("mhash-ec15", 1001)
	pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	planData := pb.Build(t)

	// Tamper the self_hash field.
	tamperedPlan := tamperSelfHash(t, planData)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, tamperedPlan, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	server := testhelpers.NewChunkStoreServer(t)
	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:  planPath,
		Parallel:  1,
		Logger:    stderrlog.NewWith(&stderr, false),
		Transport: server.Transport(),
	})
	if code != exitcode.PlanTampered {
		t.Errorf("exit code = %d, want %d (PlanTampered)", code, exitcode.PlanTampered)
	}
	if server.RequestCount() != 0 {
		t.Errorf("server received %d requests, want 0", server.RequestCount())
	}
}

func TestApplyExitCodes_NetworkExhausted_Exit12(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())
	workdir := t.TempDir()
	copyDir(t, fixture.StaleDir, filepath.Join(workdir, "pocketdb"))

	relPath := "pocketdb/blocks/00000000.dat"
	pb := testhelpers.NewPlanBuilder("mhash-ec12", 1002)
	pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
	pb.AddWholeFileDivergence(relPath, fixture.CanonicalHashes[relPath])
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Chaos server that fails all requests.
	realServer := testhelpers.NewChunkStoreServer(t)
	chaos := testhelpers.NewChaosServer(t, realServer)
	chaos.SetTransientFailCount(999)

	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:  planPath,
		Parallel:  1,
		Logger:    stderrlog.NewWith(&stderr, false),
		Transport: chaos.Transport(),
	})
	if code != exitcode.NetworkBudgetExhausted {
		t.Errorf("exit code = %d, want %d (NetworkBudgetExhausted)", code, exitcode.NetworkBudgetExhausted)
	}
}

func TestApplyExitCodes_Success_Exit0(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())
	workdir := t.TempDir()
	copyDir(t, fixture.StaleDir, filepath.Join(workdir, "pocketdb"))

	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]

	server := testhelpers.NewChunkStoreServer(t)
	canonPath := filepath.Join(fixture.CanonicalDir, "blocks", "00000000.dat")
	data, err := os.ReadFile(canonPath)
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	server.AddChunk(hash, data)

	pb := testhelpers.NewPlanBuilder("mhash-ec0", 1003)
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
	if code != exitcode.Success {
		t.Errorf("exit code = %d, want %d (Success)", code, exitcode.Success)
	}
}

// tamperSelfHash replaces the first 10 chars after "self_hash":" in raw JSON.
func tamperSelfHash(t testing.TB, raw []byte) []byte {
	t.Helper()
	needle := []byte(`"self_hash":"`)
	idx := bytes.Index(raw, needle)
	if idx < 0 {
		t.Fatal("tamperSelfHash: could not find self_hash field")
	}
	start := idx + len(needle)
	if start+10 > len(raw) {
		t.Fatal("tamperSelfHash: plan too short")
	}
	out := append([]byte(nil), raw...)
	copy(out[start:start+10], []byte("aaaaaaaaaa"))
	return out
}
