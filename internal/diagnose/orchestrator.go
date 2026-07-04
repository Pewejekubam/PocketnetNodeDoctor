package diagnose

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/preflight"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// Options is the diagnose pathway's input.
type Options struct {
	CanonicalURL string
	PocketDBPath string
	PlanOutPath  string
	PinnedHash   string
	Logger       *stderrlog.Logger
	Transport    http.RoundTripper // optional; nil for production
}

// Diagnose executes the read-only diagnose pathway, streaming the manifest
// body via manifest.FetchAndProcess so memory is O(parser buffer + per-page
// object) regardless of canonical size. Returns the typed exit code and a
// wrapped error (nil on Success).
//
// Sequence:
//  1. running-node predicate (pre-manifest, D8)
//  2. plan-out writability probe (moved earlier — fail fast before download)
//  3. FetchAndProcess streams the body. Per entry:
//     - sqlite_pages (main.sqlite3): stream pages through
//     ComparePagesStreaming; count divergent-page-bytes for VolumeCapacity
//     (per pre-spec line 314: bound is 2x plan size, not 2x canonical total).
//     - whole_file: CompareFileByPathHash on hdr.Path / hdr.Hash.
//     Trust-root verification + trust_anchors presence happen inside
//     FetchAndProcess after the body is fully consumed.
//  4. Post-stream predicates: CheckFormatVersionValue, VersionMismatchValue,
//     VolumeCapacityFromTotalBytes, PermissionReadOnly.
//  5. Plan emission (atomic write).
//  6. Summary emission.
func Diagnose(ctx context.Context, opts Options) (exitcode.Code, error) {
	logger := opts.Logger
	if logger == nil {
		logger = stderrlog.New(false)
	}

	// 1. Pre-manifest: running-node check.
	pre := preflight.PreManifest()
	if res := pre.Fn(preflight.PreflightContext{PocketDBPath: opts.PocketDBPath, Logger: logger}); !res.Pass {
		logger.Info("%s", res.Refused.Diagnostic)
		return res.Refused.Code, fmt.Errorf("%s", res.Refused.Diagnostic)
	}

	// 2. Plan-out writability probe — moved BEFORE the stream so a 3.8 GB
	// download isn't wasted on an unwritable target.
	if err := ProbeWritable(opts.PlanOutPath); err != nil {
		logger.Info("plan-out writability probe failed: %v", err)
		return exitcode.GenericError, err
	}

	// 3. Stream the manifest. The entry callback streams divergences into the
	// incremental plan emitter (sqlite_pages pages to a spool, whole_file into a
	// small buffer) and tallies divergent-page-bytes for the post-stream
	// VolumeCapacity check (pre-spec line 314). Peak memory is O(1) in page count
	// (FR-001) — no []plan.Page is ever materialized.
	prog := NewProgressEmitter(logger)
	emitter, err := NewPlanEmitter(opts.PlanOutPath)
	if err != nil {
		logger.Info("plan emitter init failed: %v", err)
		return exitcode.GenericError, err
	}
	// Abort on every non-success exit path (predicate refusal, stream error,
	// write error) so no partial plan and no spool are left behind (EC-003).
	finalized := false
	defer func() {
		if !finalized {
			emitter.Abort()
		}
	}()

	var (
		totalBytes uint64
		pageCount  int64
		fileCount  int64
	)

	entryFn := func(hdr *manifest.EntryHeader, pages iter.Seq2[manifest.Page, error]) error {
		switch hdr.EntryKind {
		case manifest.EntryKindSQLitePages:
			// This doctor routes sqlite_pages to pocketdb/main.sqlite3 (the
			// only sqlite_pages entry it knows how to recover). hdr.Path is
			// not yet populated at callback time (canonical key order places
			// "path" after "pages"); declaring AssumedPath makes the stream
			// reader fail-closed after the entry's path is parsed, so a
			// manifest carrying sqlite_pages for another artifact (e.g.
			// pocketdb/web.sqlite3) aborts diagnose instead of having its
			// pages attributed to — and later spliced into — main.sqlite3
			// (pocketnet-node-doctor-cpm).
			const sqliteEntryPath = "pocketdb/main.sqlite3"
			hdr.AssumedPath = sqliteEntryPath

			started := prog.Enter(sqliteEntryPath)
			n, err := emitter.StreamSQLitePages(sqliteEntryPath, ComparePagesStreaming(opts.PocketDBPath, sqliteEntryPath, pages))
			prog.Exit(sqliteEntryPath, started)
			if err != nil {
				return err
			}
			if n > 0 {
				fileCount++
				pageCount += int64(n)
			}
			// Per pre-spec line 314: volume-capacity bound is 2x the size of
			// files in the plan, not 2x the canonical total. Count only
			// divergent page-bytes (pocketnet-node-doctor-8m0), one page at a
			// time as they stream (whole_file divergent bytes treated as 0).
			totalBytes += uint64(n) * 4096
			return nil

		case manifest.EntryKindWholeFile:
			// hdr.Path and hdr.Hash are set for whole_file (canonical key
			// order: entry_kind, hash, path — all parsed before the entry
			// closes and fn is invoked).
			div, divergent, err := CompareFileByPathHash(opts.PocketDBPath, hdr.Path, hdr.Hash)
			if err != nil {
				return err
			}
			if divergent {
				emitter.BufferWholeFile(div)
				fileCount++
			}
			return nil
		}
		// Unknown entry_kind: ignore (forward-compat).
		return nil
	}

	hdr, err := manifest.FetchAndProcess(ctx, opts.CanonicalURL, opts.PinnedHash, opts.Transport, httptransfer.DefaultPolicy(), filepath.Dir(opts.PlanOutPath), entryFn)
	if err != nil {
		// Manifest-level errors (spool capacity, trust-root mismatch,
		// trust_anchors missing, fetch, path-invalid, network, decode).
		logger.Info("manifest stream failed: %v", err)
		// Spool capacity shortfall / mid-spool ENOSPC reuses the existing
		// insufficient-disk allocation (exit 5) — same category as the
		// post-stream VolumeCapacity check; no new exit code (FR-002, D2).
		if manifest.IsSpoolCapacity(err) {
			return exitcode.Capacity, err
		}
		return exitcode.GenericError, err
	}

	// 4. Post-stream predicates. Any refusal returns here and the deferred
	// Abort() discards the spool and any temp file.
	if err := manifest.CheckFormatVersionValue(hdr.FormatVersion); err != nil {
		logger.Info("manifest format_version refused: %v", err)
		if manifest.IsFormatVersionUnrecognized(err) {
			return exitcode.ManifestFormatVersionUnrecognized, err
		}
		return exitcode.GenericError, err
	}

	if vmRes := preflight.VersionMismatchValue(hdr.CanonicalIdentity.PocketnetCoreVersion); !vmRes.Pass {
		logger.Info("preflight refused (version-mismatch): %s", vmRes.Refused.Diagnostic)
		return vmRes.Refused.Code, fmt.Errorf("version-mismatch: %s", vmRes.Refused.Diagnostic)
	}

	if vcRes := preflight.VolumeCapacityFromTotalBytes(opts.PocketDBPath, totalBytes); !vcRes.Pass {
		logger.Info("preflight refused (volume-capacity): %s", vcRes.Refused.Diagnostic)
		return vcRes.Refused.Code, fmt.Errorf("volume-capacity: %s", vcRes.Refused.Diagnostic)
	}

	if prRes := preflight.PermissionReadOnly(preflight.PreflightContext{PocketDBPath: opts.PocketDBPath, Logger: logger}); !prRes.Pass {
		logger.Info("preflight refused (permission-readonly): %s", prRes.Refused.Diagnostic)
		return prRes.Refused.Code, fmt.Errorf("permission-readonly: %s", prRes.Refused.Diagnostic)
	}

	// 5. Plan emission. The emitter assembles the plan temp file (sqlite_pages
	// group from the spool, then the whole_file buffer — frozen emission order),
	// computes the self-hash incrementally, and atomically renames into place.
	//
	// Resolve pocketdb path to absolute so apply can target it even if invoked
	// from a different working directory (pocketnet-node-doctor-x08).
	absPocketDB, err := filepath.Abs(opts.PocketDBPath)
	if err != nil {
		return exitcode.GenericError, fmt.Errorf("diagnose: resolve pocketdb path: %w", err)
	}

	ci := plan.CanonicalIdentity{
		BlockHeight:          hdr.CanonicalIdentity.BlockHeight,
		ManifestHash:         opts.PinnedHash,
		PocketnetCoreVersion: hdr.CanonicalIdentity.PocketnetCoreVersion,
	}
	if err := emitter.Finalize(ci, opts.CanonicalURL, absPocketDB); err != nil {
		logger.Info("plan write failed: %v", err)
		return exitcode.GenericError, err
	}
	finalized = true

	// 6. Summary.
	emitSummaryCounts(stderrWriter(logger), pageCount, fileCount)

	return exitcode.Success, nil
}

// stderrWriter exposes the logger's underlying writer for io.Writer-shaped
// callers (EmitSummary). The logger writes to os.Stderr by default; tests
// that need to capture stderr inject their own io.Writer at construction.
type loggerWriter struct{ l *stderrlog.Logger }

func (lw loggerWriter) Write(p []byte) (int, error) {
	if lw.l == nil {
		return 0, nil
	}
	lw.l.Info("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func stderrWriter(l *stderrlog.Logger) loggerWriter { return loggerWriter{l: l} }
