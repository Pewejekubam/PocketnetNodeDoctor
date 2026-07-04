package hashutil

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// T011: HashSQLitePages yields {offset, hash} per page; offset non-negative
// multiple of pageSize; sorted ascending; compares against expected.txt.
func TestHashSQLitePages_MatchesFixture(t *testing.T) {
	expected := readExpectedPages(t)
	path := filepath.Join("testdata", "sqlite_pages.bin")
	const pageSize = 4096
	seq, err := HashSQLitePages(path, pageSize)
	if err != nil {
		t.Fatalf("HashSQLitePages: %v", err)
	}

	var got []PageHash
	for ph, err := range seq {
		if err != nil {
			t.Fatalf("iter error: %v", err)
		}
		if ph.Offset < 0 {
			t.Errorf("negative offset: %d", ph.Offset)
		}
		if ph.Offset%pageSize != 0 {
			t.Errorf("offset %d not a multiple of %d", ph.Offset, pageSize)
		}
		got = append(got, ph)
	}

	if len(got) != len(expected) {
		t.Fatalf("got %d pages, want %d", len(got), len(expected))
	}
	for i := range got {
		if got[i].Offset != expected[i].Offset {
			t.Errorf("page %d: offset got %d want %d", i, got[i].Offset, expected[i].Offset)
		}
		if got[i].Hash != expected[i].Hash {
			t.Errorf("page %d: hash got %s want %s", i, got[i].Hash, expected[i].Hash)
		}
	}

	// Sorted ascending check
	for i := 1; i < len(got); i++ {
		if got[i].Offset <= got[i-1].Offset {
			t.Errorf("not strictly ascending at %d: %d <= %d", i, got[i].Offset, got[i-1].Offset)
		}
	}
}

func TestHashSQLitePages_BadPageSize(t *testing.T) {
	if _, err := HashSQLitePages("testdata/sqlite_pages.bin", 0); err == nil {
		t.Errorf("want error on pageSize=0")
	}
	if _, err := HashSQLitePages("testdata/sqlite_pages.bin", -1); err == nil {
		t.Errorf("want error on pageSize<0")
	}
}

func TestHashSQLitePages_NotPageAligned_YieldsError(t *testing.T) {
	// whole_file.bin is 4097 bytes — not a multiple of 4096. Iteration
	// should yield 1 page then an error on the partial trailing read.
	seq, err := HashSQLitePages("testdata/whole_file.bin", 4096)
	if err != nil {
		t.Fatalf("HashSQLitePages: %v", err)
	}
	var sawError bool
	var pages int
	for _, err := range seq {
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "page-aligned") {
				sawError = true
				break
			}
			t.Errorf("unexpected error: %v", err)
		}
		pages++
	}
	if !sawError {
		t.Errorf("expected page-alignment error, saw %d pages and no error", pages)
	}
}

func readExpectedPages(t *testing.T) []PageHash {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "expected.txt"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var out []PageHash
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		off, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue // skip whole_file_sha256 line (non-numeric)
		}
		out = append(out, PageHash{Offset: off, Hash: parts[1]})
	}
	return out
}
