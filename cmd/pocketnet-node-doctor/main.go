package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/cli"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/diagnose"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) exitcode.Code {
	opts, err := cli.Parse(args, os.Stdout, os.Stderr)
	if err != nil {
		var pe *cli.ParseError
		if asParseErr(err, &pe) {
			return pe.Code
		}
		return exitcode.GenericError
	}
	switch opts.Subcommand {
	case "help", "version":
		return exitcode.Success
	case "diagnose":
		planOut, err := cli.ResolvePlanOut(opts.PocketDBPath, opts.PlanOut)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitcode.GenericError
		}
		code, _ := diagnose.Diagnose(context.Background(), diagnose.Options{
			CanonicalURL: opts.CanonicalURL,
			PocketDBPath: opts.PocketDBPath,
			PlanOutPath:  planOut,
			PinnedHash:   opts.PinnedHash,
			Logger:       stderrlog.New(opts.Verbose),
		})
		return code
	case "apply":
		code, err := apply.Run(context.Background(), apply.Options{
			PlanPath: opts.PlanPath,
			Parallel: opts.Parallel,
			Logger:   stderrlog.New(opts.Verbose),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return code
	}
	return exitcode.GenericError
}

func asParseErr(err error, target **cli.ParseError) bool {
	type wrapper interface{ Unwrap() error }
	for err != nil {
		if pe, ok := err.(*cli.ParseError); ok {
			*target = pe
			return true
		}
		if w, ok := err.(wrapper); ok {
			err = w.Unwrap()
			continue
		}
		break
	}
	return false
}
