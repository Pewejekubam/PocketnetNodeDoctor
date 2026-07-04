// apply_rss_bounded_test.go — regression test for pocketnet-node-doctor-6er.
//
// Verifies that apply's peak resident memory stays bounded as the workload
// scales. Pre-refactor, apply materialized the full pending-tasks slice and
// pre-allocated a workload-sized taskCh buffer; the chunk-004 SC-007 drill
// captured peak RSS of ~5.4 GiB on a 16,384-page-divergence workload (sealed
// archive runs-archive/20260510T013245Z.tar.gz, apply-rss.log).
//
// The current streaming pipeline holds at most `parallel` tasks in flight.
// This test exercises a workload sized like a fraction of the drill (large
// enough that the old behavior would balloon, small enough to keep CI fast)
// and asserts the post-apply VmHWM delta stays under a generous bound.
//
// The threshold is deliberately loose (300 MiB delta). The historical
// regression was ~5.4 GiB; even a substantially worse-than-expected
// streaming implementation would not approach this bound, while a return to
// O(workload) materialization would blow through it.
//
// Linux-only: reads /proc/self/status. Skipped elsewhere.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_RSSBounded_LargeWholeFileDivergence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VmHWM measurement requires /proc/self/status (linux-only)")
	}
	if testing.Short() {
		t.Skip("skipping RSS-bounded test in -short mode")
	}

	const (
		fileCount    = 16384             // matches SC-007 drill scale (16K pages) — bounds zstd decoder churn at drill-equivalent fetch concurrency
		fileSize     = 4096              // 4 KiB each → 64 MiB of canonical data
		rssThreshold = 300 * 1024 * 1024 // 300 MiB delta upper bound
	)

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	if err := os.MkdirAll(pocketdbWork, 0o755); err != nil {
		t.Fatalf("mkdir pocketdb: %v", err)
	}
	// "synth/" lives at datadir level (sibling of pocketdb/) and is exempt
	// from the post-apply SQLite integrity_check + hash verification path
	// that would otherwise need a real SQLite fixture.
	datadir := filepath.Dir(pocketdbWork)
	synthDir := filepath.Join(datadir, "synth")
	if err := os.MkdirAll(synthDir, 0o755); err != nil {
		t.Fatalf("mkdir synth: %v", err)
	}

	// Register fileCount distinct chunks; live files are absent so apply
	// fetches each canonical chunk and promotes it via os.Rename.
	server := testhelpers.NewChunkStoreServer(t)
	pb := testhelpers.NewPlanBuilder("rss-bounded-manifest-hash", 1)
	pb.WithPocketDBPath(pocketdbWork)
	for i := 0; i < fileCount; i++ {
		content := bytes.Repeat([]byte{byte((i % 251) + 1)}, fileSize)
		// Vary the last 4 bytes by index so each file's hash is unique.
		content[fileSize-4] = byte(i)
		content[fileSize-3] = byte(i >> 8)
		content[fileSize-2] = byte(i >> 16)
		content[fileSize-1] = byte(i >> 24)
		sum := sha256.Sum256(content)
		hash := hex.EncodeToString(sum[:])
		server.AddChunk(hash, content)
		pb.AddWholeFileAbsent(fmt.Sprintf("synth/page-%05d", i), hash)
	}

	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	planData = nil

	// GC to stabilize the baseline reading.
	runtime.GC()
	hwmBefore, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM before: %v", err)
	}

	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          4,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	if err != nil {
		t.Fatalf("apply.Run: %v\nstderr: %s", err, stderr.String())
	}
	if code != exitcode.Success {
		t.Fatalf("apply.Run exit=%d, want Success\nstderr: %s", code, stderr.String())
	}

	hwmAfter, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM after: %v", err)
	}
	deltaKB := hwmAfter - hwmBefore
	t.Logf("VmHWM before=%d KB after=%d KB delta=%d KB (%d MiB) for %d-file divergence",
		hwmBefore, hwmAfter, deltaKB, deltaKB/1024, fileCount)

	if deltaKB*1024 > rssThreshold {
		t.Errorf("apply peak-RSS delta %d MiB exceeds %d MiB bound for %d-file workload\n"+
			"This suggests apply has regressed to O(workload) memory; the streaming "+
			"pipeline should hold ≤ parallel tasks in flight regardless of plan size.",
			deltaKB/1024, rssThreshold/(1024*1024), fileCount)
	}
}

// readVmHWMKB returns the high-water-mark resident set size (peak RSS) from
// /proc/self/status, in kilobytes. Linux-specific.
func readVmHWMKB() (int64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		// Format: "VmHWM:\t  12345 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed VmHWM line: %q", line)
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmHWM kB %q: %w", fields[1], err)
		}
		return n, nil
	}
	return 0, fmt.Errorf("VmHWM not found in /proc/self/status")
}
