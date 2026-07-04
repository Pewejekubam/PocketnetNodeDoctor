package apply

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/fetch"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// RunWorker reads DivergenceTasks from tasks, calls fetch.FetchWithRetry for
// each, and sends FetchResult (with Err set on failure) to results. The
// goroutine exits when tasks is closed or ctx is cancelled.
// When transport is nil, http.DefaultTransport is used.
func RunWorker(ctx context.Context, tasks <-chan DivergenceTask, results chan<- FetchResult, transport http.RoundTripper, policy httptransfer.TransferPolicy) {
	runWorkerWithLogger(ctx, tasks, results, transport, policy, stderrlog.NewWith(nil, false))
}

// runWorkerWithLogger is like RunWorker but accepts a logger for retry messages.
func runWorkerWithLogger(ctx context.Context, tasks <-chan DivergenceTask, results chan<- FetchResult, transport http.RoundTripper, policy httptransfer.TransferPolicy, logger *stderrlog.Logger) {
	if transport == nil {
		transport = http.DefaultTransport
	}
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-tasks:
			if !ok {
				return
			}
			logLabel := task.PlanRelPath
			if task.PageOffset >= 0 {
				logLabel = task.PlanRelPath + ":" + fmt.Sprintf("%d", task.PageOffset)
			}
			err := fetch.FetchWithRetry(ctx, task.ChunkURL, task.StagingPath, logLabel, transport, policy, logger)
			results <- FetchResult{
				Task:        task,
				Path:        task.StagingPath,
				PlanRelPath: task.PlanRelPath,
				Err:         err,
			}
		}
	}
}
