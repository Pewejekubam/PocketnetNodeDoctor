package httptransfer

import (
	"context"
	"io"
	"time"
)

// stallGuardReader wraps an io.ReadCloser and cancels the stall context if no
// Read progress occurs within threshold. ctx is the stall context (child of the
// caller's context), allowing both per-read timer and parent-cancel to abort.
type stallGuardReader struct {
	body      io.ReadCloser
	cancel    context.CancelCauseFunc
	threshold time.Duration
	ctx       context.Context
}

// Read arms a per-read stall timer before delegating to the inner body. The
// timer fires cancelCause(TransferStalledError) if the body Read takes longer
// than threshold. A goroutine races the body read against context cancellation
// so that both timer-fired stalls and parent-context cancels abort promptly.
func (r *stallGuardReader) Read(p []byte) (int, error) {
	timer := time.AfterFunc(r.threshold, func() {
		r.cancel(TransferStalledError{Threshold: r.threshold})
	})
	defer timer.Stop()

	type res struct {
		n   int
		err error
	}
	ch := make(chan res, 1)
	go func() {
		n, err := r.body.Read(p)
		ch <- res{n, err}
	}()

	select {
	case result := <-ch:
		return result.n, result.err
	case <-r.ctx.Done():
		return 0, context.Cause(r.ctx)
	}
}

func (r *stallGuardReader) Close() error {
	return r.body.Close()
}

// WrapBodyWithStallGuard wraps body with per-read stall detection using
// policy.BodyStallThreshold. A child stall context is created from ctx; the
// returned cancel function stops the guard and releases the stall context.
func WrapBodyWithStallGuard(ctx context.Context, body io.ReadCloser, policy TransferPolicy) (io.ReadCloser, context.CancelFunc) {
	stallCtx, cancelCause := context.WithCancelCause(ctx)
	r := &stallGuardReader{
		body:      body,
		cancel:    cancelCause,
		threshold: policy.BodyStallThreshold,
		ctx:       stallCtx,
	}
	return r, func() { cancelCause(nil) }
}
