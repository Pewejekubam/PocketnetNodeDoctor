package diagnose

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// T066: D10 progress messages on stderr — class-entry, 5%-cadence for
// main.sqlite3, 25-files-cadence for blocks/chainstate/indexes, class-exit.
func TestProgressEmitter_ClassEntryAndExit(t *testing.T) {
	var buf bytes.Buffer
	logger := stderrlog.NewWith(&buf, false)
	e := NewProgressEmitter(logger)

	started := e.Enter("blocks")
	e.Exit("blocks", started)

	out := buf.String()
	if !strings.Contains(out, "[diagnose] hashing blocks...") {
		t.Errorf("class-entry missing: %q", out)
	}
	if !strings.Contains(out, "[diagnose] hashed blocks in") {
		t.Errorf("class-exit missing: %q", out)
	}
}

func TestProgressEmitter_MainSQLite_5PercentCadence(t *testing.T) {
	var buf bytes.Buffer
	logger := stderrlog.NewWith(&buf, false)
	e := NewProgressEmitter(logger)

	// Total 200 pages → 5% = 10 pages → emit at 10, 20, 30, ..., 200 plus
	// at-or-before-end. Verify we get fewer than 200 emissions.
	for n := 1; n <= 200; n++ {
		e.MainSQLitePage(n, 200)
	}
	count := strings.Count(buf.String(), "[diagnose] hashing main.sqlite3 pages:")
	if count == 0 || count >= 200 {
		t.Errorf("emission count %d; want sparse 5%%-cadence", count)
	}
}

func TestProgressEmitter_FileBatch_25Cadence(t *testing.T) {
	var buf bytes.Buffer
	logger := stderrlog.NewWith(&buf, false)
	e := NewProgressEmitter(logger)

	for n := 1; n <= 100; n++ {
		e.FileBatch("blocks", n, 100)
	}
	count := strings.Count(buf.String(), "[diagnose] hashing blocks:")
	// Expected emissions at n = 25, 50, 75, 100 = 4 emissions.
	if count < 3 || count > 5 {
		t.Errorf("emission count %d; want ~4 (25-cadence)", count)
	}
}

func TestProgressEmitter_NilSafe(t *testing.T) {
	// Should not panic.
	var e *ProgressEmitter
	_ = e.Enter("x")
	e.Exit("x", time.Now())
	e.MainSQLitePage(1, 100)
	e.FileBatch("blocks", 1, 100)
}
