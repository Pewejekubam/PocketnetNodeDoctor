package diagnose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// T061: EC-001 — every canonical entry becomes a whole_file divergence
// with expected_source: "fetch_full" when local file is missing.
func TestCompareFile_MissingLocal_FetchFull(t *testing.T) {
	root := t.TempDir() // empty pocketdb
	entry := manifest.Entry{
		EntryKind: manifest.EntryKindWholeFile,
		Path:      "blocks/000000.dat",
		Hash:      "0000000000000000000000000000000000000000000000000000000000000003",
	}
	div, divergent, err := CompareFile(root, entry)
	if err != nil {
		t.Fatalf("CompareFile: %v", err)
	}
	if !divergent {
		t.Fatalf("missing file must be divergent")
	}
	if div.Kind != plan.DivergenceKindWholeFile {
		t.Errorf("kind got %q", div.Kind)
	}
	if div.ExpectedSource != "fetch_full" {
		t.Errorf("expected_source got %q want fetch_full", div.ExpectedSource)
	}
	if div.ExpectedHash != entry.Hash {
		t.Errorf("expected_hash got %q want %q", div.ExpectedHash, entry.Hash)
	}
}

// T062: EC-002 — partial pocketdb. Files present-but-divergent use the
// normal divergence shape (no expected_source); missing files use fetch_full.
func TestCompareFile_PartialPocketdb_PresentDivergent_NoFetchFull(t *testing.T) {
	// pocketnet-core layout: pocketdbRoot is the pocketdb/ subdirectory;
	// blocks/ lives at the parent (datadir) level.
	datadir := t.TempDir()
	pocketdbRoot := filepath.Join(datadir, "pocketdb")
	if err := mkdirAll(pocketdbRoot); err != nil {
		t.Fatal(err)
	}
	// Write an actual file with non-matching content at the datadir level.
	path := filepath.Join(datadir, "blocks/000000.dat")
	if err := mkdirAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("local-content-differs")); err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entry{
		EntryKind: manifest.EntryKindWholeFile,
		Path:      "blocks/000000.dat",
		Hash:      "0000000000000000000000000000000000000000000000000000000000000003",
	}
	div, divergent, err := CompareFile(pocketdbRoot, entry)
	if err != nil {
		t.Fatalf("CompareFile: %v", err)
	}
	if !divergent {
		t.Fatalf("present-but-divergent must be divergent")
	}
	if div.ExpectedSource != "" {
		t.Errorf("present-but-divergent must NOT carry expected_source; got %q", div.ExpectedSource)
	}
}

func mkdirAll(p string) error            { return os.MkdirAll(p, 0o700) }
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o600) }
