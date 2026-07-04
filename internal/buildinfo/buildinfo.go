// Package buildinfo holds build-time-injected identity strings. Populated
// at build via `go build -ldflags "-X github.com/pocketnet-team/pocketnet-node-doctor/internal/buildinfo.<field>=<value>"`
// (D11).
package buildinfo

var (
	Version   = "(unknown)"
	Commit    = "(unknown)"
	BuildDate = "(unknown)"
)
