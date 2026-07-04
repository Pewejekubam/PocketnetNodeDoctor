// T059: apply subcommand flag contract — --plan required, --parallel range validated.
package contract

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/cli"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

func TestApplyFlags_PlanMissing_Exit1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := cli.Parse([]string{"apply"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected ParseError when --plan is missing")
	}
	pe, ok := err.(*cli.ParseError)
	if !ok {
		t.Fatalf("expected *cli.ParseError, got %T: %v", err, err)
	}
	if pe.Code != exitcode.GenericError {
		t.Errorf("code = %d, want %d (GenericError)", pe.Code, exitcode.GenericError)
	}
}

func TestApplyFlags_ParallelZero_Exit1NamingRange(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := cli.Parse([]string{"apply", "--plan", "x.json", "--parallel", "0"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected ParseError for --parallel 0")
	}
	pe, ok := err.(*cli.ParseError)
	if !ok {
		t.Fatalf("expected *cli.ParseError, got %T", err)
	}
	if pe.Code != exitcode.GenericError {
		t.Errorf("code = %d, want %d", pe.Code, exitcode.GenericError)
	}
	if !strings.Contains(pe.Msg, "1") || !strings.Contains(pe.Msg, "32") {
		t.Errorf("error message %q does not mention valid range 1..32", pe.Msg)
	}
}

func TestApplyFlags_ParallelThirtyThree_Exit1NamingRange(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := cli.Parse([]string{"apply", "--plan", "x.json", "--parallel", "33"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected ParseError for --parallel 33")
	}
	pe, ok := err.(*cli.ParseError)
	if !ok {
		t.Fatalf("expected *cli.ParseError, got %T", err)
	}
	if !strings.Contains(pe.Msg, "1") || !strings.Contains(pe.Msg, "32") {
		t.Errorf("error message %q does not mention valid range 1..32", pe.Msg)
	}
}

func TestApplyFlags_DefaultParallel_Accepted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, err := cli.Parse([]string{"apply", "--plan", "x.json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Subcommand != "apply" {
		t.Errorf("Subcommand = %q, want apply", opts.Subcommand)
	}
	if opts.PlanPath != "x.json" {
		t.Errorf("PlanPath = %q, want x.json", opts.PlanPath)
	}
	if opts.Parallel != 4 {
		t.Errorf("Parallel = %d, want 4 (default)", opts.Parallel)
	}
}

func TestApplyFlags_VerboseAccepted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, err := cli.Parse([]string{"apply", "--plan", "x.json", "--verbose"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !opts.Verbose {
		t.Errorf("Verbose = false, want true")
	}
}
