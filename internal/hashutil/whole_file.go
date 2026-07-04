// Package hashutil provides streaming SHA-256 hashing for whole files and
// for SQLite-shaped page files. All hashes are returned as lowercase 64-hex
// strings. Buffer size is fixed at 1 MiB (D14).
package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const bufSize = 1 << 20 // 1 MiB

func HashWholeFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, bufSize)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
