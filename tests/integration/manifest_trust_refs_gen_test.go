// manifest_trust_refs_gen_test.go — SC-004 reference generator (T001–T003).
//
// Materialises the five committed fixture manifests + their matching local
// pocketdb state, runs the *unchanged* diagnose binary over each via an
// in-process httptest server, and commits the normalised plan.json outputs as
// the SC-004 references, plus a PROVENANCE.md recording the generating commit
// and command line.
//
// This generator is gated behind MANIFEST_TRUST_GEN=1 so it never runs in the
// normal suite — it is run once, by hand, against the merge-base tree, and its
// output (fixtures/, refs/, PROVENANCE.md) is committed. The byte-equivalence
// test (TestTrustBoundary_SC004_ByteEquivalence) consumes those committed
// artifacts.
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

func mtCanonicalSQLite() []byte {
	const nPages = 6
	b := make([]byte, nPages*mtPageSize)
	for i := 0; i < nPages; i++ {
		for j := 0; j < mtPageSize; j++ {
			b[i*mtPageSize+j] = byte(i + 1)
		}
	}
	// Cosmetic SQLite header magic (the page-hash comparison is content-based).
	copy(b[0:16], []byte("SQLite format 3\x00"))
	return b
}

func mtCanonicalWholeFile() []byte {
	b := make([]byte, 5000)
	for i := range b {
		b[i] = byte((i % 97) + 1)
	}
	return b
}

// mtWriteFixture writes one fixture's manifest.json and local pocketdb state to
// disk under fixtures/<class>/, returning the manifest bytes and trust-root.
func mtWriteFixture(t *testing.T, class string, m manifest.Manifest, pocketdbFiles map[string][]byte) (string, string) {
	t.Helper()
	body, trustRoot := mtBuildCanonicalManifest(t, m)
	classDir := filepath.Join(mtFixturesDir(), class)
	pocketdbDir := filepath.Join(classDir, "pocketdb")
	if err := os.MkdirAll(pocketdbDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pocketdbDir, err)
	}
	if err := os.WriteFile(filepath.Join(classDir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for rel, content := range pocketdbFiles {
		dst := filepath.Join(pocketdbDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}
	return pocketdbDir, trustRoot
}

func mtFixtureSpec(t *testing.T, class string) (manifest.Manifest, map[string][]byte) {
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
	case "sqlite_pages_matching":
		return base(200, []manifest.Entry{sqliteEntry}), map[string][]byte{"main.sqlite3": canonicalSQLite}
	case "whole_file_divergent":
		local := append([]byte(nil), canonicalWhole...)
		local[0] ^= 0xFF // differ
		return base(300, []manifest.Entry{wholeEntry}), map[string][]byte{"data/file.bin": local}
	case "whole_file_matching":
		return base(400, []manifest.Entry{wholeEntry}), map[string][]byte{"data/file.bin": canonicalWhole}
	case "empty_divergence":
		return base(500, []manifest.Entry{sqliteEntry, wholeEntry}), map[string][]byte{
			"main.sqlite3":  canonicalSQLite,
			"data/file.bin": canonicalWhole,
		}
	}
	t.Fatalf("unknown fixture class %q", class)
	return manifest.Manifest{}, nil
}

func TestGenerateManifestTrustReferences(t *testing.T) {
	if os.Getenv("MANIFEST_TRUST_GEN") != "1" {
		t.Skip("reference generator: set MANIFEST_TRUST_GEN=1 to materialise fixtures + refs")
	}
	if err := os.MkdirAll(mtRefsDir(), 0o755); err != nil {
		t.Fatalf("mkdir refs: %v", err)
	}

	commit := "unknown"
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	for _, class := range mtFixtureClasses {
		m, files := mtFixtureSpec(t, class)
		pocketdbDir, trustRoot := mtWriteFixture(t, class, m, files)

		code, err, stderr, _, raw := mtRunDiagnose(t, mustReadFile(t, filepath.Join(mtFixturesDir(), class, "manifest.json")), trustRoot, pocketdbDir)
		if err != nil || code != exitcode.Success {
			t.Fatalf("class %s: diagnose code=%d err=%v stderr=%q", class, code, err, stderr)
		}
		if raw == nil {
			t.Fatalf("class %s: no plan.json emitted", class)
		}
		norm := mtNormalizePlan(t, raw)
		if err := os.WriteFile(filepath.Join(mtRefsDir(), class+".plan.json"), norm, 0o644); err != nil {
			t.Fatalf("class %s: write ref: %v", class, err)
		}
		t.Logf("class %s: ref written (%d bytes, trust-root %s)", class, len(norm), trustRoot[:12])
	}

	prov := fmt.Sprintf(`# SC-004 Reference Provenance — Manifest Trust Boundary (Chunk 1)

These reference `+"`plan.json`"+` outputs were generated by the **unchanged**
(pre-spool-verify-parse) diagnose binary at the integration target's merge-base,
so the post-change binary's output must match them byte-for-byte (after path
normalisation — see below). This pins SC-004.

## Generating commit

    %s

## Command line

    MANIFEST_TRUST_GEN=1 go test ./tests/integration/ -run TestGenerateManifestTrustReferences -v

## Fixture classes (SC-004 vacuity guard)

The five outcome classes span both entry kinds and the empty divergence set so
byte-equivalence cannot pass vacuously:

- sqlite_pages_divergent — sqlite_pages entry, two divergent local pages
- sqlite_pages_matching  — sqlite_pages entry, local matches canonical (zero divergences)
- whole_file_divergent   — whole_file entry, local differs (present-but-different)
- whole_file_matching    — whole_file entry, local matches canonical (zero divergences)
- empty_divergence       — both kinds, all matching (zero divergences)

## Normalisation

Three fields are inputs echoed into the plan rather than products of the parse,
and each is environment-specific: `+"`manifest_url`"+` (random httptest port),
`+"`pocketdb_path`"+` (absolute checkout-specific path), and `+"`self_hash`"+`
(computed over both). All three are replaced with stable tokens
(`+"`<MANIFEST_URL>`"+`, `+"`<POCKETDB>`"+`, `+"`<SELFHASH>`"+`) in the committed
references and in the post-change output before comparison. Every other byte —
format_version, canonical_identity, divergences, ordering — is compared
verbatim, and the post-change plan's self_hash is verified independently via
plan.VerifySelfHash. This is the faithful SC-004 basis: the rewrite moves only
the manifest byte source, never the plan output.
`, commit)
	if err := os.WriteFile(filepath.Join(mtFixturesDir(), "..", "PROVENANCE.md"), []byte(prov), 0o644); err != nil {
		t.Fatalf("write provenance: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
