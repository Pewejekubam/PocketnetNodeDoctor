// Package trustroot holds the compiled-in canonical-manifest trust-root hash.
// Override at build via `go build -ldflags "-X github.com/pocketnet-team/pocketnet-node-doctor/internal/trustroot.PinnedHash=<hex>"`
// (D11). The default is the v1 development trust-root per pre-spec
// Implementation Context.
package trustroot

var PinnedHash = "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249"
