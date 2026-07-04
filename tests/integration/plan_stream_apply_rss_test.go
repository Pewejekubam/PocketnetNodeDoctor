// plan_stream_apply_rss_test.go — SC-002 apply consumption RSS bound (011-010
// chunk-1, task T006).
//
// Drives apply over a plan carrying >= 2,000,000 sqlite_pages page entries built
// by the streaming test-side generator (psGenerateLargeSQLitePlan — never
// PlanBuilder.Build before the baseline read, which would mask the delta via the
// monotone VmHWM), and asserts the whole-run peak RSS delta stays under 300 MiB.
//
// RED-FIRST: on the merge-base (materializing) binary — which reads the whole
// ~180 MB plan.json into memory and then materialises every page into a full
// plan.Plan held for the entire run — this delta is multiple hundred MiB and the
// test FAILS. That failure, captured before any streaming production change, is
// the SC-002 vacuity guard. The streaming consumer (version-gate + self-hash +
// per-page StreamDivergences) must turn it GREEN.
//
// Linux-only (reads /proc/self/status); skipped under -short.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_RSSBounded_SQLitePages(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VmHWM measurement requires /proc/self/status (linux-only)")
	}
	if testing.Short() {
		t.Skip("skipping RSS-bounded test in -short mode")
	}

	const (
		nPages       = 2_000_000         // >= 2M sqlite_pages page entries (SC-002)
		rssThreshold = 300 * 1024 * 1024 // 300 MiB delta upper bound
	)

	workdir := t.TempDir()
	pocketdbDir := filepath.Join(workdir, "pocketdb")
	if err := os.MkdirAll(pocketdbDir, 0o755); err != nil {
		t.Fatalf("mkdir pocketdb: %v", err)
	}
	// "synth/" lives at datadir level (sibling of pocketdb/) so the promoted
	// live file (synth/pages.db) is exempt from the post-apply integrity_check.
	datadir := filepath.Dir(pocketdbDir)
	if err := os.MkdirAll(filepath.Join(datadir, "synth"), 0o755); err != nil {
		t.Fatalf("mkdir synth: %v", err)
	}

	planPath := filepath.Join(workdir, "plan.json")
	chunkHash, chunkContent := psGenerateLargeSQLitePlan(t, planPath, pocketdbDir, nPages)

	server := testhelpers.NewChunkStoreServer(t)
	server.AddChunk(chunkHash, chunkContent)

	// GC to stabilize the baseline reading. The plan.json is already on disk;
	// nothing page-sized is resident in the test process at this point.
	runtime.GC()
	hwmBefore, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM before: %v", err)
	}

	var stderr bytes.Buffer
	code, aerr := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          4,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})

	hwmAfter, herr := readVmHWMKB()
	if herr != nil {
		t.Fatalf("read VmHWM after: %v", herr)
	}
	deltaKB := hwmAfter - hwmBefore
	t.Logf("VmHWM before=%d KB after=%d KB delta=%d KB (%d MiB) for %d-page apply",
		hwmBefore, hwmAfter, deltaKB, deltaKB/1024, nPages)

	if aerr != nil {
		t.Fatalf("apply.Run: %v\nstderr: %s", aerr, stderr.String())
	}
	if code != exitcode.Success {
		t.Fatalf("apply.Run exit=%d, want Success\nstderr (tail): %s", code, tailString(stderr.String(), 2000))
	}

	if deltaKB*1024 > rssThreshold {
		t.Errorf("apply peak-RSS delta %d MiB exceeds %d MiB bound for %d-page workload\n"+
			"The materializing consumer reads the whole plan.json and holds every page "+
			"in a plan.Plan for the run; the streaming consumer must hold O(1) in pages.",
			deltaKB/1024, rssThreshold/(1024*1024), nPages)
	}
}

// tailString returns the last n bytes of s (avoids dumping a multi-MB stderr).
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
