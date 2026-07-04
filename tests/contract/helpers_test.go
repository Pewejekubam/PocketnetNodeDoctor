package contract

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// copyDir copies src directory tree to dst (dst is created if absent).
func copyDir(t testing.TB, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("copyDir: mkdir %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("copyDir: readdir %s: %v", src, err)
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(t, srcPath, dstPath)
		} else {
			copyFile(t, srcPath, dstPath)
		}
	}
}

func copyFile(t testing.TB, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("copyFile: open %s: %v", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("copyFile: mkdir %s: %v", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("copyFile: create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copyFile: copy %s→%s: %v", src, dst, err)
	}
}
