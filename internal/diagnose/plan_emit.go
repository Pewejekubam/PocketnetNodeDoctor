package diagnose

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// PlanEmitter incrementally assembles a canonform plan.json. sqlite_pages
// divergences stream to a transient spool as they are detected (never a
// materialized []plan.Page); the small whole_file group buffers in memory
// (O(whole-file-count), FR-001-authorized). Finalize assembles the plan temp
// file with an incremental self-hash and renames it into place, superseding
// WritePlanAtomic at the diagnose emission site while preserving its temp-file +
// fsync + rename contract (EC-003).
//
// Output is byte-identical to plan.Marshal for the same divergences (SC-003):
// every fragment is produced by internal/canonform and joined with canonform's
// exact array/object comma structure, and the self-hash is computed over
// exactly the payload bytes per the prefix-payload contract.
type PlanEmitter struct {
	planOutPath string
	spoolPath   string
	spool       *os.File
	sqliteCount int               // sqlite_pages divergences written to the spool
	wholeFile   []plan.Divergence // O(whole-file-count) buffer, emitted after the sqlite group
	tmpPath     string            // set in Finalize; removed by Abort on failure
}

// NewPlanEmitter opens the transient divergence spool in the plan-out directory
// (DA1: filepath.Dir(planOutPath) is already probed writable). The plan temp
// file is opened lazily in Finalize.
func NewPlanEmitter(planOutPath string) (*PlanEmitter, error) {
	spool, err := os.CreateTemp(filepath.Dir(planOutPath), "pocketnet-node-doctor-planemit-*.spool")
	if err != nil {
		return nil, fmt.Errorf("plan-emit: create spool: %w", err)
	}
	return &PlanEmitter{planOutPath: planOutPath, spoolPath: spool.Name(), spool: spool}, nil
}

// StreamSQLitePages writes one sqlite_pages divergence to the spool, streaming
// each divergent page from the compare-feed as a canonform page object without
// ever buffering the page list. If the feed yields zero divergent pages, nothing
// is written (matching the materializing orchestrator, which records a
// divergence only when len(divPages) > 0). Returns the number of divergent pages
// written (for the caller's VolumeCapacity totalBytes tally) and any feed error.
func (e *PlanEmitter) StreamSQLitePages(path string, pages iter.Seq2[plan.Page, error]) (int, error) {
	started := false
	count := 0
	for pg, err := range pages {
		if err != nil {
			return count, err
		}
		if !started {
			if e.sqliteCount > 0 {
				if _, werr := e.spool.WriteString(","); werr != nil {
					return count, fmt.Errorf("plan-emit: spool: %w", werr)
				}
			}
			if _, werr := e.spool.WriteString(`{"divergence_kind":"sqlite_pages","pages":[`); werr != nil {
				return count, fmt.Errorf("plan-emit: spool: %w", werr)
			}
			started = true
		} else {
			if _, werr := e.spool.WriteString(","); werr != nil {
				return count, fmt.Errorf("plan-emit: spool: %w", werr)
			}
		}
		b, cerr := canonform.Marshal(pg)
		if cerr != nil {
			return count, fmt.Errorf("plan-emit: canonform page: %w", cerr)
		}
		if _, werr := e.spool.Write(b); werr != nil {
			return count, fmt.Errorf("plan-emit: spool: %w", werr)
		}
		count++
	}
	if started {
		pathBytes, cerr := canonform.Marshal(path)
		if cerr != nil {
			return count, fmt.Errorf("plan-emit: canonform path: %w", cerr)
		}
		if _, werr := e.spool.WriteString(`],"path":`); werr != nil {
			return count, fmt.Errorf("plan-emit: spool: %w", werr)
		}
		if _, werr := e.spool.Write(pathBytes); werr != nil {
			return count, fmt.Errorf("plan-emit: spool: %w", werr)
		}
		if _, werr := e.spool.WriteString("}"); werr != nil {
			return count, fmt.Errorf("plan-emit: spool: %w", werr)
		}
		e.sqliteCount++
	}
	return count, nil
}

// BufferWholeFile appends a whole_file divergence to the in-memory buffer,
// emitted (in call order) after the sqlite_pages group during Finalize.
func (e *PlanEmitter) BufferWholeFile(div plan.Divergence) {
	e.wholeFile = append(e.wholeFile, div)
}

// Finalize assembles the plan temp file through io.MultiWriter(tmp, hasher),
// computes the self-hash incrementally over exactly the payload bytes, appends
// the self_hash member, fsyncs, and renames — then removes the spool. On any
// error the caller invokes Abort (no partial plan at the final path, EC-003).
func (e *PlanEmitter) Finalize(ci plan.CanonicalIdentity, manifestURL, pocketDBPath string) error {
	if err := e.spool.Sync(); err != nil {
		return fmt.Errorf("plan-emit: sync spool: %w", err)
	}
	if _, err := e.spool.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("plan-emit: seek spool: %w", err)
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("plan-emit: rand: %w", err)
	}
	e.tmpPath = e.planOutPath + ".tmp." + hex.EncodeToString(suffix)
	f, err := os.OpenFile(e.tmpPath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("plan-emit: create %q: %w", e.tmpPath, err)
	}

	h := sha256.New()
	mw := io.MultiWriter(f, h) // payload bytes go to file AND hasher

	if err := e.writePayload(mw, ci, manifestURL, pocketDBPath); err != nil {
		_ = f.Close()
		return err
	}

	// Trailing } to the hasher only — completes the self-hash payload object.
	h.Write([]byte("}"))
	selfHash := hex.EncodeToString(h.Sum(nil))

	// ,"self_hash":"<hex>"} to the file only.
	if _, err := io.WriteString(f, `,"self_hash":"`+selfHash+`"}`); err != nil {
		_ = f.Close()
		return fmt.Errorf("plan-emit: write self_hash: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("plan-emit: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("plan-emit: close: %w", err)
	}
	if err := os.Rename(e.tmpPath, e.planOutPath); err != nil {
		return fmt.Errorf("plan-emit: rename: %w", err)
	}
	e.tmpPath = "" // renamed away; Abort must not remove the final plan
	e.removeSpool()
	return nil
}

// writePayload writes the self-hash payload (the plan object without its closing
// } and without the self_hash member) to w, in canonform top-level key order:
// canonical_identity, divergences, format_version, manifest_url, pocketdb_path.
func (e *PlanEmitter) writePayload(w io.Writer, ci plan.CanonicalIdentity, manifestURL, pocketDBPath string) error {
	ciBytes, err := canonform.Marshal(ci)
	if err != nil {
		return fmt.Errorf("plan-emit: canonform canonical_identity: %w", err)
	}
	if _, err := io.WriteString(w, `{"canonical_identity":`); err != nil {
		return err
	}
	if _, err := w.Write(ciBytes); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"divergences":[`); err != nil {
		return err
	}

	// sqlite_pages group (streamed spool copy), then whole_file group — the
	// frozen emission order (both entry-arrival within group).
	if _, err := io.Copy(w, e.spool); err != nil {
		return fmt.Errorf("plan-emit: copy spool: %w", err)
	}
	haveDiv := e.sqliteCount > 0
	for _, div := range e.wholeFile {
		if haveDiv {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		db, cerr := canonform.Marshal(div)
		if cerr != nil {
			return fmt.Errorf("plan-emit: canonform whole_file: %w", cerr)
		}
		if _, err := w.Write(db); err != nil {
			return err
		}
		haveDiv = true
	}
	if _, err := io.WriteString(w, `]`); err != nil {
		return err
	}

	fvBytes, err := canonform.Marshal(plan.FormatVersion)
	if err != nil {
		return fmt.Errorf("plan-emit: canonform format_version: %w", err)
	}
	muBytes, err := canonform.Marshal(manifestURL)
	if err != nil {
		return fmt.Errorf("plan-emit: canonform manifest_url: %w", err)
	}
	ppBytes, err := canonform.Marshal(pocketDBPath)
	if err != nil {
		return fmt.Errorf("plan-emit: canonform pocketdb_path: %w", err)
	}
	if _, err := io.WriteString(w, `,"format_version":`); err != nil {
		return err
	}
	if _, err := w.Write(fvBytes); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"manifest_url":`); err != nil {
		return err
	}
	if _, err := w.Write(muBytes); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"pocketdb_path":`); err != nil {
		return err
	}
	if _, err := w.Write(ppBytes); err != nil {
		return err
	}
	return nil
}

// Abort removes the spool and (if Finalize created it) the plan temp file, so no
// partial plan is observable at the final path (EC-003). Idempotent; safe to
// call after a successful Finalize (both paths already cleaned).
func (e *PlanEmitter) Abort() {
	e.removeSpool()
	if e.tmpPath != "" {
		_ = os.Remove(e.tmpPath)
		e.tmpPath = ""
	}
}

func (e *PlanEmitter) removeSpool() {
	if e.spool != nil {
		_ = e.spool.Close()
		e.spool = nil
	}
	if e.spoolPath != "" {
		_ = os.Remove(e.spoolPath)
		e.spoolPath = ""
	}
}
