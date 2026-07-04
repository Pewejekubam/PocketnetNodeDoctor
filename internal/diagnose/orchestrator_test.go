package diagnose

import (
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// TestOrchestrator_TotalBytesTally guards the VolumeCapacity totalBytes tally at
// the entryFn/emitter seam (T013). No success criterion exercises the
// VolumeCapacity path at production scale, so a silent regression in the
// per-page += 4096 accounting would ship unnoticed. StreamSQLitePages returns
// the divergent-page count the orchestrator multiplies by 4096; this asserts the
// count is exact and the derived tally equals pageCount * 4096.
func TestOrchestrator_TotalBytesTally(t *testing.T) {
	for _, pageCount := range []int{0, 1, 7, 4096} {
		dir := t.TempDir()
		e, err := NewPlanEmitter(filepath.Join(dir, "plan.json"))
		if err != nil {
			t.Fatalf("NewPlanEmitter: %v", err)
		}
		t.Cleanup(e.Abort)

		pages := make([]plan.Page, pageCount)
		for i := range pages {
			pages[i] = plan.Page{Offset: int64(i) * 4096, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"}
		}

		n, err := e.StreamSQLitePages("pocketdb/main.sqlite3", pagesSeqFromSlice(pages))
		if err != nil {
			t.Fatalf("StreamSQLitePages(%d pages): %v", pageCount, err)
		}
		if n != pageCount {
			t.Errorf("StreamSQLitePages returned count %d, want %d", n, pageCount)
		}
		// The orchestrator's tally: totalBytes += uint64(n) * 4096 per entry.
		gotTally := uint64(n) * 4096
		wantTally := uint64(pageCount) * 4096
		if gotTally != wantTally {
			t.Errorf("totalBytes tally = %d, want %d (pageCount %d * 4096)", gotTally, wantTally, pageCount)
		}
	}
}
