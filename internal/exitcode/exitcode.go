// Package exitcode declares the typed sentinel exit codes the doctor returns
// to the operator's shell. Allocation per cli-surface.md § Exit code allocation.
package exitcode

// Code is a process exit code. The zero value is Success.
type Code int

const (
	Success      Code = 0
	GenericError Code = 1
	RunningNode  Code = 2
	// AheadOfCanonical is reserved. The predicate it named was removed
	// (pocketnet-node-doctor-qd1): operator intent IS the consent for
	// recovery; refusing on local-ahead-of-canonical blocked the most
	// common operator scenario. The code slot is preserved to keep the
	// cli-surface code-space allocation stable.
	AheadOfCanonical                  Code = 3
	VersionMismatch                   Code = 4
	Capacity                          Code = 5
	PermissionReadOnly                Code = 6
	ManifestFormatVersionUnrecognized Code = 7

	// Apply-time exit codes (10–15 reserved per cli-surface.md).
	RollbackCompleted      Code = 10
	RollbackFailed         Code = 11
	NetworkBudgetExhausted Code = 12
	Reserved13             Code = 13
	SupersededCanonical    Code = 14
	PlanTampered           Code = 15
)
