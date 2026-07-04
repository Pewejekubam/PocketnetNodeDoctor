// Package diagnose implements the read-only diagnose pathway: pre-flight
// predicates, manifest fetch+verify, hash phase, plan emission, summary.
package diagnose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ProbeWritable verifies the doctor can write to the plan-out destination
// before any expensive hash work begins (D6, cli-surface.md § Plan-out
// writability probe).
//
// Steps:
//  1. Determine the target directory (Dir(planOutPath))
//  2. Create <dir>/.pocketnet-node-doctor-writeprobe-<rand>
//  3. Write 1 byte
//  4. fsync (best-effort)
//  5. Unlink
//
// Failure (permission denied / ENOSPC / read-only / missing parent dir)
// returns an error naming the unwritable target. Callers map this to a
// generic-error sentinel + diagnostic.
func ProbeWritable(planOutPath string) error {
	dir := filepath.Dir(planOutPath)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("plan-out directory %q: %w", dir, err)
	}
	suffix, err := randSuffix()
	if err != nil {
		return fmt.Errorf("plan-out probe: %w", err)
	}
	probe := filepath.Join(dir, ".pocketnet-node-doctor-writeprobe-"+suffix)
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("plan-out probe: create %q: %w", probe, err)
	}
	defer os.Remove(probe)
	if _, err := f.Write([]byte{0}); err != nil {
		f.Close()
		return fmt.Errorf("plan-out probe: write %q: %w", probe, err)
	}
	_ = f.Sync()
	return f.Close()
}

func randSuffix() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
