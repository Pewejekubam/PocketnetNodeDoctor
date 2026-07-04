// diagnose_wrong_file_pages_test.go — pocketnet-node-doctor-cpm: a manifest
// carrying a sqlite_pages entry for a file OTHER than pocketdb/main.sqlite3
// (the manifest schema anticipates e.g. pocketdb/web.sqlite3) must make
// diagnose refuse, not silently compare that entry's pages against
// main.sqlite3 and emit a plan that splices the wrong file's canonical pages
// into main.sqlite3.
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// mustWriteFile writes content at root/rel, creating parent directories.
func mustWriteFile(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func TestDiagnose_SecondSQLitePagesEntry_RefusedNotMisattributed(t *testing.T) {
	canonicalMain := mtCanonicalSQLite()

	// Canonical web.sqlite3 content: same page count, different bytes — its
	// page hashes match neither canonicalMain nor the local web copy below.
	canonicalWeb := make([]byte, len(canonicalMain))
	for i := range canonicalWeb {
		canonicalWeb[i] = byte((i % 251) + 2)
	}
	copy(canonicalWeb[0:16], []byte("SQLite format 3\x00"))

	cc := int64(50)
	m := manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          600,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{
			{
				EntryKind:     manifest.EntryKindSQLitePages,
				Path:          "pocketdb/main.sqlite3",
				ChangeCounter: &cc,
				Pages:         mtPageHashes(canonicalMain),
			},
			{
				EntryKind: manifest.EntryKindSQLitePages,
				Path:      "pocketdb/web.sqlite3",
				Pages:     mtPageHashes(canonicalWeb),
			},
		},
		TrustAnchors: []byte(`[]`),
	}
	body, trustRoot := mtBuildCanonicalManifest(t, m)

	// Local pocketdb: main.sqlite3 identical to canonical (zero genuine main
	// divergence); web.sqlite3 present with arbitrary non-canonical content.
	// Under the bug, entry 2's pages are compared against main.sqlite3 and
	// every mismatching page becomes a main.sqlite3 divergence — a plan that
	// would splice web.sqlite3's canonical pages into main.sqlite3.
	pocketdbDir := t.TempDir()
	mustWriteFile(t, pocketdbDir, "main.sqlite3", canonicalMain)
	localWeb := append([]byte(nil), canonicalMain...) // any bytes ≠ canonicalWeb
	mustWriteFile(t, pocketdbDir, "web.sqlite3", localWeb)

	code, err, stderr, _, raw := mtRunDiagnose(t, body, trustRoot, pocketdbDir)

	if code == exitcode.Success {
		t.Fatalf("diagnose must refuse a manifest with a non-main sqlite_pages entry; got exit 0 (stderr=%q)", stderr)
	}
	if err == nil {
		t.Fatalf("want wrong-file attribution error; got nil")
	}
	if !strings.Contains(err.Error(), "pocketdb/web.sqlite3") || !strings.Contains(err.Error(), "pocketdb/main.sqlite3") {
		t.Errorf("error must name both the assumed and actual paths; got %v", err)
	}
	if raw != nil {
		t.Errorf("no plan.json may be emitted on refusal; got %d bytes", len(raw))
	}
}
