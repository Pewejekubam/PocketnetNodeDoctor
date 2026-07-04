// T048: apply returns SupersededCanonical when the live manifest hash doesn't
// match the plan's canonical_identity.manifest_hash.
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

func TestApply_SupersededCanonical(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Plan's canonical_identity uses a specific manifest_hash that the live
	// manifest will NOT match — the manifest server returns a manifest whose
	// SHA-256 is different from the plan's declared manifest_hash.
	const planManifestHash = "aaaa000000000000000000000000000000000000000000000000000000000000"

	relPath := "pocketdb/blocks/00000000.dat"
	hash := fixture.CanonicalHashes[relPath]

	pb := testhelpers.NewPlanBuilder(planManifestHash, 10005)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// Fake manifest server that serves a manifest whose content (and therefore
	// SHA-256) will not match planManifestHash.
	manifestServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve a minimal manifest JSON whose SHA-256 != planManifestHash
		body, _ := json.Marshal(map[string]any{
			"format_version": 1,
			"block_height":   99999,
			"manifest_hash":  "bbbb000000000000000000000000000000000000000000000000000000000000",
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(manifestServer.Close)

	// Chunk store server (should NOT be contacted)
	chunkServer := testhelpers.NewChunkStoreServer(t)

	var stderr bytes.Buffer
	code, runErr := apply.Run(context.Background(), apply.Options{
		PlanPath:    planPath,
		Parallel:    1,
		Logger:      stderrlog.NewWith(&stderr, true),
		Transport:   manifestServer.Client().Transport,
		ManifestURL: manifestServer.URL + "/manifest.json",
	})
	t.Log("apply stderr:", stderr.String())

	if code != exitcode.SupersededCanonical {
		t.Errorf("apply.Run exit code = %d, want %d (SupersededCanonical)", code, exitcode.SupersededCanonical)
	}
	_ = runErr

	// Assert stderr contains block height comparison
	stderrOutput := stderr.String()
	if !strings.Contains(stderrOutput, "block_height") && !strings.Contains(stderrOutput, "height") {
		t.Errorf("stderr does not mention block height; stderr:\n%s", stderrOutput)
	}

	// Assert no chunks were fetched
	if got := chunkServer.RequestCount(); got != 0 {
		t.Errorf("chunkServer.RequestCount() = %d, want 0 — chunk store should not have been contacted", got)
	}
}
