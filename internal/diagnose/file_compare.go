package diagnose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/hashutil"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// CompareFile hashes the local whole_file artifact and compares against the
// manifest entry's hash. Missing local file → divergence with
// expected_source: "fetch_full" (EC-001 / EC-002).
//
// Returns (Divergence, divergent=true, error). If local file matches
// canonical, divergent=false.
func CompareFile(pocketdbRoot string, entry manifest.Entry) (plan.Divergence, bool, error) {
	if entry.EntryKind != manifest.EntryKindWholeFile {
		return plan.Divergence{}, false, fmt.Errorf("file-compare: not a whole_file entry: %q", entry.Path)
	}
	return CompareFileByPathHash(pocketdbRoot, entry.Path, entry.Hash)
}

// CompareFileByPathHash is the scalar-arg companion to CompareFile, taking
// path and expectedHash directly so callers (e.g., the streaming orchestrator
// using manifest.FetchAndProcess) don't need to construct a synthetic
// manifest.Entry.
func CompareFileByPathHash(pocketdbRoot, path, expectedHash string) (plan.Divergence, bool, error) {
	resolved := joinRel(pocketdbRoot, path)
	if _, err := os.Stat(resolved); errors.Is(err, os.ErrNotExist) {
		return plan.Divergence{
			Kind:           plan.DivergenceKindWholeFile,
			Path:           path,
			ExpectedHash:   expectedHash,
			ExpectedSource: "fetch_full",
		}, true, nil
	}
	got, err := hashutil.HashWholeFile(resolved)
	if err != nil {
		return plan.Divergence{}, false, fmt.Errorf("file-compare: hash %q: %w", resolved, err)
	}
	if got != expectedHash {
		return plan.Divergence{
			Kind:         plan.DivergenceKindWholeFile,
			Path:         path,
			ExpectedHash: expectedHash,
		}, true, nil
	}
	return plan.Divergence{}, false, nil
}

// joinRel resolves a manifest entry path to an absolute filesystem path.
//
// pocketdbRoot is the operator-supplied `--pocketdb` path (the pocketdb/
// subdirectory of the pocketnet data directory, where main.sqlite3 lives).
//
// Manifest paths fall into two categories:
//   - "pocketdb/..." paths: relative to pocketdbRoot (strip the prefix).
//   - All other paths (e.g. "chainstate/", "blocks/"): these are relative to
//     the datadir, which is the parent of pocketdbRoot.
//
// Examples:
//
//	pocketdbRoot=/home/pocketnet/.pocketcoin/pocketdb entry="pocketdb/main.sqlite3"
//	  -> /home/pocketnet/.pocketcoin/pocketdb/main.sqlite3
//	pocketdbRoot=/home/pocketnet/.pocketcoin/pocketdb entry="chainstate/CURRENT"
//	  -> /home/pocketnet/.pocketcoin/chainstate/CURRENT
func joinRel(pocketdbRoot, entryPath string) string {
	if strings.HasPrefix(entryPath, "pocketdb/") {
		return filepath.Join(pocketdbRoot, strings.TrimPrefix(entryPath, "pocketdb/"))
	}
	// chainstate/, blocks/, etc. live at the datadir level (parent of pocketdb/).
	return filepath.Join(filepath.Dir(pocketdbRoot), entryPath)
}

// sortPages orders divergent pages by ascending offset.
func sortPages(pages []plan.Page) {
	sort.Slice(pages, func(i, j int) bool { return pages[i].Offset < pages[j].Offset })
}
