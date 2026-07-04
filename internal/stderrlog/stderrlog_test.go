package stderrlog

import (
	"bytes"
	"strings"
	"testing"
)

// T009: Info always writes; Debug gated on verbose; output goes to the
// configured writer (defaults to os.Stderr); nothing to stdout (D16).
func TestLogger_InfoAlwaysWrites(t *testing.T) {
	var stderr bytes.Buffer
	l := NewWith(&stderr, false)
	l.Info("hello %s", "world")
	if !strings.Contains(stderr.String(), "hello world") {
		t.Errorf("Info output missing: %q", stderr.String())
	}
}

func TestLogger_DebugGatedOnVerbose(t *testing.T) {
	var stderr bytes.Buffer
	l := NewWith(&stderr, false)
	l.Debug("secret")
	if stderr.Len() != 0 {
		t.Errorf("Debug wrote when verbose=false: %q", stderr.String())
	}

	stderr.Reset()
	l = NewWith(&stderr, true)
	l.Debug("secret %d", 42)
	if !strings.Contains(stderr.String(), "secret 42") {
		t.Errorf("Debug missing under verbose=true: %q", stderr.String())
	}
}

func TestLogger_DefaultStderr(t *testing.T) {
	// New() returns a logger writing to os.Stderr; we don't capture os.Stderr
	// here, but we verify the constructor returns a non-nil logger and that
	// it doesn't panic.
	l := New(false)
	if l == nil {
		t.Fatal("New returned nil")
	}
	// Smoke: call Info; it goes to os.Stderr. Test stdout silence is verified
	// at the orchestrator level (T067).
}
