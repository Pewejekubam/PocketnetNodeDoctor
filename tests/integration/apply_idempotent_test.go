// T022: apply idempotency — running apply a second time with the same plan
// exits 0 and issues no new network requests.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_Idempotent(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

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

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-idempotent", 55)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	pb.AddWholeFileDivergence("pocketdb/chainstate/CURRENT", fixture.CanonicalHashes["pocketdb/chainstate/CURRENT"])
	pb.AddWholeFileDivergence("pocketdb/indexes/txindex/CURRENT", fixture.CanonicalHashes["pocketdb/indexes/txindex/CURRENT"])
	pb.AddSQLitePageDivergence("pocketdb/main.sqlite3", []plan.Page{
		{
			Offset:       0,
			ExpectedHash: fixture.CanonicalHashes["pocketdb/main.sqlite3"],
		},
	})

	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	runApply := func(label string) exitcode.Code {
		t.Helper()
		var stderr bytes.Buffer
		code, err := apply.Run(context.Background(), apply.Options{
			PlanPath:          planPath,
			Parallel:          4,
			Logger:            stderrlog.NewWith(&stderr, false),
			Transport:         server.Transport(),
			ChunkStoreBaseURL: server.URL(),
		})
		t.Logf("%s stderr: %s", label, stderr.String())
		if err != nil {
			t.Fatalf("%s: apply.Run error: %v", label, err)
		}
		return code
	}

	// First run — applies all divergences
	code1 := runApply("first run")
	if code1 != exitcode.Success {
		t.Fatalf("first run exit code %d, want %d", code1, exitcode.Success)
	}
	afterFirst := server.RequestCount()

	// Second run — should not issue any new requests
	code2 := runApply("second run")
	if code2 != exitcode.Success {
		t.Fatalf("second run exit code %d, want %d", code2, exitcode.Success)
	}
	afterSecond := server.RequestCount()

	if afterSecond != afterFirst {
		t.Errorf("second run issued %d new request(s); want 0 (idempotent)",
			afterSecond-afterFirst)
	}
}
