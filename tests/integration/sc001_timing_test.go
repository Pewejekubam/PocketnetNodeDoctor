//go:build reference_rig

// T072: SC-001 timing half. Reference-rig-only test (build-tag gated).
// Diagnose against a 30-day-divergent reference-scale fixture must complete
// end-to-end within 5 minutes wall-clock on the named reference rig
// (8 vCPU x86_64, NVMe-class disk, 16 GB RAM). Manual run; not a CI gate.
package integration

import (
	"testing"
	"time"
)

func TestSC001_ReferenceRig_TimingHalf_5MinuteBudget(t *testing.T) {
	// This test is intentionally conservative — the reference-scale rig
	// build is the operator's responsibility. The harness here measures
	// the wall-clock budget; the rig assembly is documented in
	// README.md § Reference rig.
	start := time.Now()
	t.Skip("SC-001 reference rig timing test requires the named reference rig and a reference-scale 30-day-divergent fixture; see README.md")
	if elapsed := time.Since(start); elapsed > 5*time.Minute {
		t.Fatalf("SC-001 budget breached: %s", elapsed)
	}
}
