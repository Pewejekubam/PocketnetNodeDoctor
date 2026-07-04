// Package preflight implements the doctor's five refusal predicates that
// fire BEFORE any byte of pocketdb is read or modified. Predicates are
// evaluated by the orchestrator in canonical order with stop-at-first-refusal:
// the first refusing predicate's Refuse{Code,Diagnostic} is returned and
// subsequent predicates are NOT invoked.
package preflight

import (
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// PredicateResult is a sum type: Pass means the predicate did not trip;
// Refuse carries the typed exit code and the human-readable diagnostic the
// orchestrator emits to stderr before exiting.
type PredicateResult struct {
	Pass    bool
	Refused *Refuse
}

type Refuse struct {
	Code       exitcode.Code
	Diagnostic string
}

func Pass() PredicateResult { return PredicateResult{Pass: true} }

func Refused(code exitcode.Code, diagnostic string) PredicateResult {
	return PredicateResult{Pass: false, Refused: &Refuse{Code: code, Diagnostic: diagnostic}}
}

// PreflightContext is the read-only input each predicate receives.
type PreflightContext struct {
	PocketDBPath string
	Manifest     *manifest.Manifest
	Logger       *stderrlog.Logger
}
