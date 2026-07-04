package cli

import (
	"bytes"
	"strings"
	"testing"
)

// T057: flag.NewFlagSet per subcommand; long-form-only flags --canonical,
// --pocketdb, --plan-out, --verbose; global --help, --version; unknown
// short forms rejected.
func TestParse_DiagnoseHappyPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"diagnose", "--canonical", "https://example.invalid/manifest.json", "--pocketdb", "/var/lib/pocketnet/pocketdb"}
	opts, err := Parse(args, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Subcommand != "diagnose" {
		t.Errorf("Subcommand got %q want diagnose", opts.Subcommand)
	}
	if opts.CanonicalURL != "https://example.invalid/manifest.json" {
		t.Errorf("CanonicalURL got %q", opts.CanonicalURL)
	}
	if opts.PocketDBPath != "/var/lib/pocketnet/pocketdb" {
		t.Errorf("PocketDBPath got %q", opts.PocketDBPath)
	}
}

func TestParse_DiagnoseMissingRequired(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if _, err := Parse([]string{"diagnose"}, &stdout, &stderr); err == nil {
		t.Errorf("want ParseError on missing --canonical/--pocketdb")
	}
}

func TestParse_UnknownShortForm_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := Parse([]string{"diagnose", "-c", "x"}, &stdout, &stderr)
	if err == nil {
		t.Errorf("want ParseError on unknown short flag")
	}
}

func TestParse_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, err := Parse([]string{"weird"}, &stdout, &stderr)
	if err == nil {
		t.Errorf("want ParseError on unknown subcommand")
	}
}

func TestParse_TopHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, err := Parse([]string{"--help"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Subcommand != "help" {
		t.Errorf("Subcommand got %q want help", opts.Subcommand)
	}
	if !strings.Contains(stdout.String(), "pocketnet-node-doctor") {
		t.Errorf("--help output missing brand")
	}
	if !strings.Contains(stdout.String(), "Exit codes:") {
		t.Errorf("--help missing exit codes")
	}
}

func TestParse_Version_WritesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, err := Parse([]string{"--version"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Subcommand != "version" {
		t.Errorf("Subcommand got %q want version", opts.Subcommand)
	}
	if !strings.Contains(stdout.String(), "trust-root:") {
		t.Errorf("--version missing trust-root: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("--version must not write stderr; got %q", stderr.String())
	}
}

func TestParse_Verbose(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, _ := Parse([]string{"diagnose", "--canonical", "u", "--pocketdb", "p", "--verbose"}, &stdout, &stderr)
	if !opts.Verbose {
		t.Errorf("Verbose flag not parsed")
	}
}
