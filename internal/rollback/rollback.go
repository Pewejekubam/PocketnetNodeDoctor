// Package rollback restores the pre-apply pocketdb state from shadow copies.
package rollback

import "os"

// Shadow represents a pre-apply copy of a live file.
type Shadow struct {
	ShadowPath string // path to the shadow copy in staging/shadows/
	LivePath   string // original live path in pocketdb/
	Taken      bool   // false for absent-file entries (no shadow was taken)
}

// RollbackStatus indicates the outcome of a rollback attempt.
type RollbackStatus int

const (
	ResultCompleted RollbackStatus = iota // all shadows restored
	ResultFailed                          // one or more shadows could not be restored
)

// RollbackResult carries the rollback outcome and any unrestored file paths.
type RollbackResult struct {
	Status      RollbackStatus
	FailedPaths []string // live paths that could not be restored (non-empty on ResultFailed)
}

// Rollback restores the pre-apply pocketdb state by renaming each shadow back
// to its live path (in reverse order). Only shadows with Taken=true are processed.
// Returns ResultCompleted if all succeeded, ResultFailed if any os.Rename failed.
func Rollback(shadows []Shadow) RollbackResult {
	var failedPaths []string
	for i := len(shadows) - 1; i >= 0; i-- {
		s := shadows[i]
		if !s.Taken {
			continue
		}
		if err := os.Rename(s.ShadowPath, s.LivePath); err != nil {
			failedPaths = append(failedPaths, s.LivePath)
		}
	}
	if len(failedPaths) > 0 {
		return RollbackResult{Status: ResultFailed, FailedPaths: failedPaths}
	}
	return RollbackResult{Status: ResultCompleted}
}
