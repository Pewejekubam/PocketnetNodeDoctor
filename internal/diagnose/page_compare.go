package diagnose

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/hashutil"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// ComparePages stream-iterates hashutil.HashSQLitePages over
// <pocketdbRoot>/<entry.Path>, comparing each (offset, hash) against the
// manifest's per-page hash. Returns the divergent pages.
//
// Both the manifest Pages slice and HashSQLitePages iterate in ascending
// offset order, so comparison is a single merge-pass with O(1) extra memory —
// no intermediate maps are built.
//
// Special cases:
//   - File missing: every canonical page becomes a divergent entry.
//   - File shorter than canonical: every page beyond the local file's end
//     becomes a divergent entry.
//   - File longer than canonical: extra local pages are ignored (apply will
//     truncate to canonical length on swap).
func ComparePages(pocketdbRoot string, entry manifest.Entry) ([]plan.Page, error) {
	if entry.EntryKind != manifest.EntryKindSQLitePages {
		return nil, fmt.Errorf("page-compare: not an sqlite_pages entry: %q", entry.Path)
	}
	path := joinRel(pocketdbRoot, entry.Path)

	// File missing → all canonical pages diverge.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		out := make([]plan.Page, 0, len(entry.Pages))
		for _, p := range entry.Pages {
			out = append(out, plan.Page{Offset: p.Offset, ExpectedHash: p.Hash})
		}
		return out, nil
	}

	const pageSize = 4096
	seq, err := hashutil.HashSQLitePages(path, pageSize)
	if err != nil {
		return nil, fmt.Errorf("page-compare: hash: %w", err)
	}

	// Merge-compare: manifest pages and local pages are both in ascending
	// offset order. Walk them with a single index into entry.Pages.
	manifPages := entry.Pages
	manifIdx := 0
	var divergent []plan.Page

	for ph, ierr := range seq {
		if ierr != nil {
			if errors.Is(ierr, io.ErrUnexpectedEOF) {
				// Page-misalignment: stop; remaining canonical pages reported below.
				break
			}
			return nil, fmt.Errorf("page-compare: %w", ierr)
		}

		// Advance past any canonical pages whose offset is less than the
		// current local page — these are gaps in the local file (should not
		// happen for a well-formed SQLite, but treated as divergent below).
		for manifIdx < len(manifPages) && manifPages[manifIdx].Offset < ph.Offset {
			divergent = append(divergent, plan.Page{
				Offset:       manifPages[manifIdx].Offset,
				ExpectedHash: manifPages[manifIdx].Hash,
			})
			manifIdx++
		}

		if manifIdx >= len(manifPages) {
			// Local has pages beyond canonical's coverage; ignore.
			break
		}

		if manifPages[manifIdx].Offset == ph.Offset {
			// Matching offset: compare hashes.
			if ph.Hash != manifPages[manifIdx].Hash {
				divergent = append(divergent, plan.Page{
					Offset:       ph.Offset,
					ExpectedHash: manifPages[manifIdx].Hash,
				})
			}
			manifIdx++
		}
		// If manifPages[manifIdx].Offset > ph.Offset: extra local page not in
		// canonical; ignore it.
	}

	// Canonical pages not reached by the local file → divergent.
	for ; manifIdx < len(manifPages); manifIdx++ {
		divergent = append(divergent, plan.Page{
			Offset:       manifPages[manifIdx].Offset,
			ExpectedHash: manifPages[manifIdx].Hash,
		})
	}

	sortPages(divergent)
	return divergent, nil
}

// ComparePagesStreaming is the streaming counterpart to ComparePages: instead
// of indexing into a materialized []manifest.Page, it consumes the manifest
// pages from manifPageIter via iter.Pull2, holding at most one manifest page
// in memory at a time, and yields divergent pages as an iter.Seq2 as they are
// found. This is the path that prevents OOM on the 40M-page main.sqlite3 entry —
// paired with manifest.FetchAndProcess, neither the manifest body, the manifest
// page list, nor the divergent-page list is ever fully materialized (D3, FR-001).
//
// Same merge-compare semantics as ComparePages:
//   - File missing: every manifest page becomes divergent (drains manifPageIter).
//   - File shorter than canonical: trailing canonical pages are divergent.
//   - File longer than canonical: extra local pages are ignored (apply truncates).
//   - Page-misalignment (io.ErrUnexpectedEOF mid-stream): treated as local
//     ending; remaining canonical pages are reported as divergent.
//
// Divergent pages are yielded in ascending-offset order **as found** — no
// terminal sort. Both the manifest page stream and hashutil.HashSQLitePages
// iterate in strictly ascending offset order and the merge-compare always emits
// the smaller-offset page first, so the yielded order is already monotonically
// ascending (DA4). Hash-read and manifest-iter errors surface through the Seq2
// error slot, after which iteration stops.
//
// path is the manifest entry's relative path (e.g., "pocketdb/main.sqlite3"),
// resolved against pocketdbRoot via the same joinRel rules as ComparePages.
func ComparePagesStreaming(pocketdbRoot, path string, manifPageIter iter.Seq2[manifest.Page, error]) iter.Seq2[plan.Page, error] {
	return func(yield func(plan.Page, error) bool) {
		resolved := joinRel(pocketdbRoot, path)

		manifNext, manifStop := iter.Pull2(manifPageIter)
		defer manifStop()

		// File missing → drain manifPageIter and yield every page as divergent.
		if _, err := os.Stat(resolved); errors.Is(err, os.ErrNotExist) {
			for {
				mp, mErr, ok := manifNext()
				if !ok {
					return
				}
				if mErr != nil {
					yield(plan.Page{}, fmt.Errorf("page-compare: manifest iter: %w", mErr))
					return
				}
				if !yield(plan.Page{Offset: mp.Offset, ExpectedHash: mp.Hash}, nil) {
					return
				}
			}
		}

		const pageSize = 4096
		seq, err := hashutil.HashSQLitePages(resolved, pageSize)
		if err != nil {
			yield(plan.Page{}, fmt.Errorf("page-compare: hash: %w", err))
			return
		}

		// Prime: read first manifest page (if any).
		var (
			curManif  manifest.Page
			haveManif bool
		)
		if mp, mErr, ok := manifNext(); ok {
			if mErr != nil {
				yield(plan.Page{}, fmt.Errorf("page-compare: manifest iter: %w", mErr))
				return
			}
			curManif, haveManif = mp, true
		}

		// advanceManif reads the next manifest page. Returns false if the
		// manifest iterator errored (an error was already yielded) so callers
		// abort immediately.
		advanceManif := func() (ok bool) {
			mp, mErr, more := manifNext()
			if !more {
				haveManif = false
				return true
			}
			if mErr != nil {
				yield(plan.Page{}, fmt.Errorf("page-compare: manifest iter: %w", mErr))
				return false
			}
			curManif = mp
			return true
		}

		for ph, ierr := range seq {
			if ierr != nil {
				if errors.Is(ierr, io.ErrUnexpectedEOF) {
					// Page-misalignment: treat as local ending; remaining
					// manifest pages reported as divergent below.
					break
				}
				yield(plan.Page{}, fmt.Errorf("page-compare: %w", ierr))
				return
			}

			// Advance past any manifest pages whose offset is less than the
			// current local page (gaps in the local file → divergent).
			for haveManif && curManif.Offset < ph.Offset {
				if !yield(plan.Page{Offset: curManif.Offset, ExpectedHash: curManif.Hash}, nil) {
					return
				}
				if !advanceManif() {
					return
				}
			}

			if !haveManif {
				// Local has pages beyond canonical's coverage; ignore.
				break
			}

			if curManif.Offset == ph.Offset {
				if ph.Hash != curManif.Hash {
					if !yield(plan.Page{Offset: ph.Offset, ExpectedHash: curManif.Hash}, nil) {
						return
					}
				}
				if !advanceManif() {
					return
				}
			}
			// If curManif.Offset > ph.Offset: extra local page not in canonical;
			// ignore and let the next local page advance.
		}

		// Manifest pages not reached by the local file → divergent.
		for haveManif {
			if !yield(plan.Page{Offset: curManif.Offset, ExpectedHash: curManif.Hash}, nil) {
				return
			}
			if !advanceManif() {
				return
			}
		}
	}
}
