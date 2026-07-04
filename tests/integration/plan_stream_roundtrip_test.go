// plan_stream_roundtrip_test.go — SC-004 round-trip + tamper (011-010 chunk-1,
// task T019).
//
// A canonform (streaming-emitted-equivalent, byte-identical per SC-003) plan
// verifies under the streaming apply consumer; a single-byte splice that leaves
// the plan well-formed v2 is refused as tampered (exit 15) with ZERO
// staging-state mutation; a structure-breaking corruption is refused as
// plan-invalid (exit 1).
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

// psWholeFilePlan builds a workdir with one divergent whole_file (not
// main.sqlite3, so no SQLite integrity_check), a chunk store serving the
// canonical content, and a canonform plan.json. Returns paths + the valid bytes.
func psWholeFilePlan(t *testing.T) (planPath, workdir string, planData []byte, server *testhelpers.ChunkStoreServer) {
	t.Helper()
	workdir = t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	if err := os.MkdirAll(filepath.Join(pocketdbWork, "blocks"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Live file exists but diverges from canonical.
	livePath := filepath.Join(pocketdbWork, "blocks", "00000000.dat")
	if err := os.WriteFile(livePath, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := []byte("canonical replacement content")
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])

	server = testhelpers.NewChunkStoreServer(t)
	server.AddChunk(hash, canonical)

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-rt", 10010)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", hash)
	planData = pb.Build(t)

	planPath = filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatal(err)
	}
	return planPath, workdir, planData, server
}

func TestApply_StreamRoundTrip(t *testing.T) {
	planPath, workdir, _, server := psWholeFilePlan(t)

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
		t.Fatalf("round-trip exit = %d, want Success\nstderr: %s", code, stderr.String())
	}
	// Live file now matches canonical.
	got, _ := os.ReadFile(filepath.Join(workdir, "pocketdb", "blocks", "00000000.dat"))
	if string(got) != "canonical replacement content" {
		t.Errorf("live file = %q, want canonical replacement", got)
	}
}

func TestApply_StreamTamper_WellFormed(t *testing.T) {
	planPath, workdir, planData, server := psWholeFilePlan(t)

	// Splice: flip one char of the divergence expected_hash. Still well-formed v2
	// JSON, but the self_hash no longer covers these bytes.
	marker := []byte(`"expected_hash":"`)
	i := bytes.Index(planData, marker)
	if i < 0 {
		t.Fatal("expected_hash not found")
	}
	pos := i + len(marker)
	tampered := append([]byte(nil), planData...)
	if tampered[pos] == 'a' {
		tampered[pos] = 'b'
	} else {
		tampered[pos] = 'a'
	}
	if err := os.WriteFile(planPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          4,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	if code != exitcode.PlanTampered {
		t.Fatalf("tamper exit = %d, want %d (PlanTampered)\nstderr: %s", code, exitcode.PlanTampered, stderr.String())
	}
	if got := server.RequestCount(); got != 0 {
		t.Errorf("chunk store contacted %d times on a tampered plan, want 0", got)
	}
	// Zero staging-state mutation: staging dir never created.
	if _, statErr := os.Stat(filepath.Join(workdir, "pocketnet-node-doctor-staging")); statErr == nil {
		t.Error("staging directory exists but must not for a tampered plan (zero staging mutation)")
	}
}

func TestApply_StreamTamper_StructureBreaking(t *testing.T) {
	planPath, workdir, planData, server := psWholeFilePlan(t)

	// Structure-breaking: flip the opening brace so the top-level object is not an
	// object → decode failure (class 1 → exit 1), before any side effect.
	tampered := append([]byte(nil), planData...)
	tampered[0] = '['
	if err := os.WriteFile(planPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          4,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	if code != exitcode.GenericError {
		t.Fatalf("structure-break exit = %d, want %d (GenericError)\nstderr: %s", code, exitcode.GenericError, stderr.String())
	}
	if _, statErr := os.Stat(filepath.Join(workdir, "pocketnet-node-doctor-staging")); statErr == nil {
		t.Error("staging directory exists but must not for a plan-invalid refusal")
	}
}
