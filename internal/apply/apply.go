// Package apply implements the mutating apply pathway: fetch, stage, promote,
// verify, rollback.
package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/rollback"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/staging"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/verify"
)

const maxAttempts = 5

// stagingIdentifier returns the marker identifier for a task.
// whole_file: planRelPath
// sqlite_pages: planRelPath + ":" + offset
func stagingIdentifier(task DivergenceTask) string {
	if task.PageOffset < 0 {
		return task.PlanRelPath
	}
	return fmt.Sprintf("%s:%d", task.PlanRelPath, task.PageOffset)
}

// stagingFilename returns the flat filename to use under staging/staged/ for a
// task. Uses SHA-256 of the identifier string.
func stagingFilename(planRelPath string, pageOffset int64) string {
	var id string
	if pageOffset < 0 {
		id = planRelPath
	} else {
		id = fmt.Sprintf("%s:%d", planRelPath, pageOffset)
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// resolveLivePath converts a plan divergence path to the absolute filesystem
// path of the live file on the patient node.
//
// pocketdbRoot is the operator-supplied pocketdb directory (carried in the
// plan as pocketdb_path); datadir is its parent (filepath.Dir(pocketdbRoot)),
// where chainstate/ and blocks/ live alongside pocketdb/.
//
// Manifest paths fall into two categories:
//   - "pocketdb/..." paths: relative to pocketdbRoot (strip prefix).
//   - All other paths (e.g. "chainstate/", "blocks/"): relative to datadir.
func resolveLivePath(datadir, pocketdbRoot, divPath string) string {
	if strings.HasPrefix(divPath, "pocketdb/") {
		return filepath.Join(pocketdbRoot, filepath.FromSlash(strings.TrimPrefix(divPath, "pocketdb/")))
	}
	return filepath.Join(datadir, filepath.FromSlash(divPath))
}

// deriveChunkBaseURL returns the chunk base URL. The chunks tree is served
// alongside the manifest under <manifest-parent>/chunks/<aa>/<bb>/<hash>.zst,
// so we keep the manifest URL's path component (its parent dir) instead of
// reducing to scheme+host. Empty manifest paths or "/manifest.json" at root
// reduce naturally to scheme+host (path.Dir of "/manifest.json" is "/").
// Trailing slashes on the result are stripped to keep "<base>/chunks/..."
// concatenation clean.
func deriveChunkBaseURL(opts Options) string {
	if opts.ChunkStoreBaseURL != "" {
		return opts.ChunkStoreBaseURL
	}
	if opts.ManifestURL != "" {
		u, err := url.Parse(opts.ManifestURL)
		if err == nil {
			parent := path.Dir(u.Path)
			if parent == "/" {
				parent = ""
			}
			return u.Scheme + "://" + u.Host + parent
		}
	}
	return ""
}

// executeRollback calls rollback.Rollback on shadows and logs outcomes.
func executeRollback(shadows []rollback.Shadow, logger *stderrlog.Logger) exitcode.Code {
	logger.Info("[apply] verification failed — rolling back to pre-apply state")
	result := rollback.Rollback(shadows)
	if result.Status == rollback.ResultFailed {
		for _, p := range result.FailedPaths {
			logger.Info("[apply] rollback: could not restore %s", p)
		}
		return exitcode.RollbackFailed
	}
	return exitcode.RollbackCompleted
}

// injectFaultBeforeRollback implements InjectRollbackFaultForTest: removes the
// first taken shadow file so that rollback.Rollback fails for it.
func injectFaultBeforeRollback(opts Options, shadows []rollback.Shadow) {
	if !opts.InjectRollbackFaultForTest {
		return
	}
	for _, s := range shadows {
		if s.Taken {
			_ = os.Remove(s.ShadowPath)
			return
		}
	}
}

// promoteWholeFile verifies the staged bytes against expectedHash, then renames
// to livePath. Returns requeued=true if the hash mismatches.
// When renameCount reaches injectFailOnNth, a synthetic error is returned instead
// of doing the real rename.
func promoteWholeFile(result FetchResult, renameCount *int32, injectFailOnNth int) (requeued bool, err error) {
	data, err := os.ReadFile(result.Path)
	if err != nil {
		return false, fmt.Errorf("read staged file %s: %w", result.Path, err)
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	if got != result.Task.ExpectedHash {
		_ = os.Remove(result.Path)
		return true, nil
	}

	// Hash OK: promote.
	liveDir := filepath.Dir(result.Task.LivePath)
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", liveDir, err)
	}

	if injectFailOnNth > 0 {
		n := atomic.AddInt32(renameCount, 1)
		if int(n) == injectFailOnNth {
			_ = os.Remove(result.Path)
			return false, fmt.Errorf("injected rename failure (Nth=%d) for %s", injectFailOnNth, result.Task.LivePath)
		}
	}

	if err := os.Rename(result.Path, result.Task.LivePath); err != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", result.Path, result.Task.LivePath, err)
	}
	return false, nil
}

// promoteSQLitePage verifies the staged bytes against expectedHash, then
// writes them at the correct offset into livePath via WriteAt.
func promoteSQLitePage(result FetchResult, renameCount *int32, injectFailOnNth int) (requeued bool, err error) {
	data, err := os.ReadFile(result.Path)
	if err != nil {
		return false, fmt.Errorf("read staged page file %s: %w", result.Path, err)
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	if got != result.Task.ExpectedHash {
		_ = os.Remove(result.Path)
		return true, nil
	}

	// Ensure parent directory exists.
	liveDir := filepath.Dir(result.Task.LivePath)
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", liveDir, err)
	}

	if injectFailOnNth > 0 {
		n := atomic.AddInt32(renameCount, 1)
		if int(n) == injectFailOnNth {
			_ = os.Remove(result.Path)
			return false, fmt.Errorf("injected rename failure (Nth=%d) for %s", injectFailOnNth, result.Task.LivePath)
		}
	}

	f, err := os.OpenFile(result.Task.LivePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false, fmt.Errorf("open live file %s: %w", result.Task.LivePath, err)
	}
	defer f.Close()

	if _, err := f.WriteAt(data, result.Task.PageOffset); err != nil {
		return false, fmt.Errorf("write page at offset %d in %s: %w", result.Task.PageOffset, result.Task.LivePath, err)
	}

	_ = os.Remove(result.Path)
	return false, nil
}

// cleanupStaging removes shadow files and staged files after a successful apply.
// The markers/ directory and plan-hash sentinel are intentionally preserved so
// that a second invocation with the same plan can detect all tasks as already
// complete and exit 0 without issuing any network requests (idempotency).
func cleanupStaging(stagingDir string, shadows []rollback.Shadow, logger *stderrlog.Logger) {
	for _, s := range shadows {
		if s.Taken {
			if err := os.Remove(s.ShadowPath); err != nil && !os.IsNotExist(err) {
				logger.Info("[apply] cleanup: remove shadow %s: %v", s.ShadowPath, err)
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(stagingDir, "staged")); err != nil {
		logger.Info("[apply] cleanup: remove staged/: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(stagingDir, "shadows")); err != nil {
		logger.Info("[apply] cleanup: remove shadows/: %v", err)
	}
	// Intentionally do NOT remove plan-hash or markers/ — they are needed for
	// idempotent re-runs (EC-006).
}

// errStopFeed aborts a StreamSQLitePages walk when the task consumer stops early.
var errStopFeed = errors.New("apply: task feed stopped")

// divergenceTaskIter yields one DivergenceTask at a time over the pending
// divergences, streamed from the on-disk plan (sqlite_pages pages one at a time,
// then the whole_file group from the header set) in frozen emission order.
// Marker-completed entries are skipped. The iterator allocates O(1) extra memory
// in page count — no slice of all pending tasks is materialized (FR-003). Every
// hash was validated 64-lowercase-hex pre-staging, so the h[0:2] slices are safe.
func divergenceTaskIter(headers []plan.DivergenceHeader, planPath, stagingDir, stagedDir, datadir, pocketdbRoot, chunkBaseURL string) iter.Seq[DivergenceTask] {
	return func(yield func(DivergenceTask) bool) {
		stopped := false

		// sqlite_pages group — pages streamed.
		serr := streamSQLitePages(planPath, func(idx int, pages iter.Seq2[plan.Page, error]) error {
			divPath := headers[idx].Path
			livePath := resolveLivePath(datadir, pocketdbRoot, divPath)
			for page, perr := range pages {
				if perr != nil {
					return perr
				}
				id := fmt.Sprintf("%s:%d", divPath, page.Offset)
				if staging.MarkerExists(stagingDir, id) {
					continue
				}
				ph := page.ExpectedHash
				task := DivergenceTask{
					PlanRelPath:  divPath,
					ChunkURL:     chunkBaseURL + "/chunks/" + ph[0:2] + "/" + ph[2:4] + "/" + ph + ".zst",
					ExpectedHash: page.ExpectedHash,
					StagingPath:  filepath.Join(stagedDir, stagingFilename(divPath, page.Offset)),
					LivePath:     livePath,
					Attempt:      0,
					IsAbsentFile: false,
					PageOffset:   page.Offset,
				}
				if !yield(task) {
					stopped = true
					return errStopFeed
				}
			}
			return nil
		})
		// The plan was fully validated pre-staging; a non-stop stream error here is
		// unreachable in practice. Stop-signal aborts are expected and swallowed.
		if stopped || serr != nil {
			return
		}

		// whole_file group.
		for _, h := range headers {
			if h.Kind != plan.DivergenceKindWholeFile {
				continue
			}
			if staging.MarkerExists(stagingDir, h.Path) {
				continue
			}
			hh := h.ExpectedHash
			task := DivergenceTask{
				PlanRelPath:  h.Path,
				ChunkURL:     chunkBaseURL + "/chunks/" + hh[0:2] + "/" + hh[2:4] + "/" + hh + ".zst",
				ExpectedHash: h.ExpectedHash,
				StagingPath:  filepath.Join(stagedDir, stagingFilename(h.Path, -1)),
				LivePath:     resolveLivePath(datadir, pocketdbRoot, h.Path),
				Attempt:      0,
				IsAbsentFile: h.ExpectedSource == "fetch_full",
				PageOffset:   -1,
			}
			if !yield(task) {
				return
			}
		}
	}
}

// streamHeaders opens planPath and returns the divergence header set (shape- and
// hash-validated). O(divergence-count) memory.
func streamHeaders(planPath string) ([]plan.DivergenceHeader, error) {
	f, err := os.Open(planPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return plan.StreamDivergenceHeaders(f)
}

// streamSQLitePages opens planPath and streams pages per sqlite_pages divergence.
func streamSQLitePages(planPath string, fn func(int, iter.Seq2[plan.Page, error]) error) error {
	f, err := os.Open(planPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return plan.StreamSQLitePages(f, fn)
}

// validatePageHashes streams every page and rejects any expected_hash that is not
// 64-lowercase-hex, pre-staging (bead p5m). O(1) in page count.
func validatePageHashes(planPath string, headers []plan.DivergenceHeader) error {
	return streamSQLitePages(planPath, func(idx int, pages iter.Seq2[plan.Page, error]) error {
		for pg, perr := range pages {
			if perr != nil {
				return perr
			}
			if err := plan.ValidatePageHash(idx, pg.Offset, pg.ExpectedHash); err != nil {
				return err
			}
		}
		return nil
	})
}

// Run is the main entry point for the apply subcommand.
func Run(ctx context.Context, opts Options) (exitcode.Code, error) {
	logger := opts.Logger
	if logger == nil {
		logger = stderrlog.New(false)
	}

	// Plan acquisition is streamed over the on-disk file in strict defect-class
	// precedence order — version gate (7) → self-hash verify (15) → shape/hash
	// validation (1) — each pass an O(1)-in-pages streaming decode (FR-003).
	// Passes 1–3 complete before staging.CreateOrResume, so every refusal mutates
	// zero staging state; the in-memory plan.Plan is never materialized.

	// 1. Version gate — token-walk the header (skips the divergences array),
	// gate format_version BEFORE self-hash verification (bead 9v9).
	vf, err := os.Open(opts.PlanPath)
	if err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: read plan %s: %w", opts.PlanPath, err)
	}
	hdr, err := plan.GateFormatVersion(vf)
	vf.Close()
	if err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: parse plan: %w", err)
	}
	if hdr.FormatVersion != plan.FormatVersion {
		return exitcode.ManifestFormatVersionUnrecognized,
			&plan.UnrecognizedFormatVersionError{Got: hdr.FormatVersion, Want: plan.FormatVersion}
	}

	// 2. Verify self-hash → PlanTampered on failure (after version, before side effects).
	if err := VerifyPlanSelfHash(opts.PlanPath); err != nil {
		return exitcode.PlanTampered, err
	}

	// 3. Shape + hash-format validation pass — pre-staging, so a refusal mutates
	// zero staging state and every hash is pre-validated (the task-feed's h[0:2]
	// chunk-URL slice can never panic — bead p5m).
	headers, err := streamHeaders(opts.PlanPath)
	if err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: plan invalid: %w", err)
	}
	if err := validatePageHashes(opts.PlanPath, headers); err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: plan invalid: %w", err)
	}

	// 4a. Inherit manifest URL from the plan when the caller didn't supply one.
	// plan.manifest_url is populated by diagnose so apply can resolve the chunk
	// base URL without a separate CLI flag (pocketnet-node-doctor-2h8).
	if opts.ManifestURL == "" && hdr.ManifestURL != "" {
		opts.ManifestURL = hdr.ManifestURL
	}

	// 5. If ManifestURL is set, verify manifest freshness. VerifyManifestFreshness
	// consumes only canonical_identity, supplied from the streamed header.
	if opts.ManifestURL != "" {
		servedHeight, err := VerifyManifestFreshness(ctx, plan.Plan{CanonicalIdentity: hdr.CanonicalIdentity}, opts.ManifestURL, opts.Transport, httptransfer.DefaultPolicy())
		if err != nil {
			logger.Info("[apply] plan canonical block height: %d", hdr.CanonicalIdentity.BlockHeight)
			logger.Info("[apply] served canonical block height: %d", servedHeight)
			return exitcode.SupersededCanonical, err
		}
	}

	// 6. Derive paths.
	//
	// pocketdbRoot comes from plan.pocketdb_path (stamped by diagnose), so apply
	// targets the same datadir the operator diagnosed even when plan.json is
	// stored outside the datadir (pocketnet-node-doctor-x08). datadir is the
	// parent of pocketdbRoot — chainstate/, blocks/ live there. stagingRoot
	// deliberately lives next to plan.json (planDir) — staging is a doctor
	// artifact, not a datadir artifact.
	if hdr.PocketDBPath == "" {
		return exitcode.GenericError,
			fmt.Errorf("apply: plan is missing pocketdb_path; regenerate with the current diagnose")
	}
	planDir := filepath.Dir(opts.PlanPath)
	pocketdbRoot := hdr.PocketDBPath
	datadir := filepath.Dir(pocketdbRoot)
	stagingRoot := filepath.Join(planDir, "pocketnet-node-doctor-staging")

	// 7. Create/resume staging directory.
	stagingDir, resumed, err := staging.CreateOrResume(stagingRoot, hdr.SelfHash)
	if err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: create/resume staging: %w", err)
	}
	stagingDirStr := string(stagingDir)

	// Ensure staged/ subdir exists.
	stagedDir := filepath.Join(stagingDirStr, "staged")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return exitcode.GenericError, fmt.Errorf("apply: create staged dir: %w", err)
	}

	// 8. Log resume vs. fresh.
	if resumed {
		logger.Info("[apply] resuming: staging=%s plan=%s", stagingDirStr, hdr.SelfHash[:12])
	} else {
		logger.Info("[apply] applying plan: %d divergences, block_height=%d",
			len(headers), hdr.CanonicalIdentity.BlockHeight)
	}

	// 9. Pass 1: scan divergences to count pending work and collect one
	// shadowInfo per unique live path with pending work. Memory bound is
	// O(unique live paths) — typically low hundreds even for 16K-page
	// workloads — not O(workload).
	chunkBaseURL := deriveChunkBaseURL(opts)

	type shadowInfo struct {
		livePath    string
		planRelPath string
		isAbsent    bool
	}

	pendingByLivePath := make(map[string]*shadowInfo)
	var shadowInfoOrder []*shadowInfo
	pendingCount := 0

	addPending := func(livePath, planRelPath string, isAbsent bool) {
		if _, ok := pendingByLivePath[livePath]; !ok {
			si := &shadowInfo{livePath: livePath, planRelPath: planRelPath, isAbsent: isAbsent}
			pendingByLivePath[livePath] = si
			shadowInfoOrder = append(shadowInfoOrder, si)
		}
		pendingCount++
	}

	// sqlite_pages first (frozen emission order), pages streamed one at a time.
	if serr := streamSQLitePages(opts.PlanPath, func(idx int, pages iter.Seq2[plan.Page, error]) error {
		divPath := headers[idx].Path
		livePath := resolveLivePath(datadir, pocketdbRoot, divPath)
		for page, perr := range pages {
			if perr != nil {
				return perr
			}
			id := fmt.Sprintf("%s:%d", divPath, page.Offset)
			if staging.MarkerExists(stagingDirStr, id) {
				continue
			}
			addPending(livePath, divPath, false)
		}
		return nil
	}); serr != nil {
		return exitcode.GenericError, fmt.Errorf("apply: scan plan: %w", serr)
	}

	// whole_file group (from the small header set).
	for _, h := range headers {
		if h.Kind != plan.DivergenceKindWholeFile {
			continue
		}
		if staging.MarkerExists(stagingDirStr, h.Path) { // marker id = planRelPath
			continue
		}
		livePath := resolveLivePath(datadir, pocketdbRoot, h.Path)
		addPending(livePath, h.Path, h.ExpectedSource == "fetch_full")
	}

	// 10. If no pending tasks, we're done.
	if pendingCount == 0 {
		logger.Info("[apply] all tasks already complete — nothing to do")
		return exitcode.Success, nil
	}

	// 11. Take shadows for each unique live path that has pending tasks.
	var shadows []rollback.Shadow
	for _, si := range shadowInfoOrder {
		shadowPath := filepath.Join(stagingDirStr, "shadows", filepath.FromSlash(si.planRelPath))

		// A shadow is only taken if the live file exists. Absent files (either
		// explicitly absent via ExpectedSource="fetch_full", or files that simply
		// don't exist locally) have nothing to shadow-copy.
		liveExists := true
		if si.isAbsent {
			liveExists = false
		} else {
			if _, statErr := os.Stat(si.livePath); os.IsNotExist(statErr) {
				liveExists = false
			}
		}
		taken := liveExists

		if taken {
			// Only take shadow if not already taken (idempotent on resume).
			if _, statErr := os.Stat(shadowPath); os.IsNotExist(statErr) {
				if err := staging.TakeShadow(si.livePath, stagingDirStr, si.planRelPath); err != nil {
					return exitcode.GenericError, fmt.Errorf("apply: take shadow for %s: %w", si.planRelPath, err)
				}
			}
		}

		shadows = append(shadows, rollback.Shadow{
			ShadowPath: shadowPath,
			LivePath:   si.livePath,
			Taken:      taken,
		})
	}

	// 12. Start worker pool and stream tasks. The task channel is bounded by
	// `parallel` (not workload size) and an iter.Pull-driven feed pushes
	// exactly one fresh-or-retry task per result consumed. Memory in flight
	// is O(parallel) regardless of how many divergences the plan carries.
	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 4
	}

	var renameCount int32 // for InjectRenameFailOnNth
	var promotionsDone int32

	taskCh := make(chan DivergenceTask, parallel)
	resultCh := make(chan FetchResult, parallel)

	for i := 0; i < parallel; i++ {
		go runWorkerWithLogger(ctx, taskCh, resultCh, opts.Transport, httptransfer.DefaultPolicy(), logger)
	}

	nextTask, stopTask := iter.Pull(divergenceTaskIter(headers, opts.PlanPath, stagingDirStr, stagedDir, datadir, pocketdbRoot, chunkBaseURL))
	defer stopTask()

	// Prime: feed up to `parallel` initial tasks so workers have immediate work.
	for i := 0; i < parallel; i++ {
		t, ok := nextTask()
		if !ok {
			break
		}
		taskCh <- t
	}

	remaining := pendingCount
	var networkBudgetExhausted bool // fetch or hash-gate retries exhausted → exit 12, no rollback
	var promotionFailed bool        // rename/write failure → exit via rollback

	for remaining > 0 {
		select {
		case <-ctx.Done():
			close(taskCh)
			return exitcode.GenericError, ctx.Err()
		case result := <-resultCh:
			remaining--

			// retry holds an optional requeued task; consumed before pulling the
			// next iterator task so the bounded taskCh never has more than
			// `parallel` items in flight.
			var retry DivergenceTask
			var hasRetry bool

			if result.Err != nil {
				if result.Task.Attempt+1 >= maxAttempts {
					logger.Info("[apply] budget exhausted for %s: %v", result.Task.ChunkURL, result.Err)
					networkBudgetExhausted = true
				} else {
					retry = result.Task
					retry.Attempt++
					remaining++
					hasRetry = true
				}
			} else {
				// Promote.
				var requeued bool
				var promoteErr error

				if result.Task.PageOffset >= 0 {
					requeued, promoteErr = promoteSQLitePage(result, &renameCount, opts.InjectRenameFailOnNth)
				} else {
					requeued, promoteErr = promoteWholeFile(result, &renameCount, opts.InjectRenameFailOnNth)
				}

				switch {
				case promoteErr != nil:
					logger.Info("[apply] promote %s: %v", result.Task.PlanRelPath, promoteErr)
					promotionFailed = true
				case requeued:
					if result.Task.Attempt+1 >= maxAttempts {
						logger.Info("[apply] budget exhausted (hash mismatch) for %s", result.Task.PlanRelPath)
						networkBudgetExhausted = true
					} else {
						retry = result.Task
						retry.Attempt++
						remaining++
						hasRetry = true
					}
				default:
					atomic.AddInt32(&promotionsDone, 1)
					id := stagingIdentifier(result.Task)
					if markerErr := staging.WriteMarker(stagingDirStr, id); markerErr != nil {
						logger.Info("[apply] write marker for %s: %v", id, markerErr)
					}
				}
			}

			// Feed exactly one task per result consumed (retry or fresh).
			// This keeps the worker pool saturated while bounding in-flight
			// tasks to ≤ parallel and avoiding deadlock between taskCh and
			// resultCh writers.
			if hasRetry {
				taskCh <- retry
			} else if t, ok := nextTask(); ok {
				taskCh <- t
			}
		}
	}

	close(taskCh)

	// Promotion (rename) failure → rollback: the file system is in a
	// potentially inconsistent state (partial rename), so we must restore from
	// shadows.
	if promotionFailed {
		injectFaultBeforeRollback(opts, shadows)
		return executeRollback(shadows, logger), nil
	}

	// Network budget exhaustion → exit 12. Partial promotions and markers are
	// preserved so the next invocation can resume (EC-006 resumability). We do
	// NOT rollback since only fully-verified promotions are visible in pocketdb.
	if networkBudgetExhausted {
		return exitcode.NetworkBudgetExhausted, fmt.Errorf("apply: network budget exhausted")
	}

	// 17. Post-apply verification.
	logger.Info("[apply] verifying post-apply state...")

	// Inject SQLite corruption if requested.
	if opts.InjectSQLiteCorruptionForTest || opts.InjectSQLiteCorruptionAfterRename {
		sqlitePath := filepath.Join(pocketdbRoot, "main.sqlite3")
		if writeErr := os.WriteFile(sqlitePath, []byte("corrupted for test"), 0o644); writeErr != nil {
			logger.Info("[apply] corruption injection failed: %v", writeErr)
		}
	}

	// Build hash verification map: only whole_file entries. Keys are the full
	// plan-relative path (e.g. "pocketdb/web.sqlite3", "chainstate/CURRENT").
	// VerifyHashes resolves them via resolveLivePath against datadir.
	hashMap := make(map[string]string)
	hasSQLite := false
	for _, h := range headers {
		if h.Kind == plan.DivergenceKindWholeFile {
			hashMap[h.Path] = h.ExpectedHash
			if strings.HasSuffix(h.Path, "main.sqlite3") {
				hasSQLite = true
			}
		}
		if h.Kind == plan.DivergenceKindSQLitePages && strings.HasSuffix(h.Path, "main.sqlite3") {
			hasSQLite = true
		}
	}

	// InjectHashMismatchAfterPromoteForTest: corrupt all entries in the hash map
	// to force verification failure (corrupts every whole_file entry).
	if opts.InjectHashMismatchAfterPromoteForTest {
		for k := range hashMap {
			hashMap[k] = "0000000000000000000000000000000000000000000000000000000000000000"
		}
	}

	// InjectRollbackFaultForTest: force verification to fail so rollback is triggered,
	// then corrupt a shadow so rollback itself fails.
	if opts.InjectRollbackFaultForTest {
		for k := range hashMap {
			hashMap[k] = "0000000000000000000000000000000000000000000000000000000000000000"
		}
		if len(hashMap) == 0 {
			// No whole-file entries in hashMap; add a fake entry that will fail.
			hashMap["__inject_fault__"] = "0000000000000000000000000000000000000000000000000000000000000000"
		}
	}

	// Run SQLite integrity_check BEFORE hash verification: the integrity check
	// gives a more meaningful error message for SQLite corruption than a hash
	// mismatch would.
	if hasSQLite {
		sqlitePath := filepath.Join(pocketdbRoot, "main.sqlite3")
		logger.Info("[apply] integrity_check: %s", sqlitePath)
		if err := verify.VerifySQLite(sqlitePath); err != nil {
			logger.Info("[apply] integrity_check failed: %v", err)
			injectFaultBeforeRollback(opts, shadows)
			return executeRollback(shadows, logger), nil
		}
	}

	// Hash verification: exclude main.sqlite3 (already covered by integrity_check
	// above) to avoid double-reporting on the same file.
	if hasSQLite {
		delete(hashMap, "pocketdb/main.sqlite3")
	}

	if len(hashMap) > 0 {
		// Resolve each plan-relative path to an absolute path via resolveLivePath
		// so that non-"pocketdb/" paths (e.g. "chainstate/", "blocks/") resolve
		// to the datadir level rather than inside pocketdbRoot.
		absHashMap := make(map[string]string, len(hashMap))
		for planPath, hash := range hashMap {
			absHashMap[resolveLivePath(datadir, pocketdbRoot, planPath)] = hash
		}
		if err := verify.VerifyHashes("", absHashMap); err != nil {
			logger.Info("[apply] hash verification failed: %v", err)
			injectFaultBeforeRollback(opts, shadows)
			return executeRollback(shadows, logger), nil
		}
	}

	// 18. Log success.
	logger.Info("[apply] success: all divergences resolved")

	// 19. Cleanup.
	cleanupStaging(stagingDirStr, shadows, logger)

	// 20. DiskCostObserver.
	if opts.DiskCostObserver != nil {
		opts.DiskCostObserver(0, 0, 0)
	}

	return exitcode.Success, nil
}
