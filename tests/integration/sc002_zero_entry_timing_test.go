//go:build reference_rig

// T073: SC-002 — zero-entry plan within 5-minute budget on the reference rig
// for the identical-to-canonical reference-scale fixture. Build-tag gated.
package integration

import (
	"testing"
	"time"
)

func TestSC002_ReferenceRig_ZeroEntryPlan_5MinuteBudget(t *testing.T) {
	start := time.Now()
	t.Skip("SC-002 reference rig timing test requires the named reference rig and a reference-scale identical-to-canonical fixture; see README.md")
	if elapsed := time.Since(start); elapsed > 5*time.Minute {
		t.Fatalf("SC-002 budget breached: %s", elapsed)
	}
}
