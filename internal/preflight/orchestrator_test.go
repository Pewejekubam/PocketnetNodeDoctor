package preflight

import (
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// T043: EvaluateOrdered runs predicates in order; STOPS at first refusal.
func TestEvaluateOrdered_StopsAtFirstRefusal(t *testing.T) {
	var calls []string
	preds := []Predicate{
		{Name: "first", Fn: func(PreflightContext) PredicateResult {
			calls = append(calls, "first")
			return Refused(exitcode.RunningNode, "first refused")
		}},
		{Name: "second", Fn: func(PreflightContext) PredicateResult {
			calls = append(calls, "second")
			return Pass()
		}},
		{Name: "third", Fn: func(PreflightContext) PredicateResult {
			calls = append(calls, "third")
			return Pass()
		}},
	}
	name, res, invocations := EvaluateOrdered(PreflightContext{}, preds)
	if name != "first" {
		t.Errorf("first refusing predicate: got %q want first", name)
	}
	if res.Pass {
		t.Errorf("want refuse")
	}
	if invocations != 1 {
		t.Errorf("want exactly 1 invocation; got %d (calls: %v)", invocations, calls)
	}
	if len(calls) != 1 || calls[0] != "first" {
		t.Errorf("subsequent predicates must NOT run; got %v", calls)
	}
}

func TestEvaluateOrdered_AllPass_ReturnsPass(t *testing.T) {
	preds := []Predicate{
		{Name: "a", Fn: func(PreflightContext) PredicateResult { return Pass() }},
		{Name: "b", Fn: func(PreflightContext) PredicateResult { return Pass() }},
	}
	name, res, invocations := EvaluateOrdered(PreflightContext{}, preds)
	if name != "" {
		t.Errorf("name on all-pass should be empty; got %q", name)
	}
	if !res.Pass {
		t.Errorf("want pass")
	}
	if invocations != 2 {
		t.Errorf("want 2 invocations; got %d", invocations)
	}
}

// Canonical order: post-manifest is version-mismatch -> capacity -> permission.
func TestPostManifestOrder_CanonicalSequence(t *testing.T) {
	got := PostManifestOrder()
	want := []string{"version-mismatch", "volume-capacity", "permission-readonly"}
	if len(got) != len(want) {
		t.Fatalf("got %d predicates want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Name != want[i] {
			t.Errorf("position %d: got %q want %q", i, got[i].Name, want[i])
		}
	}
}

func TestPreManifest_IsRunningNode(t *testing.T) {
	if PreManifest().Name != "running-node" {
		t.Errorf("PreManifest must be running-node; got %q", PreManifest().Name)
	}
}
