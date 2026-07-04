// plan_stream_refs_gen_test.go — SC-003 fixture spec + reference generator
// (011-010 chunk-1, tasks T002/T007/T009).
//
// psFixtureSpec is the authoritative, reviewable "input spec" for the five
// SC-003 byte-identity fixture classes (task T002). Both the reference
// generator (this file) and the byte-identity consumer
// (plan_stream_byte_identity_test.go) materialise fixtures from it into a
// tempdir — no opaque binary fixtures are committed, so there is no
// git-drops-empty-dir hazard for the fetch_full class (whose local pocketdb is
// intentionally absent).
//
// The generator runs the *merge-base* (materializing) diagnose binary over each
// fixture and commits the normalised plan.json outputs as the SC-003 references
// (refs/<class>.plan.json) plus a README.md provenance file. It is gated behind
// PLAN_STREAM_GEN=1 so it never runs in the normal suite; after the streaming
// emitter lands, TestPlanStream_ByteIdentity asserts the streaming binary
// reproduces these bytes exactly.
//
// Provenance: the production tree on this branch is byte-identical to the
// pre-spec merge-base f97fe5b (`git diff --stat f97fe5b..HEAD -- internal cmd
// tests go.mod go.sum` is empty), so the HEAD binary IS the materializing
// merge-base binary for reference capture.
//
// Reuses the mt* helpers from manifest_trust_common_test.go
// (mtBuildCanonicalManifest, mtPageHashes, mtWholeFileHash, mtRunDiagnose,
// mtNormalizePlan, mtCanonicalSQLite, mtCanonicalWholeFile).
package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

func psRefsDir() string { return filepath.Join("testdata", "plan-stream", "refs") }

// psFixtureClasses are the five SC-003 byte-identity classes. The set spans
// both divergence kinds, both whole_file sub-shapes (fetch_full carries
// expected_source; present-but-different does not), a mixed-kind plan whose
// manifest publishes the whole_file entry BEFORE the sqlite_pages entry (so
// detection order != frozen emission order — a manifest-order emitter cannot
// pass), and the empty divergence set (byte-equivalence cannot pass vacuously).
var psFixtureClasses = []string{
	"sqlite_pages_divergent",
	"whole_file_fetch_full",
	"whole_file_present_diff",
	"mixed_kind",
	"zero_divergence",
}

// psFixtureSpec returns the manifest + local pocketdb state for one class.
func psFixtureSpec(t *testing.T, class string) (manifest.Manifest, map[string][]byte) {
	t.Helper()
	canonicalSQLite := mtCanonicalSQLite()
	canonicalWhole := mtCanonicalWholeFile()
	cc := int64(50)

	sqliteEntry := manifest.Entry{
		EntryKind:     manifest.EntryKindSQLitePages,
		Path:          "pocketdb/main.sqlite3",
		ChangeCounter: &cc,
		Pages:         mtPageHashes(canonicalSQLite),
	}
	wholeEntry := manifest.Entry{
		EntryKind: manifest.EntryKindWholeFile,
		Path:      "pocketdb/data/file.bin",
		Hash:      mtWholeFileHash(canonicalWhole),
	}
	id := func(bh int64) manifest.CanonicalIdentity {
		return manifest.CanonicalIdentity{
			BlockHeight:          bh,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		}
	}
	base := func(bh int64, entries []manifest.Entry) manifest.Manifest {
		return manifest.Manifest{
			FormatVersion:     1,
			CanonicalIdentity: id(bh),
			Entries:           entries,
			TrustAnchors:      json.RawMessage(`[]`),
		}
	}

	switch class {
	case "sqlite_pages_divergent":
		local := append([]byte(nil), canonicalSQLite...)
		for j := 0; j < mtPageSize; j++ { // mutate pages 2 and 4
			local[2*mtPageSize+j] = 0xAA
			local[4*mtPageSize+j] = 0xBB
		}
		return base(100, []manifest.Entry{sqliteEntry}), map[string][]byte{"main.sqlite3": local}
	case "whole_file_fetch_full":
		// local data/file.bin ABSENT -> whole_file divergence carries
		// expected_source: "fetch_full".
		return base(200, []manifest.Entry{wholeEntry}), map[string][]byte{}
	case "whole_file_present_diff":
		// local present but different -> whole_file divergence, NO expected_source.
		local := append([]byte(nil), canonicalWhole...)
		local[0] ^= 0xFF
		return base(300, []manifest.Entry{wholeEntry}), map[string][]byte{"data/file.bin": local}
	case "mixed_kind":
		// Manifest publishes whole_file BEFORE sqlite_pages; both diverge.
		// Emitted plan must place the sqlite_pages group first (frozen order),
		// proving emission order != manifest/detection order. data/file.bin
		// absent -> fetch_full whole_file; main.sqlite3 mutated -> sqlite_pages.
		local := append([]byte(nil), canonicalSQLite...)
		for j := 0; j < mtPageSize; j++ { // mutate pages 1 and 3
			local[1*mtPageSize+j] = 0xCC
			local[3*mtPageSize+j] = 0xDD
		}
		return base(400, []manifest.Entry{wholeEntry, sqliteEntry}), map[string][]byte{"main.sqlite3": local}
	case "zero_divergence":
		return base(500, []manifest.Entry{sqliteEntry, wholeEntry}), map[string][]byte{
			"main.sqlite3":  canonicalSQLite,
			"data/file.bin": canonicalWhole,
		}
	}
	t.Fatalf("unknown plan-stream fixture class %q", class)
	return manifest.Manifest{}, nil
}

// psMaterializeFixture writes the class's local pocketdb state under baseDir and
// returns the canonform manifest body, its trust-root (sha256 hex over the
// served body), and the pocketdb dir. The manifest is passed to diagnose
// directly (served in-process), never written to disk.
func psMaterializeFixture(t *testing.T, baseDir, class string) (manifestBody []byte, pinned, pocketdbDir string) {
	t.Helper()
	m, files := psFixtureSpec(t, class)
	body, trustRoot := mtBuildCanonicalManifest(t, m)
	pocketdbDir = filepath.Join(baseDir, "pocketdb")
	if err := os.MkdirAll(pocketdbDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pocketdbDir, err)
	}
	for rel, content := range files {
		dst := filepath.Join(pocketdbDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}
	return body, trustRoot, pocketdbDir
}

func TestGeneratePlanStreamReferences(t *testing.T) {
	if os.Getenv("PLAN_STREAM_GEN") != "1" {
		t.Skip("reference generator: set PLAN_STREAM_GEN=1 to (re)materialise refs")
	}
	if err := os.MkdirAll(psRefsDir(), 0o755); err != nil {
		t.Fatalf("mkdir refs: %v", err)
	}

	commit := "unknown"
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	for _, class := range psFixtureClasses {
		manifestBody, pinned, pocketdbDir := psMaterializeFixture(t, t.TempDir(), class)
		code, err, stderr, _, raw := mtRunDiagnose(t, manifestBody, pinned, pocketdbDir)
		if err != nil || code != exitcode.Success {
			t.Fatalf("class %s: diagnose code=%d err=%v stderr=%q", class, code, err, stderr)
		}
		if raw == nil {
			t.Fatalf("class %s: no plan.json emitted", class)
		}
		norm := mtNormalizePlan(t, raw)
		if err := os.WriteFile(filepath.Join(psRefsDir(), class+".plan.json"), norm, 0o644); err != nil {
			t.Fatalf("class %s: write ref: %v", class, err)
		}
		t.Logf("class %s: ref written (%d bytes)", class, len(norm))
	}

	readme := fmt.Sprintf(`# SC-003 Byte-Identity References — Plan-Scale Streaming (Chunk 1)

These reference `+"`plan.json`"+` outputs were generated by the **merge-base
(materializing)** diagnose binary — the implementation before the plan-scale
streaming change. After the streaming emission rewrite lands, the streaming
binary's output must match them **byte-for-byte** (after path normalisation),
which is the SC-003 gate (TestPlanStream_ByteIdentity).

## Generating commit

    %s

Production-tree provenance: `+"`git diff --stat f97fe5b..HEAD -- internal cmd tests go.mod go.sum`"+`
is **empty** on this branch, so the HEAD tree above is byte-identical to the
pre-spec merge-base `+"`f97fe5b`"+` for all production code — the HEAD binary is
the materializing merge-base binary.

## Command line

    PLAN_STREAM_GEN=1 go test ./tests/integration/ -run TestGeneratePlanStreamReferences -v

## Fixture inputs

The five fixture inputs are defined deterministically in code by psFixtureSpec
(plan_stream_refs_gen_test.go) — the authoritative, reviewable input spec. Both
the generator and the byte-identity consumer materialise them into a tempdir, so
no opaque binary fixtures are committed:

- sqlite_pages_divergent  — sqlite_pages entry, two divergent local pages
- whole_file_fetch_full   — whole_file entry, local ABSENT -> divergence carries expected_source: fetch_full
- whole_file_present_diff — whole_file entry, local present-but-different -> divergence WITHOUT expected_source
- mixed_kind              — manifest publishes whole_file BEFORE sqlite_pages; emitted plan places the sqlite_pages group first (frozen emission order != detection order). A manifest-order emitter cannot pass.
- zero_divergence         — both kinds, all matching (empty divergences)

## Normalisation

Three fields are inputs echoed into the plan rather than products of emission,
and each is environment-specific: `+"`manifest_url`"+` (random httptest port),
`+"`pocketdb_path`"+` (absolute checkout-specific path), and `+"`self_hash`"+`
(computed over both). All three are replaced with stable tokens
(`+"`<MANIFEST_URL>`, `<POCKETDB>`, `<SELFHASH>`"+`) in the committed references
and in the post-change output before comparison (mtNormalizePlan). Every other
byte — format_version, canonical_identity, divergences, ordering — is compared
verbatim, and the post-change plan's self_hash is verified independently via
plan.VerifySelfHash.

## SC-001 / SC-002 red figures (merge-base, materializing)

Captured by the RSS harnesses (plan_stream_diagnose_rss_test.go,
plan_stream_apply_rss_test.go) at >= 2,000,000 sqlite_pages page entries against
this merge-base tree. Recorded BEFORE any streaming production change lands
(vacuity guard):

- SC-001 diagnose whole-run peak RSS delta: <RECORD from merge-base run> (bound post-change: < 300 MiB)
- SC-002 apply whole-run peak RSS delta:    <RECORD from merge-base run> (bound post-change: < 300 MiB)

## Bead 9v9 / p5m merge-base baselines (SC-005 / SC-006 red)

- 9v9: an intact format_version 1 plan is reported as **tampered (exit 15)** by
  the merge-base binary instead of version-unrecognized (exit 7). Post-change: exit 7.
- p5m: a v2 plan carrying a malformed page/divergence expected_hash (whose
  self_hash is recomputed over the malformed bytes so it passes the tamper gate)
  **panics** the merge-base binary in the divergenceTaskIter h[0:2] slice.
  Post-change: a typed, entry-naming refusal (exit 1), no panic.
`, commit)
	if err := os.WriteFile(filepath.Join(psRefsDir(), "..", "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
}
