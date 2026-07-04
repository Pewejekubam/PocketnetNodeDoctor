// plan_stream_gen_test.go — streaming test-side plan generator (011-010
// chunk-1, task T003).
//
// psGenerateLargeSQLitePlan writes a >= nPages-`sqlite_pages`-page plan.json to
// disk WITHOUT ever materializing a plan.Plan in the test process — the
// forbidden shortcut (`PlanBuilder.Build` before the baseline VmHWM read) drives
// the monotone high-water mark past the apply consumption peak and masks the
// SC-002 delta. It streams canonform fragments through an incremental SHA-256
// exactly as the prefix-payload contract prescribes, so the emitted file is a
// byte-valid format_version 2 plan with a correct self_hash.
//
// Fixture shape (SC-002 vacuity requirements):
//   - one sqlite_pages divergence carrying all nPages pages — the realistic
//     production shape (a single main.sqlite3 entry holds millions of pages), so
//     a decoder that materialises one whole divergence still blows the bound.
//   - path NOT suffixed main.sqlite3 → apply's post-apply integrity_check is
//     exempt (no real SQLite fixture needed).
//   - unique, dense offsets 0..nPages-1 → unique marker identifiers
//     (path:offset), so apply does not deadlock on duplicate identifiers, and a
//     1-byte shared chunk keeps the promoted live file ~nPages bytes.
//   - one shared page-content hash → one registered chunk.
package integration

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// psGenerateLargeSQLitePlan streams a plan.json with nPages sqlite_pages page
// entries to planPath. pocketdbDir is stamped into pocketdb_path. Returns the
// shared chunk's raw content and its lowercase-hex SHA-256 (the value every
// page's expected_hash carries), for the caller to register with a chunk store.
func psGenerateLargeSQLitePlan(t *testing.T, planPath, pocketdbDir string, nPages int) (chunkHash string, chunkContent []byte) {
	t.Helper()

	chunkContent = []byte{0x07} // 1-byte chunk → live file stays ~nPages bytes
	sum := sha256.Sum256(chunkContent)
	chunkHash = hex.EncodeToString(sum[:])

	absPocketDB, err := filepath.Abs(pocketdbDir)
	if err != nil {
		t.Fatalf("abs pocketdb: %v", err)
	}

	mustCanon := func(v any) []byte {
		b, err := canonform.Marshal(v)
		if err != nil {
			t.Fatalf("canonform: %v", err)
		}
		return b
	}

	f, err := os.Create(planPath)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 1<<20)
	h := sha256.New()
	mw := io.MultiWriter(bw, h) // payload bytes go to file AND hasher

	ci := plan.CanonicalIdentity{
		BlockHeight:          3806626,
		ManifestHash:         "sc002" + chunkHash[5:], // arbitrary; freshness is skipped (no manifest_url)
		PocketnetCoreVersion: "0.21.16-test",
	}

	// Top-level canonform key order: canonical_identity, divergences,
	// format_version, manifest_url, pocketdb_path, self_hash.
	// Per-divergence order: divergence_kind, pages, path.
	// Per-page order: expected_hash, offset.
	io.WriteString(mw, `{"canonical_identity":`)                      //nolint:errcheck
	mw.Write(mustCanon(ci))                                           //nolint:errcheck
	io.WriteString(mw, `,"divergences":[`)                            //nolint:errcheck
	io.WriteString(mw, `{"divergence_kind":"sqlite_pages","pages":[`) //nolint:errcheck
	for i := 0; i < nPages; i++ {
		if i > 0 {
			io.WriteString(mw, ",") //nolint:errcheck
		}
		mw.Write(mustCanon(plan.Page{Offset: int64(i), ExpectedHash: chunkHash})) //nolint:errcheck
	}
	io.WriteString(mw, `],"path":`)          //nolint:errcheck
	mw.Write(mustCanon("synth/pages.db"))    //nolint:errcheck
	io.WriteString(mw, `}`)                  //nolint:errcheck (close divergence object)
	io.WriteString(mw, `]`)                  //nolint:errcheck (close divergences array)
	io.WriteString(mw, `,"format_version":`) //nolint:errcheck
	mw.Write(mustCanon(plan.FormatVersion))  //nolint:errcheck
	io.WriteString(mw, `,"manifest_url":`)   //nolint:errcheck
	mw.Write(mustCanon(""))                  //nolint:errcheck
	io.WriteString(mw, `,"pocketdb_path":`)  //nolint:errcheck
	mw.Write(mustCanon(absPocketDB))         //nolint:errcheck

	// Trailing } to the hasher only, then ,"self_hash":"<hex>"} to the file only.
	h.Write([]byte("}")) //nolint:errcheck
	selfHash := hex.EncodeToString(h.Sum(nil))
	io.WriteString(bw, `,"self_hash":"`+selfHash+`"}`) //nolint:errcheck

	if err := bw.Flush(); err != nil {
		t.Fatalf("flush plan: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync plan: %v", err)
	}
	return chunkHash, chunkContent
}
