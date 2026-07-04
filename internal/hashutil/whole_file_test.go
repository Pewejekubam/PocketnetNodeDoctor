package hashutil

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T010: HashWholeFile returns lowercase 64-hex SHA-256, streamed via 1 MiB
// buffer (D14); compares against testdata/expected.txt.
func TestHashWholeFile_MatchesFixture(t *testing.T) {
	want := readExpectedField(t, "whole_file_sha256")
	path := filepath.Join("testdata", "whole_file.bin")
	got, err := HashWholeFile(path)
	if err != nil {
		t.Fatalf("HashWholeFile: %v", err)
	}
	if got != want {
		t.Errorf("hash mismatch:\n got %s\nwant %s", got, want)
	}
	if got != strings.ToLower(got) || len(got) != 64 {
		t.Errorf("want lowercase 64-hex, got %q (len %d)", got, len(got))
	}
}

func TestHashWholeFile_MissingFile_ReturnsError(t *testing.T) {
	if _, err := HashWholeFile("testdata/does-not-exist"); err == nil {
		t.Errorf("want error on missing file")
	}
}

func readExpectedField(t *testing.T, key string) string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "expected.txt"))
	if err != nil {
		t.Fatalf("open expected.txt: %v", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	t.Fatalf("key %q not in expected.txt", key)
	return ""
}
