package preflight

import (
	"fmt"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// statFS returns (free, total, error). Implementations are platform-split
// in volume_capacity_unix.go and volume_capacity_windows.go.
var statFS func(path string) (free, total uint64, err error) = statfsImpl

// VolumeCapacity refuses with exit code 5 when free volume capacity is less
// than 2 × Σ(manifest entry sizes). Required factor of 2 covers the worst
// case: keep all current bytes while staging a full canonical copy (D-doc
// data-model.md row).
func VolumeCapacity(ctx PreflightContext) PredicateResult {
	if ctx.Manifest == nil {
		return Refused(exitcode.GenericError, "volume-capacity: nil manifest")
	}
	return VolumeCapacityFromTotalBytes(ctx.PocketDBPath, requiredBytes(ctx.Manifest))
}

// VolumeCapacityFromTotalBytes is the scalar-arg companion to VolumeCapacity,
// taking the total entry-bytes count directly. Used by the streaming diagnose
// orchestrator, which counts page-bytes (4096 × pageCount) inside the entry
// callback as the manifest streams (no materialized *manifest.Manifest
// available). Refuses when free space is less than 2 × totalBytes.
func VolumeCapacityFromTotalBytes(pocketdbPath string, totalBytes uint64) PredicateResult {
	required := totalBytes * 2
	free, _, err := statFS(pocketdbPath)
	if err != nil {
		return Refused(exitcode.GenericError, fmt.Sprintf("volume-capacity: statfs(%q): %v", pocketdbPath, err))
	}
	if free < required {
		return Refused(exitcode.Capacity, fmt.Sprintf("volume-capacity: %s has %d bytes free; needs %d (2x manifest size)", pocketdbPath, free, required))
	}
	return Pass()
}

// requiredBytes sums all manifest entry sizes. For sqlite_pages entries the
// size is len(pages) * 4096; for whole_file entries the size is approximated
// from the manifest hash field (we don't have a size field in v1, so this is
// a best-effort lower bound — the doctrine of "2x" gives margin). v1 manifest
// schema does not carry per-entry sizes; this implementation uses a 4096
// page-size approximation for sqlite_pages and treats whole_file entries as
// 0 (the 2x factor on the SQLite page sum dominates in practice).
func requiredBytes(m *manifest.Manifest) uint64 {
	var total uint64
	for _, e := range m.Entries {
		if e.EntryKind == manifest.EntryKindSQLitePages {
			total += uint64(len(e.Pages)) * 4096
		}
	}
	return total
}
