package httptransfer

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// slowNetBody simulates a body where each Read takes readDelay before returning
// data, up to totalReads calls.
type slowNetBody struct {
	readDelay  time.Duration
	totalReads int
	reads      int
}

func (b *slowNetBody) Read(p []byte) (int, error) {
	time.Sleep(b.readDelay)
	b.reads++
	if len(p) > 0 {
		p[0] = 0x42
	}
	if b.reads >= b.totalReads {
		return 1, io.EOF
	}
	return 1, nil
}
func (b *slowNetBody) Close() error { return nil }

// blockingBody blocks indefinitely on Read until unblock is closed.
type blockingBody struct {
	unblock chan struct{}
}

func (b *blockingBody) Read(p []byte) (int, error) {
	<-b.unblock
	return 0, io.EOF
}
func (b *blockingBody) Close() error { return nil }

// immediateBody returns n bytes immediately, then io.EOF.
type immediateBody struct{ remaining int }

func (b *immediateBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > b.remaining {
		n = b.remaining
	}
	for i := range p[:n] {
		p[i] = 0x42
	}
	b.remaining -= n
	if b.remaining == 0 {
		return n, io.EOF
	}
	return n, nil
}
func (b *immediateBody) Close() error { return nil }

// TestStallGuardReader_TimerFires verifies that when a body Read takes longer
// than BodyStallThreshold, the Read returns a stall error.
//
// Uses the unexported stallGuardReader directly (same-package test) to construct
// the guard with a known cancel context for inspection.
//
// RED against the Phase 1 stub: stub.Read calls r.body.Read without arming any
// timer, so the body returns at 200ms with no error. The assertion
// "err == nil → expected stall error" fails immediately.
func TestStallGuardReader_TimerFires(t *testing.T) {
	ctx, cancelCause := context.WithCancelCause(context.Background())
	defer cancelCause(nil)

	policy := TransferPolicy{BodyStallThreshold: 50 * time.Millisecond}
	body := &slowNetBody{readDelay: 200 * time.Millisecond, totalReads: 1}
	r := &stallGuardReader{
		body:      body,
		cancel:    cancelCause,
		threshold: policy.BodyStallThreshold,
		ctx:       ctx,
	}

	buf := make([]byte, 1)
	_, err := r.Read(buf)

	if err == nil {
		t.Fatalf("TimerFires: expected stall error after %v; got nil", policy.BodyStallThreshold)
	}
	if !errors.Is(err, ErrTransferStalled) {
		t.Errorf("TimerFires: err = %v; want chain containing ErrTransferStalled", err)
	}
	if ctx.Err() != context.Canceled {
		t.Errorf("TimerFires: ctx.Err() = %v; want context.Canceled", ctx.Err())
	}
	if !errors.Is(context.Cause(ctx), ErrTransferStalled) {
		t.Errorf("TimerFires: context.Cause = %v; want chain containing ErrTransferStalled", context.Cause(ctx))
	}
}

// TestStallGuardReader_NoStallOnFastRead verifies that a body that returns
// immediately does not trip the stall guard.
func TestStallGuardReader_NoStallOnFastRead(t *testing.T) {
	policy := TransferPolicy{BodyStallThreshold: 100 * time.Millisecond}
	body := &immediateBody{remaining: 64}
	ctx := context.Background()

	guardedBody, cancel := WrapBodyWithStallGuard(ctx, body, policy)
	defer cancel()

	buf := make([]byte, 64)
	_, err := guardedBody.Read(buf)
	if err != nil && err != io.EOF {
		t.Errorf("NoStallOnFastRead: unexpected error: %v", err)
	}
}

// TestStallGuardReader_ParentCancelAborts verifies that cancelling the parent
// context aborts a blocked Read promptly.
//
// RED against the Phase 1 stub: WrapBodyWithStallGuard returns the unmodified
// body; the stub Read blocks forever on the body without watching ctx.Done.
// The select in the test times out after 500ms — FAIL.
func TestStallGuardReader_ParentCancelAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	unblock := make(chan struct{})
	body := &blockingBody{unblock: unblock}
	defer close(unblock)

	policy := TransferPolicy{BodyStallThreshold: 10 * time.Second}
	guardedBody, guardCancel := WrapBodyWithStallGuard(ctx, body, policy)
	defer guardCancel()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := guardedBody.Read(buf)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("ParentCancelAborts: expected non-nil error after parent cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("ParentCancelAborts: Read did not abort within 500ms after parent cancel")
	}
}

// TestStallGuardReader_ResetOnReturn verifies that the per-read timer resets on
// each Read return: two sequential reads that each take 80ms (below the 100ms
// threshold) both succeed — timing does not accumulate across reads.
func TestStallGuardReader_ResetOnReturn(t *testing.T) {
	policy := TransferPolicy{BodyStallThreshold: 100 * time.Millisecond}
	body := &slowNetBody{readDelay: 80 * time.Millisecond, totalReads: 2}
	ctx := context.Background()

	guardedBody, cancel := WrapBodyWithStallGuard(ctx, body, policy)
	defer cancel()

	buf := make([]byte, 1)
	for i := 0; i < 2; i++ {
		_, err := guardedBody.Read(buf)
		if err != nil && err != io.EOF {
			t.Errorf("ResetOnReturn: read %d: unexpected error: %v", i, err)
		}
	}
}
