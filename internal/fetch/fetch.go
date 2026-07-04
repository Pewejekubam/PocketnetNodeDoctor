// Package fetch fetches and decompresses chunks from the canonical chunk store.
package fetch

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// Fetch downloads a chunk from chunkURL and writes decompressed bytes to dstPath.
// The URL extension signals the encoding:
//   - ".zst" suffix → zstd decompression (klauspost/compress/zstd decoder)
//   - ".gz"  suffix → gzip decompression (stdlib compress/gzip decoder)
//   - other         → raw copy (no decompression)
//
// No Accept-Encoding request header is sent; encoding is determined by URL extension.
// Returns error on non-200 status or IO failure.
func Fetch(ctx context.Context, chunkURL, dstPath string, transport http.RoundTripper, policy httptransfer.TransferPolicy) error {
	rt := httptransfer.NewPhasedTransport(transport, policy)
	client := &http.Client{Transport: rt}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chunkURL, nil)
	if err != nil {
		return fmt.Errorf("fetch: build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch: unexpected status %d for %s", resp.StatusCode, chunkURL)
	}

	guardedBody, cancelGuard := httptransfer.WrapBodyWithStallGuard(ctx, resp.Body, policy)
	defer cancelGuard()

	var r io.Reader
	switch {
	case strings.HasSuffix(chunkURL, ".zst"):
		zr, err := zstd.NewReader(guardedBody)
		if err != nil {
			return fmt.Errorf("fetch: zstd reader: %w", err)
		}
		defer zr.Close()
		r = zr
	case strings.HasSuffix(chunkURL, ".gz"):
		gr, err := gzip.NewReader(guardedBody)
		if err != nil {
			return fmt.Errorf("fetch: gzip reader: %w", err)
		}
		defer gr.Close()
		r = gr
	default:
		r = guardedBody
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("fetch: create dest file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("fetch: copy: %w", err)
	}
	return nil
}
