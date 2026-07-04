package preflight

// Predicate is a named refusal check.
type Predicate struct {
	Name string
	Fn   func(PreflightContext) PredicateResult
}

// PostManifestOrder returns the three predicates that fire AFTER the manifest
// has been fetched + verified, in canonical order:
//
//	version-mismatch -> volume-capacity -> permission-readonly.
//
// The running-node predicate fires BEFORE manifest fetch (D8) and is exposed
// separately as PreManifest.
func PostManifestOrder() []Predicate {
	return []Predicate{
		{Name: "version-mismatch", Fn: VersionMismatch},
		{Name: "volume-capacity", Fn: VolumeCapacity},
		{Name: "permission-readonly", Fn: PermissionReadOnly},
	}
}

// PreManifest is the running-node predicate — the only refusal check that
// fires before the manifest is fetched.
func PreManifest() Predicate {
	return Predicate{Name: "running-node", Fn: RunningNode}
}

// EvaluateOrdered runs predicates in order with stop-at-first-refusal.
// Returns the first refusing predicate's name and result, or ("", Pass())
// when every predicate passes. The 'invocations' return is the count of
// predicates actually invoked (assertable in tests for stop-at-first-refusal).
func EvaluateOrdered(ctx PreflightContext, predicates []Predicate) (string, PredicateResult, int) {
	for i, p := range predicates {
		res := p.Fn(ctx)
		if !res.Pass {
			return p.Name, res, i + 1
		}
	}
	return "", Pass(), len(predicates)
}
