package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// parseApply parses the apply subcommand flags.
// --plan is required.
// --parallel defaults to 4, valid range 1..32.
// --verbose is optional.
func parseApply(args []string, stdout, stderr io.Writer) (Options, error) {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)

	planPath := fs.String("plan", "", "path to the plan.json produced by diagnose (required)")
	parallel := fs.Int("parallel", 4, "number of parallel chunk fetch workers (1..32)")
	verbose := fs.Bool("verbose", false, "emit debug messages on stderr")
	help := fs.Bool("help", false, "show apply help")

	if err := fs.Parse(args); err != nil {
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: err.Error()}
	}

	if *help {
		printApplyHelp(stdout)
		return Options{Subcommand: "help"}, nil
	}

	if *planPath == "" {
		fmt.Fprintln(stderr, "apply: --plan is required")
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: "--plan is required"}
	}

	if *parallel < 1 || *parallel > 32 {
		msg := fmt.Sprintf("apply: --parallel %d is out of range; valid range is 1..32", *parallel)
		fmt.Fprintln(stderr, msg)
		return Options{}, &ParseError{Code: exitcode.GenericError, Msg: msg}
	}

	return Options{
		Subcommand: "apply",
		PlanPath:   *planPath,
		Parallel:   *parallel,
		Verbose:    *verbose,
	}, nil
}

func printApplyHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: pocketnet-node-doctor apply --plan <path> [--parallel <n>] [--verbose]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Consumes the plan produced by `diagnose` and atomically applies all divergent")
	fmt.Fprintln(w, "chunks to the local pocketdb. Rolls back on verification failure.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --plan <path>      Path to plan.json produced by diagnose (required)")
	fmt.Fprintln(w, "  --parallel <n>     Number of parallel fetch workers, 1..32 (default 4)")
	fmt.Fprintln(w, "  --verbose          Emit debug messages on stderr")
}
