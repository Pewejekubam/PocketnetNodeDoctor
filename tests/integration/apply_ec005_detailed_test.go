// T067: apply EC-005 detailed block-height comparison — when the live manifest
// hash differs from the plan's manifest_hash, apply exits SupersededCanonical
// and stderr contains block height lines for both the plan and served canonical.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_EC005_BlockHeightComparison(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Plan uses a specific manifest_hash (all 'a's) that the manifest server
	// will NOT match — the served manifest has a different hash.
	const planManifestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const planBlockHeight = 10050

	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]

	pb := testhelpers.NewPlanBuilder(planManifestHash, planBlockHeight)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// Fake manifest server — serves a manifest at a different (higher) block height
	const servedBlockHeight = 10200
	fakeManifest := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"format_version": 1,
			"block_height":   servedBlockHeight,
			"manifest_hash":  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(fakeManifest.Close)

	var stderr bytes.Buffer
	// ManifestURL is a field on apply.Options that specifies the manifest
	// endpoint to verify against before applying. This field does not exist
	// yet — RED compile.
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:    planPath,
		Parallel:    1,
		Logger:      stderrlog.NewWith(&stderr, true),
		Transport:   fakeManifest.Client().Transport,
		ManifestURL: fakeManifest.URL + "/manifest.json",
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = SupersededCanonical (14)
	if code != exitcode.SupersededCanonical {
		t.Errorf("exit code = %d, want %d (SupersededCanonical)", code, exitcode.SupersededCanonical)
	}

	stderrStr := stderr.String()

	// Assert: stderr contains a "plan canonical block height" line
	if !strings.Contains(stderrStr, "plan canonical block height") {
		t.Errorf("stderr does not contain \"plan canonical block height\"\nstderr: %s", stderrStr)
	}

	// Assert: stderr contains a "served canonical block height" line
	if !strings.Contains(stderrStr, "served canonical block height") {
		t.Errorf("stderr does not contain \"served canonical block height\"\nstderr: %s", stderrStr)
	}
}
