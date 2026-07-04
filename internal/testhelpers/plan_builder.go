package testhelpers

import (
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// PlanBuilder constructs a plan.Plan and serializes it to valid plan.json bytes
// with a correct self_hash.
type PlanBuilder struct {
	manifestHash string
	blockHeight  int64
	pocketdbPath string
	divergences  []plan.Divergence
}

// NewPlanBuilder creates a PlanBuilder for a plan referencing the given
// canonical manifest hash and block height.
func NewPlanBuilder(canonicalManifestHash string, blockHeight int64) *PlanBuilder {
	return &PlanBuilder{
		manifestHash: canonicalManifestHash,
		blockHeight:  blockHeight,
	}
}

// AddWholeFileDivergence appends a whole_file divergence that requires
// a replacement fetch (expected_source is empty, implying a full-file fetch
// of the named path).
func (b *PlanBuilder) AddWholeFileDivergence(planRelPath, expectedHash string) *PlanBuilder {
	b.divergences = append(b.divergences, plan.Divergence{
		Kind:         plan.DivergenceKindWholeFile,
		Path:         planRelPath,
		ExpectedHash: expectedHash,
	})
	return b
}

// AddWholeFileAbsent appends a whole_file divergence with
// expected_source "fetch_full", indicating the file is absent locally.
func (b *PlanBuilder) AddWholeFileAbsent(planRelPath, expectedHash string) *PlanBuilder {
	b.divergences = append(b.divergences, plan.Divergence{
		Kind:           plan.DivergenceKindWholeFile,
		Path:           planRelPath,
		ExpectedHash:   expectedHash,
		ExpectedSource: "fetch_full",
	})
	return b
}

// WithPocketDBPath records the pocketdb directory the plan was diagnosed
// against. apply requires this field (plan.PocketDBPath); tests that go through
// apply.Run must set it to the pocketdb directory their fixture uses.
func (b *PlanBuilder) WithPocketDBPath(pocketdbPath string) *PlanBuilder {
	b.pocketdbPath = pocketdbPath
	return b
}

// AddSQLitePageDivergence appends a sqlite_pages divergence for the given
// plan-relative path with the supplied page list.
func (b *PlanBuilder) AddSQLitePageDivergence(planRelPath string, pages []plan.Page) *PlanBuilder {
	b.divergences = append(b.divergences, plan.Divergence{
		Kind:  plan.DivergenceKindSQLitePages,
		Path:  planRelPath,
		Pages: pages,
	})
	return b
}

// Build serializes the plan to canonical JSON bytes with a valid self_hash.
// Calls t.Fatal on any error.
func (b *PlanBuilder) Build(t testing.TB) []byte {
	t.Helper()

	p := plan.Plan{
		FormatVersion: plan.FormatVersion,
		CanonicalIdentity: plan.CanonicalIdentity{
			BlockHeight:  b.blockHeight,
			ManifestHash: b.manifestHash,
		},
		PocketDBPath: b.pocketdbPath,
		Divergences:  b.divergences,
	}

	hash, err := plan.ComputeSelfHash(p)
	if err != nil {
		t.Fatalf("PlanBuilder.Build: ComputeSelfHash: %v", err)
	}
	p.SelfHash = hash

	data, err := plan.Marshal(p)
	if err != nil {
		t.Fatalf("PlanBuilder.Build: Marshal: %v", err)
	}
	return data
}
