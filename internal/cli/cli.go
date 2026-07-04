// Package cli is the command-line surface for pocketnet-node-doctor.
// Subcommands: diagnose, apply (apply is reserved in chunk 002 and emits
// "not implemented in chunk 002" with exit code 1; chunk 003 owns its body).
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/buildinfo"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/trustroot"
)

// Options is the parsed CLI surface for `diagnose` and `apply`.
type Options struct {
	Subcommand   string
	CanonicalURL string
	PocketDBPath string
	PlanOut      string
	PinnedHash   string
	Verbose      bool

	// apply subcommand fields
	PlanPath string
	Parallel int
}

// ParseError is returned for invalid invocations. Carries the help text
// the doctor printed so callers can map to exit codes uniformly.
type ParseError struct {
	Code exitcode.Code
	Msg  string
}

func (e *ParseError) Error() string { return e.Msg }

// Parse parses argv (excluding argv[0]). Returns the typed options, or a
// ParseError for invalid invocations. --help and --version are handled
// inline (printed; returned with Subcommand == "help" / "version" and a
// nil error so callers can short-circuit cleanly).
func Parse(args []string, stdout, stderr io.Writer) (Options, error) {
	if len(args) == 0 {
		printTopHelp(stderr)
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: "no subcommand provided"}
	}
	switch args[0] {
	case "--help", "-h":
		printTopHelp(stdout)
		return Options{Subcommand: "help"}, nil
	case "--version":
		printVersion(stdout)
		return Options{Subcommand: "version"}, nil
	case "diagnose":
		return parseDiagnose(args[1:], stdout, stderr)
	case "apply":
		return parseApply(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "pocketnet-node-doctor: unknown subcommand %q\n", args[0])
		printTopHelp(stderr)
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: fmt.Sprintf("unknown subcommand %q", args[0])}
	}
}

func parseDiagnose(args []string, stdout, stderr io.Writer) (Options, error) {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	canonical := fs.String("canonical", "", "URL of the canonical manifest")
	pocketdb := fs.String("pocketdb", "", "path to the local pocketnet data directory")
	planOut := fs.String("plan-out", "", "path to write the plan.json (defaults to <dirname pocketdb>/plan.json)")
	pinnedHash := fs.String("pinned-hash", trustroot.PinnedHash, "expected SHA-256 of the manifest (overrides compiled-in trust-root)")
	verbose := fs.Bool("verbose", false, "emit debug messages on stderr")
	help := fs.Bool("help", false, "show diagnose help")

	// flag.NewFlagSet defines short forms only when explicitly declared. We
	// declare none; --canonical etc. are long-form only. Unknown flags
	// (including short forms like -c) are rejected by ContinueOnError.
	if err := fs.Parse(args); err != nil {
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: err.Error()}
	}
	if *help {
		printDiagnoseHelp(stdout)
		return Options{Subcommand: "help"}, nil
	}
	if *canonical == "" {
		fmt.Fprintln(stderr, "diagnose: --canonical is required")
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: "--canonical required"}
	}
	if *pocketdb == "" {
		fmt.Fprintln(stderr, "diagnose: --pocketdb is required")
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: "--pocketdb required"}
	}
	return Options{
		Subcommand:   "diagnose",
		CanonicalURL: *canonical,
		PocketDBPath: *pocketdb,
		PlanOut:      *planOut,
		PinnedHash:   *pinnedHash,
		Verbose:      *verbose,
	}, nil
}

func printTopHelp(w io.Writer) {
	fmt.Fprintln(w, "pocketnet-node-doctor — recover dead/corrupted pocketnet nodes by downloading only byte-level differences from a canonical snapshot.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  pocketnet-node-doctor diagnose --canonical <url> --pocketdb <path> [--plan-out <path>] [--verbose]")
	fmt.Fprintln(w, "  pocketnet-node-doctor apply --plan <path>            (chunk 003)")
	fmt.Fprintln(w, "  pocketnet-node-doctor --help")
	fmt.Fprintln(w, "  pocketnet-node-doctor --version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes:")
	fmt.Fprintln(w, "  0  success")
	fmt.Fprintln(w, "  1  generic error")
	fmt.Fprintln(w, "  2  refused: a pocketnet node is running")
	fmt.Fprintln(w, "  3  refused: local node is ahead of the canonical")
	fmt.Fprintln(w, "  4  refused: pocketnet-core version mismatch")
	fmt.Fprintln(w, "  5  refused: insufficient volume capacity")
	fmt.Fprintln(w, "  6  refused: pocketdb is read-only")
	fmt.Fprintln(w, "  7  refused: manifest format_version unrecognized")
}

func printDiagnoseHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: pocketnet-node-doctor diagnose --canonical <url> --pocketdb <path> [--plan-out <path>] [--verbose]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Compares the local pocketdb to the canonical manifest. Emits plan.json describing")
	fmt.Fprintln(w, "the divergent pages/files. Read-only — does not modify pocketdb. To apply the plan,")
	fmt.Fprintln(w, "use `pocketnet-node-doctor apply` (chunk 003).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --canonical <url>    HTTPS URL of the canonical manifest (required)")
	fmt.Fprintln(w, "  --pocketdb <path>    Path to the local pocketnet data directory (required)")
	fmt.Fprintln(w, "  --plan-out <path>    Where to write plan.json (default: <dirname pocketdb>/plan.json)")
	fmt.Fprintln(w, "  --pinned-hash <hex>  Expected SHA-256 of the manifest; overrides compiled-in trust-root")
	fmt.Fprintln(w, "  --verbose            Emit debug messages on stderr")
}

// printVersion writes to stdout (the single intentional stdout writer for
// the doctor; not part of the diagnose pathway).
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "pocketnet-node-doctor %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
	fmt.Fprintf(w, "trust-root: %s\n", trustroot.PinnedHash)
}

// EnsureSubcommand keeps go vet happy if Parse is the only caller of
// flag.NewFlagSet; nothing functional.
var _ = os.Args
