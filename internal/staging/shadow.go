package staging

import (
	"io"
	"os"
	"path/filepath"
)

// TakeShadow copies or hard-links the file at livePath to stagingDir/shadows/planRelPath.
// isAbsent variadic: if true, no shadow is taken (absent-file entries have nothing to copy).
// Tries os.Link first; falls back to io.Copy if it fails (cross-device, etc.).
// Uses filepath.FromSlash(planRelPath) for Windows compatibility.
func TakeShadow(livePath, stagingDir, planRelPath string, isAbsent ...bool) error {
	if len(isAbsent) > 0 && isAbsent[0] {
		return nil
	}

	dest := filepath.Join(stagingDir, "shadows", filepath.FromSlash(planRelPath))

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	// Try hard link first
	if err := os.Link(livePath, dest); err == nil {
		return nil
	}

	// Fall back to copy
	return copyFile(livePath, dest)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
