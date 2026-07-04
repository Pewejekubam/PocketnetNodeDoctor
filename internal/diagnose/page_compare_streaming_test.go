package diagnose

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// collectStreamPages drains an iter.Seq2[plan.Page, error] feed into a slice,
// returning the first error encountered (if any). Verifies the D3 contract that
// ComparePagesStreaming yields divergent pages one at a time.
func collectStreamPages(seq iter.Seq2[plan.Page, error]) ([]plan.Page, error) {
	var out []plan.Page
	for pg, err := range seq {
		if err != nil {
			return out, err
		}
		out = append(out, pg)
	}
	return out, nil
}

// ascendingByOffset reports whether pages are in strictly ascending offset
// order (the no-terminal-sort invariant, DA4).
func ascendingByOffset(pages []plan.Page) bool {
	for i := 1; i < len(pages); i++ {
		if pages[i].Offset <= pages[i-1].Offset {
			return false
		}
	}
	return true
}

func sha256OfBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// pagesIterFromSlice adapts a []manifest.Page to iter.Seq2[manifest.Page, error]
// for streaming-comparator tests. Mirrors what FetchAndProcess emits per entry.
func pagesIterFromSlice(pages []manifest.Page) iter.Seq2[manifest.Page, error] {
	return func(yield func(manifest.Page, error) bool) {
		for _, p := range pages {
			if !yield(p, nil) {
				return
			}
		}
	}
}

// pagesIterError yields the prefix pages then an error, simulating a manifest-side
// iterator failure mid-stream.
func pagesIterError(prefix []manifest.Page, sentinel error) iter.Seq2[manifest.Page, error] {
	return func(yield func(manifest.Page, error) bool) {
		for _, p := range prefix {
			if !yield(p, nil) {
				return
			}
		}
		yield(manifest.Page{}, sentinel)
	}
}

// 8tc-A: all-match — streaming comparator yields zero divergent pages.
func TestComparePagesStreaming_AllMatch_NoDivergent(t *testing.T) {
	root := t.TempDir()
	pages := writeSyntheticPages(t, root, 4)
	out, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterFromSlice(pages)))
	if err != nil {
		t.Fatalf("ComparePagesStreaming: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want zero divergent; got %d (%+v)", len(out), out)
	}
}

// 8tc-B: one page divergent — same shape as TestComparePages_OnePageDivergent.
func TestComparePagesStreaming_OnePageDivergent(t *testing.T) {
	root := t.TempDir()
	pages := writeSyntheticPages(t, root, 4)
	pages[1].Hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	out, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterFromSlice(pages)))
	if err != nil {
		t.Fatalf("ComparePagesStreaming: %v", err)
	}
	if len(out) != 1 || out[0].Offset != 4096 {
		t.Errorf("want 1 divergent at offset 4096; got %+v", out)
	}
	if out[0].ExpectedHash != pages[1].Hash {
		t.Errorf("ExpectedHash got %q want %q", out[0].ExpectedHash, pages[1].Hash)
	}
}

// 8tc-C: file missing — every manifest page becomes divergent, yielded
// ascending-by-offset. The feed MUST drain the manifest iterator (not require
// the caller to) so the upstream FetchAndProcess decoder stays aligned.
func TestComparePagesStreaming_FileMissing_AllPagesDivergent(t *testing.T) {
	root := t.TempDir() // no main.sqlite3
	pages := []manifest.Page{
		{Offset: 0, Hash: "0000000000000000000000000000000000000000000000000000000000000001"},
		{Offset: 4096, Hash: "0000000000000000000000000000000000000000000000000000000000000002"},
		{Offset: 8192, Hash: "0000000000000000000000000000000000000000000000000000000000000003"},
	}
	out, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterFromSlice(pages)))
	if err != nil {
		t.Fatalf("ComparePagesStreaming: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 divergent (all canonical pages); got %d", len(out))
	}
	if !ascendingByOffset(out) {
		t.Errorf("divergent pages not ascending-by-offset: %+v", out)
	}
	for i, want := range pages {
		if out[i].Offset != want.Offset || out[i].ExpectedHash != want.Hash {
			t.Errorf("divergent[%d] got {%d,%s} want {%d,%s}", i, out[i].Offset, out[i].ExpectedHash, want.Offset, want.Hash)
		}
	}
}

// 8tc-D: local file shorter than canonical — pages beyond the local file's end
// are reported as divergent.
func TestComparePagesStreaming_LocalShorter_TailDivergent(t *testing.T) {
	root := t.TempDir()
	// Local file has 2 pages; manifest claims 4.
	localPages := writeSyntheticPages(t, root, 2)
	canonicalPages := append([]manifest.Page{}, localPages...)
	canonicalPages = append(canonicalPages,
		manifest.Page{Offset: 8192, Hash: "00000000000000000000000000000000000000000000000000000000000000aa"},
		manifest.Page{Offset: 12288, Hash: "00000000000000000000000000000000000000000000000000000000000000bb"},
	)
	out, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterFromSlice(canonicalPages)))
	if err != nil {
		t.Fatalf("ComparePagesStreaming: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 divergent (local tail missing); got %d (%+v)", len(out), out)
	}
	if out[0].Offset != 8192 || out[1].Offset != 12288 {
		t.Errorf("divergent offsets got {%d,%d} want {8192,12288}", out[0].Offset, out[1].Offset)
	}
}

// 8tc-E: local file longer than canonical — extra local pages are ignored
// (apply will truncate). This requires the feed to break out of the local
// iterator early; verify the manifest iterator drain is graceful (no error,
// no panic from over-consuming).
func TestComparePagesStreaming_LocalLonger_ExtraIgnored(t *testing.T) {
	root := t.TempDir()
	// Local file has 4 pages; manifest claims only the first 2.
	localPages := writeSyntheticPages(t, root, 4)
	canonicalPages := localPages[:2]
	out, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterFromSlice(canonicalPages)))
	if err != nil {
		t.Fatalf("ComparePagesStreaming: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want zero divergent (local has extra pages but canonical is shorter); got %d (%+v)", len(out), out)
	}
}

// 8tc-F: error from manifest iterator surfaces through the Seq2 error slot.
func TestComparePagesStreaming_ManifestIterError_Propagated(t *testing.T) {
	root := t.TempDir() // no local file → missing-file branch drains manifest iter
	sentinel := errors.New("synthetic manifest iter failure")
	_, err := collectStreamPages(ComparePagesStreaming(root, "pocketdb/main.sqlite3", pagesIterError(nil, sentinel)))
	if err == nil {
		t.Fatalf("want error from manifest iterator; got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("want error wrapping sentinel; got %v", err)
	}
}

// CompareFileByPathHash: scalar-arg companion to CompareFile.
func TestCompareFileByPathHash_FileMissing_DivergentFetchFull(t *testing.T) {
	root := t.TempDir()
	div, divergent, err := CompareFileByPathHash(root, "blocks/000000.dat", "00000000000000000000000000000000000000000000000000000000000000aa")
	if err != nil {
		t.Fatalf("CompareFileByPathHash: %v", err)
	}
	if !divergent {
		t.Fatalf("want divergent=true for missing file")
	}
	if div.Path != "blocks/000000.dat" {
		t.Errorf("Path got %q", div.Path)
	}
	if div.ExpectedSource != "fetch_full" {
		t.Errorf("ExpectedSource got %q want fetch_full", div.ExpectedSource)
	}
}

func TestCompareFileByPathHash_FileMatches_NotDivergent(t *testing.T) {
	root := t.TempDir()
	// Write a known-content file under root/blocks/.
	blocksDir := filepath.Join(filepath.Dir(root), "blocks")
	// CompareFileByPathHash should resolve "blocks/..." relative to the parent
	// of pocketdbRoot per joinRel's semantics. Build the dir hierarchy that
	// reflects that: parent(root)/blocks/000000.dat
	if err := os.MkdirAll(blocksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(blocksDir) })
	body := []byte("hello")
	target := filepath.Join(blocksDir, "000000.dat")
	if err := os.WriteFile(target, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// Compute expected SHA-256 of body.
	hexHash := sha256OfBytes(body)
	_, divergent, err := CompareFileByPathHash(root, "blocks/000000.dat", hexHash)
	if err != nil {
		t.Fatalf("CompareFileByPathHash: %v", err)
	}
	if divergent {
		t.Errorf("want divergent=false for matching hash")
	}
}

func TestCompareFileByPathHash_HashMismatch_Divergent(t *testing.T) {
	root := t.TempDir()
	blocksDir := filepath.Join(filepath.Dir(root), "blocks")
	if err := os.MkdirAll(blocksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(blocksDir) })
	target := filepath.Join(blocksDir, "000000.dat")
	if err := os.WriteFile(target, []byte("local content"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongHash := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	div, divergent, err := CompareFileByPathHash(root, "blocks/000000.dat", wrongHash)
	if err != nil {
		t.Fatalf("CompareFileByPathHash: %v", err)
	}
	if !divergent {
		t.Fatalf("want divergent=true for hash mismatch")
	}
	if div.ExpectedHash != wrongHash {
		t.Errorf("ExpectedHash got %q", div.ExpectedHash)
	}
}
