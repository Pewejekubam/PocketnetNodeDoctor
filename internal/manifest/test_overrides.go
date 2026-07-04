package manifest

// Test override accessors for the spool seams (D3, DA3). They let sibling
// integration tests inject deterministic stubs (ENOSPC writer, constrained
// free-disk) without privileged filesystem fixtures, following the
// internal/preflight/test_overrides.go Set…ForTest convention. Each returns a
// restore func that reinstalls the production seam.

// SpoolFileForTest mirrors the unexported spoolFile interface so tests can
// supply a stub writer. Its method set is identical to spoolFile, so any value
// implementing it satisfies the production seam.
type SpoolFileForTest interface {
	Write(p []byte) (int, error)
	Name() string
	Close() error
	Remove() error
}

// SetSpoolWriterForTest overrides the spool-writer constructor. The returned
// func restores the production openSpoolWriter.
func SetSpoolWriterForTest(fn func(dir, pattern string) (SpoolFileForTest, error)) (restore func()) {
	saved := openSpoolWriter
	openSpoolWriter = func(dir, pattern string) (spoolFile, error) {
		sf, err := fn(dir, pattern)
		if err != nil {
			return nil, err
		}
		if sf == nil {
			return nil, nil
		}
		return sf, nil
	}
	return func() { openSpoolWriter = saved }
}

// SetFreeBytesForTest overrides the free-disk query. The returned func restores
// the production freeBytes.
func SetFreeBytesForTest(fn func(path string) (uint64, error)) (restore func()) {
	saved := freeBytesFn
	freeBytesFn = fn
	return func() { freeBytesFn = saved }
}
