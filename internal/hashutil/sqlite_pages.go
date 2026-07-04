package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
)

// PageHash is one page's offset (bytes from start of file) and SHA-256 hash.
type PageHash struct {
	Offset int64
	Hash   string
}

// HashSQLitePages returns an iter.Seq2[PageHash, error] (Go 1.23+) that
// yields one (offset, hash) per page of size pageSize. The iterator emits
// pages in ascending offset order. If the file is not page-aligned, the
// last yield is an error wrapping io.ErrUnexpectedEOF.
func HashSQLitePages(path string, pageSize int) (iter.Seq2[PageHash, error], error) {
	if pageSize <= 0 {
		return nil, fmt.Errorf("pageSize must be > 0, got %d", pageSize)
	}
	// Open eagerly so a missing-file error surfaces synchronously to the
	// caller rather than on first iteration.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return func(yield func(PageHash, error) bool) {
		defer f.Close()
		page := make([]byte, pageSize)
		var offset int64
		for {
			n, err := io.ReadFull(f, page)
			if errors.Is(err, io.EOF) {
				return
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				yield(PageHash{}, fmt.Errorf("file not page-aligned at offset %d (read %d of %d): %w", offset, n, pageSize, err))
				return
			}
			if err != nil {
				yield(PageHash{}, err)
				return
			}
			sum := sha256.Sum256(page)
			ph := PageHash{Offset: offset, Hash: hex.EncodeToString(sum[:])}
			if !yield(ph, nil) {
				return
			}
			offset += int64(pageSize)
		}
	}, nil
}
