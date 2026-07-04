// Package apply implements the mutating apply pathway: fetch, stage, promote,
// verify, rollback.
package apply

import (
	"net/http"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// DivergenceTask is a single unit of work: fetch one chunk and promote it.
type DivergenceTask struct {
	PlanRelPath  string
	ChunkURL     string
	ExpectedHash string
	StagingPath  string
	LivePath     string
	Attempt      int
	IsAbsentFile bool
	PageOffset   int64 // -1 for whole_file; >= 0 for sqlite_pages page
}

// FetchResult is the outcome of one worker fetch attempt.
type FetchResult struct {
	Task        DivergenceTask
	Path        string // staging path (same as Task.StagingPath)
	PlanRelPath string // mirrors Task.PlanRelPath for convenience
	Err         error
}

// Options configures an apply.Run invocation.
type Options struct {
	PlanPath          string
	Parallel          int
	Logger            *stderrlog.Logger
	Transport         http.RoundTripper
	ManifestURL       string // for EC-005; empty = skip check
	ChunkStoreBaseURL string // base URL for chunks; empty = derive from ManifestURL host

	// Test injection hooks (not exposed via CLI).
	InjectSQLiteCorruptionForTest         bool // corrupt main.sqlite3 after renames, before integrity_check
	InjectRollbackFaultForTest            bool // force rollback path + make one rollback rename fail
	InjectRenameFailOnNth                 int  // 0 = disabled; N = fail Nth os.Rename in promotion path
	InjectSQLiteCorruptionAfterRename     bool // same effect as InjectSQLiteCorruptionForTest
	InjectHashMismatchAfterPromoteForTest bool // force verify.VerifyHashes to return an error
	DiskCostObserver                      func(pocketdbBytes, stagingBytes, shadowBytes int64)
}
