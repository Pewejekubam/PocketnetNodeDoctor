// T045: apply exhausts network budget when the chunk store returns persistent 5xx.
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

func TestApply_PersistentFailureExhaustedBudget(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Real server — will never be reached because chaos fails everything
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

	// Chaos server that fails ALL requests
	chaos := testhelpers.NewChaosServer(t, realServer)
	chaos.SetTransientFailCount(999)

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-exhausted", 10002)
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

	// Assert NO file was promoted into pocketdb
	promotedPath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
	promotedData, readErr := os.ReadFile(promotedPath)
	if readErr == nil {
		// File exists — make sure it still has stale content (0xAA), not canonical (0xBB)
		for _, b := range promotedData {
			if b == 0xBB {
				t.Errorf("canonical byte 0xBB found in live file after exhausted budget — file was promoted illegally")
				break
			}
		}
	}

	// Assert stderr names the failing chunk URL
	stderrOutput := stderr.String()
	if !strings.Contains(stderrOutput, hash) && !strings.Contains(stderrOutput, "/chunks/") {
		t.Errorf("stderr does not name the failing chunk URL; stderr:\n%s", stderrOutput)
	}
}
