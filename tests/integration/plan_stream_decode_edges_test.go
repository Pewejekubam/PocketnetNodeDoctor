// plan_stream_decode_edges_test.go — streaming-decode edge cases (011-010
// chunk-1, task T020): EC-008 non-canonform → tampered (15); EC-009 header
// pathologies (bad format_version → 1, bad self_hash → 15); EC-004 marker-resume
// idempotence; EC-007 duplicate entries reproduce materialized behavior.
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
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

// applyCode runs apply.Run over planPath and returns the exit code.
func applyCode(t *testing.T, planPath string, server *testhelpers.ChunkStoreServer) exitcode.Code {
	t.Helper()
	var stderr bytes.Buffer
	opts := apply.Options{
		PlanPath: planPath,
		Parallel: 4,
		Logger:   stderrlog.NewWith(&stderr, false),
	}
	if server != nil {
		opts.Transport = server.Transport()
		opts.ChunkStoreBaseURL = server.URL()
	}
	code, _ := apply.Run(context.Background(), opts)
	t.Logf("apply stderr: %s", stderr.String())
	return code
}

func writePlan(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// EC-009: format_version absent / duplicate / non-integer → plan-invalid (exit 1).
func TestApply_EC009_BadFormatVersion(t *testing.T) {
	ci := `"canonical_identity":{"block_height":1,"manifest_hash":"h","pocketnet_core_version":"v"}`
	cases := map[string]string{
		"absent":      `{` + ci + `,"divergences":[],"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"duplicate":   `{` + ci + `,"divergences":[],"format_version":2,"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"non_integer": `{` + ci + `,"divergences":[],"format_version":"2","manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"float":       `{` + ci + `,"divergences":[],"format_version":2.5,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if code := applyCode(t, writePlan(t, body), nil); code != exitcode.GenericError {
				t.Fatalf("%s: exit = %d, want %d (GenericError)", name, code, exitcode.GenericError)
			}
		})
	}
}

// EC-009: self_hash absent / duplicate / misplaced → tampered (exit 15).
func TestApply_EC009_BadSelfHash(t *testing.T) {
	ci := `"canonical_identity":{"block_height":1,"manifest_hash":"h","pocketnet_core_version":"v"}`
	cases := map[string]string{
		"absent":    `{` + ci + `,"divergences":[],"format_version":2,"manifest_url":"","pocketdb_path":"/p"}`,
		"duplicate": `{` + ci + `,"divergences":[],"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x","self_hash":"y"}`,
		"misplaced": `{` + ci + `,"divergences":[],"format_version":2,"manifest_url":"","self_hash":"x","pocketdb_path":"/p"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if code := applyCode(t, writePlan(t, body), nil); code != exitcode.PlanTampered {
				t.Fatalf("%s: exit = %d, want %d (PlanTampered)", name, code, exitcode.PlanTampered)
			}
		})
	}
}

// EC-008: a non-canonform plan (extra whitespace in the payload) is not
// byte-identical to what its self_hash covers → tampered (exit 15).
func TestApply_EC008_NonCanonform(t *testing.T) {
	_, _, planData, _ := psWholeFilePlan(t)
	// Insert an insignificant space after the opening brace — valid JSON, but the
	// bytes the self_hash covers no longer match.
	noncanon := append([]byte(nil), planData[:1]...)
	noncanon = append(noncanon, ' ')
	noncanon = append(noncanon, planData[1:]...)
	p := writePlan(t, string(noncanon))
	if code := applyCode(t, p, nil); code != exitcode.PlanTampered {
		t.Fatalf("non-canonform exit = %d, want %d (PlanTampered)", code, exitcode.PlanTampered)
	}
}

// EC-004: a second apply with all markers present resolves nothing further and
// issues no new fetches (marker-resume via the streamed shadow-scan/task-feed).
func TestApply_EC004_MarkerResumeIdempotent(t *testing.T) {
	planPath, _, _, server := psWholeFilePlan(t)

	if code := applyCode(t, planPath, server); code != exitcode.Success {
		t.Fatalf("first apply exit = %d, want Success", code)
	}
	afterFirst := server.RequestCount()
	if afterFirst == 0 {
		t.Fatal("first apply issued no fetches; fixture is not exercising a divergence")
	}
	if code := applyCode(t, planPath, server); code != exitcode.Success {
		t.Fatalf("resume apply exit = %d, want Success", code)
	}
	if got := server.RequestCount(); got != afterFirst {
		t.Errorf("resume issued %d new fetch(es); marker-resume must issue 0", got-afterFirst)
	}
}

// EC-007: the streaming decoder reproduces the materialized decoder's handling of
// duplicate entries — neither collapsing nor rejecting them (bead 6cv, neither
// fixed nor regressed). The assertion is at the decode layer this chunk changed;
// the downstream duplicate/concurrent-apply behavior lives in the unchanged
// worker pool and is inherently racy (6cv), so it is out of scope here.
func TestApply_EC007_DuplicateEntries(t *testing.T) {
	hash := strings.Repeat("a", 64)
	pb := testhelpers.NewPlanBuilder("dup-manifest", 10011)
	pb.WithPocketDBPath("/tmp/pocketdb")
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", hash)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", hash) // duplicate
	planData := pb.Build(t)

	// Materialized oracle: how plan.Unmarshal sees the duplicates.
	oracle, err := plan.Unmarshal(planData)
	if err != nil {
		t.Fatalf("plan.Unmarshal: %v", err)
	}
	// Streaming decoder: how the consumption path sees them.
	headers, err := plan.StreamDivergenceHeaders(bytes.NewReader(planData))
	if err != nil {
		t.Fatalf("StreamDivergenceHeaders: %v", err)
	}

	if len(headers) != len(oracle.Divergences) {
		t.Fatalf("streaming decoder saw %d divergences, materialized saw %d — duplicate handling diverged",
			len(headers), len(oracle.Divergences))
	}
	if len(headers) != 2 {
		t.Fatalf("duplicate entries: got %d divergences, want 2 (neither collapsed nor rejected)", len(headers))
	}
	for i := range headers {
		if headers[i].Path != oracle.Divergences[i].Path || headers[i].ExpectedHash != oracle.Divergences[i].ExpectedHash {
			t.Errorf("divergence[%d]: streaming %q/%q != materialized %q/%q", i,
				headers[i].Path, headers[i].ExpectedHash, oracle.Divergences[i].Path, oracle.Divergences[i].ExpectedHash)
		}
	}
}
