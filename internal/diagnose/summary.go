package diagnose

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// throughputBytesPerSecond is D9's fixed ETA constant: 50 MiB/s.
const throughputBytesPerSecond = 50 * 1024 * 1024

// EmitSummary writes the human-readable summary to w per cli-surface.md
// § Summary. Zero-divergence variant: "no recovery needed: local pocketdb
// matches canonical bitwise.". Non-zero: "divergent: <N> pages across <M>
// files; <bytes> to fetch; ETA ~<T> at 50 MiB/s.".
//
// The byte total is a lower bound for whole_file divergences (v1 manifest
// has no per-entry size; apply uses Content-Length on chunk-store fetch).
func EmitSummary(w io.Writer, p plan.Plan) {
	var pages, files int64
	for _, d := range p.Divergences {
		switch d.Kind {
		case plan.DivergenceKindSQLitePages:
			pages += int64(len(d.Pages))
			files++
		case plan.DivergenceKindWholeFile:
			files++
		}
	}
	emitSummaryCounts(w, pages, files)
}

// emitSummaryCounts writes the summary from running divergence counts so the
// streaming orchestrator need not hold a materialized plan.Plan. pages is the
// total sqlite_pages page count; files is the total divergence count (both
// kinds). Byte-identical output to EmitSummary for the same divergences.
func emitSummaryCounts(w io.Writer, pages, files int64) {
	if files == 0 {
		fmt.Fprintln(w, "no recovery needed: local pocketdb matches canonical bitwise.")
		return
	}
	// whole_file size is unknown until apply-time Content-Length; the byte total
	// counts only sqlite page-bytes (a lower bound).
	bytesToFetch := pages * 4096
	eta := time.Duration(float64(bytesToFetch) / float64(throughputBytesPerSecond) * float64(time.Second))
	fmt.Fprintf(w, "divergent: %d pages across %d files; %s to fetch; ETA ~%s at 50 MiB/s.\n",
		pages, files, humanIEC(uint64(bytesToFetch)), humanDuration(eta))
}

// humanIEC renders bytes in IEC binary units: KiB, MiB, GiB, TiB.
func humanIEC(b uint64) string {
	const (
		KiB = uint64(1) << 10
		MiB = uint64(1) << 20
		GiB = uint64(1) << 30
		TiB = uint64(1) << 40
	)
	switch {
	case b >= TiB:
		return fmt.Sprintf("%.1f TiB", float64(b)/float64(TiB))
	case b >= GiB:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(GiB))
	case b >= MiB:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(MiB))
	case b >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(KiB))
	}
	return fmt.Sprintf("%d B", b)
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	parts := []string{}
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	parts = append(parts, fmt.Sprintf("%ds", s))
	return strings.Join(parts, "")
}
