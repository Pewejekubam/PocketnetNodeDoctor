// T046: apply returns NetworkBudgetExhausted when the chunk store is unreachable.
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

func TestApply_ChunkStoreUnreachable(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	realServer := testhelpers.NewChunkStoreServer(t)

	relPath := "pocketdb/blocks/00000000.dat"
	fsRel := relPath[len("pocketdb/"):]
	canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
	data, err := os.ReadFile(canonPath)
	if err != nil {
		t.Fatalf("read canonical file %s: %v", canonPath, err)
	}
	hash := fixture.CanonicalHashes[relPath]
	realServer.AddChunk(hash, data)

	// Chaos server: all connections immediately closed
	chaos := testhelpers.NewChaosServer(t, realServer)
	chaos.SetFullUnreachable(true)

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-unreachable", 10003)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	code, runErr := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr, true),
		Transport:         chaos.Transport(),
		ChunkStoreBaseURL: chaos.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	if code != exitcode.NetworkBudgetExhausted {
		t.Errorf("apply.Run exit code = %d, want %d (NetworkBudgetExhausted)", code, exitcode.NetworkBudgetExhausted)
	}
	_ = runErr

	// Assert no partial staging content exists in pocketdb
	promotedPath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
	promotedData, readErr := os.ReadFile(promotedPath)
	if readErr == nil {
		for _, b := range promotedData {
			if b == 0xBB {
				t.Errorf("canonical byte 0xBB found in live file after unreachable server — partial staging leaked")
				break
			}
		}
	}
}
