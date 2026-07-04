package diagnose

import (
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// ProgressEmitter emits class-entry, in-progress, and class-exit messages
// per D10. Cadence:
//   - main.sqlite3: every 5% of pages
//   - blocks/, chainstate/, indexes/: every 25 files
type ProgressEmitter struct {
	logger *stderrlog.Logger
}

func NewProgressEmitter(logger *stderrlog.Logger) *ProgressEmitter {
	return &ProgressEmitter{logger: logger}
}

func (e *ProgressEmitter) Enter(class string) time.Time {
	if e == nil {
		return time.Now()
	}
	e.logger.Info("[diagnose] hashing %s...", class)
	return time.Now()
}

func (e *ProgressEmitter) MainSQLitePage(n, total int) {
	if e == nil {
		return
	}
	if total <= 0 {
		return
	}
	if n != total && n%step5pct(total) != 0 {
		return
	}
	pct := (n * 100) / total
	e.logger.Info("[diagnose] hashing main.sqlite3 pages: %d / %d (%d%%)", n, total, pct)
}

func (e *ProgressEmitter) FileBatch(class string, n, total int) {
	if e == nil {
		return
	}
	if n%25 != 0 && n != total {
		return
	}
	e.logger.Info("[diagnose] hashing %s: %d / %d files", class, n, total)
}

func (e *ProgressEmitter) Exit(class string, started time.Time) {
	if e == nil {
		return
	}
	elapsed := time.Since(started).Round(time.Millisecond)
	e.logger.Info("[diagnose] hashed %s in %s", class, elapsed)
}

func step5pct(total int) int {
	step := total / 20 // 5% = 1/20
	if step < 1 {
		step = 1
	}
	return step
}
