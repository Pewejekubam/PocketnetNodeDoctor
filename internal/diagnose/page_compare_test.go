package diagnose

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// page_compare unit tests — T081 implementation, exercised via integration
// elsewhere; here we cover the discrete shapes (matching, divergent, missing,
// short).
func TestComparePages_AllMatch_NoDivergent(t *testing.T) {
	root := t.TempDir()
	pages := writeSyntheticPages(t, root, 4)
	entry := manifestEntryFromPages(pages)
	out, err := ComparePages(root, entry)
	if err != nil {
		t.Fatalf("ComparePages: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want zero divergent; got %d", len(out))
	}
}

func TestComparePages_OnePageDivergent(t *testing.T) {
	root := t.TempDir()
	pages := writeSyntheticPages(t, root, 4)
	entry := manifestEntryFromPages(pages)
	// Mutate page 1's expected hash so local differs from "canonical".
	entry.Pages[1].Hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	out, err := ComparePages(root, entry)
	if err != nil {
		t.Fatalf("ComparePages: %v", err)
	}
	if len(out) != 1 || out[0].Offset != 4096 {
		t.Errorf("want 1 divergent at offset 4096; got %+v", out)
	}
}

func TestComparePages_FileMissing_AllPagesDivergent(t *testing.T) {
	root := t.TempDir() // no main.sqlite3
	entry := manifest.Entry{
		EntryKind: manifest.EntryKindSQLitePages,
		Path:      "pocketdb/main.sqlite3",
		Pages: []manifest.Page{
			{Offset: 0, Hash: "0000000000000000000000000000000000000000000000000000000000000001"},
			{Offset: 4096, Hash: "0000000000000000000000000000000000000000000000000000000000000002"},
		},
	}
	out, err := ComparePages(root, entry)
	if err != nil {
		t.Fatalf("ComparePages: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("want 2 divergent (all canonical pages); got %d", len(out))
	}
}

func writeSyntheticPages(t *testing.T, dir string, n int) []manifest.Page {
	t.Helper()
	const pageSize = 4096
	path := filepath.Join(dir, "main.sqlite3")
	body := make([]byte, n*pageSize)
	pages := make([]manifest.Page, n)
	for i := 0; i < n; i++ {
		page := body[i*pageSize : (i+1)*pageSize]
		for j := range page {
			page[j] = byte(i)
		}
		sum := sha256.Sum256(page)
		pages[i] = manifest.Page{Offset: int64(i * pageSize), Hash: hex.EncodeToString(sum[:])}
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return pages
}

func manifestEntryFromPages(p []manifest.Page) manifest.Entry {
	cc := int64(0)
	return manifest.Entry{
		EntryKind:     manifest.EntryKindSQLitePages,
		Path:          "pocketdb/main.sqlite3",
		ChangeCounter: &cc,
		Pages:         p,
	}
}
