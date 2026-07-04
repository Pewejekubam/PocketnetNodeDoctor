// T069: apply concurrency stress — runs apply multiple times with different
// parallelism settings against a 10-divergence plan. The -race detector (via
// go test -race) validates absence of data races.
package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_ConcurrencyStress(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build 10 distinct canonical chunks by repeating a pattern over distinct file names.
	// The SmallFixture provides 4 real canonical files; we supplement with synthetic ones.
	type chunkEntry struct {
		relPath string
		hash    string
		data    []byte
	}

	server := testhelpers.NewChunkStoreServer(t)
	var chunks []chunkEntry

	// Add real canonical files
	for relPath, hash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(hash, data)
		chunks = append(chunks, chunkEntry{relPath: relPath, hash: hash, data: data})
	}

	// Add synthetic files to reach 10 divergences total
	baseDir := t.TempDir()
	for i := len(chunks); i < 10; i++ {
		content := bytes.Repeat([]byte{byte(0xC0 + i)}, 512)
		hash := quickHash(content)
		relPath := fmt.Sprintf("pocketdb/extra/file%02d.dat", i)
		server.AddChunk(hash, content)
		chunks = append(chunks, chunkEntry{relPath: relPath, hash: hash, data: content})
		_ = baseDir // reference to avoid unused warning
	}

	// runApply creates a fresh workdir and runs apply with the given parallel setting.
	runApply := func(t *testing.T, parallel int) {
		t.Helper()

		workdir := t.TempDir()
		pocketdbWork := filepath.Join(workdir, "pocketdb")
		copyDir(t, fixture.StaleDir, pocketdbWork)

		pb := testhelpers.NewPlanBuilder(
			fmt.Sprintf("test-manifest-hash-concurrency-p%d", parallel),
			int64(20006+parallel),
		)
		pb.WithPocketDBPath(pocketdbWork)
		for _, c := range chunks {
			pb.AddWholeFileDivergence(c.relPath, c.hash)
		}
		planData := pb.Build(t)

		planPath := filepath.Join(workdir, "plan.json")
		if err := os.WriteFile(planPath, planData, 0o644); err != nil {
			t.Fatalf("write plan.json: %v", err)
		}

		var stderr bytes.Buffer
		// apply.Options.Parallel and apply.Run do not exist yet — RED compile.
		code, err := apply.Run(context.Background(), apply.Options{
			PlanPath:          planPath,
			Parallel:          parallel,
			Logger:            stderrlog.NewWith(&stderr, false),
			Transport:         server.Transport(),
			ChunkStoreBaseURL: server.URL(),
		})
		t.Logf("apply (parallel=%d) stderr: %s", parallel, stderr.String())

		if err != nil {
			t.Errorf("apply.Run (parallel=%d) returned error: %v", parallel, err)
		}
		if code != exitcode.Success {
			t.Errorf("apply.Run (parallel=%d) exit code = %d, want %d (Success)", parallel, code, exitcode.Success)
		}
	}

	// Run 3 times with --parallel 4
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("parallel4/run%d", i), func(t *testing.T) {
			runApply(t, 4)
		})
	}

	// Run 3 times with --parallel 16
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("parallel16/run%d", i), func(t *testing.T) {
			runApply(t, 16)
		})
	}
}
