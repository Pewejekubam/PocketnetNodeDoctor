// T019: unit test for apply worker dispatch — DivergenceTask, FetchResult,
// and RunWorker.
package apply_test

import (
	"context"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

func TestWorkerDispatch(t *testing.T) {
	ctx := context.Background()

	tasks := make(chan apply.DivergenceTask, 1)
	results := make(chan apply.FetchResult)

	tasks <- apply.DivergenceTask{
		PlanRelPath:  "pocketdb/blocks/00000000.dat",
		ChunkURL:     "http://example.com/chunks/ab/c0/abc0000000000000000000000000000000000000000000000000000000000000.zst",
		ExpectedHash: "abc123",
		StagingPath:  "/tmp/staged/abc",
	}
	close(tasks)

	go apply.RunWorker(ctx, tasks, results, nil, httptransfer.DefaultPolicy())

	result := <-results
	if result.PlanRelPath != "pocketdb/blocks/00000000.dat" {
		t.Errorf("FetchResult.PlanRelPath = %q, want %q",
			result.PlanRelPath, "pocketdb/blocks/00000000.dat")
	}
}
