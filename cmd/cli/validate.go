package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// runValidate is the testable entry point. main() wraps it for production
// use, but tests invoke runValidate directly to capture stdout/stderr.
func runValidate(stdout, stderr io.Writer, args []string) int {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	ctx := context.Background()

	proj, loaderErr := loader.New().Load(ctx, root)
	if loaderErr != nil {
		// Lift the loader error into a Phase-1 report.
		report, _ := validator.New().ValidateLoaderError(ctx, loaderErr)
		printReport(stdout, stderr, report)
		return 1
	}

	report, err := validator.New().Validate(ctx, proj)
	if err != nil {
		fmt.Fprintln(stderr, "validator failed:", err)
		return 2
	}
	printReport(stdout, stderr, report)
	if !report.OK() {
		return 1
	}
	return 0
}

func printReport(stdout, stderr io.Writer, report *ir.ValidationReport) {
	if report == nil {
		return
	}
	if report.OK() && len(report.Issues) == 0 {
		fmt.Fprintln(stdout, "OK")
		return
	}
	for _, issue := range report.Issues {
		target := stdout
		if issue.Severity == ir.SeverityError {
			target = stderr
		}
		fmt.Fprintln(target, issue.String())
		if issue.Hint != "" {
			fmt.Fprintln(target, "  hint:", issue.Hint)
		}
	}
	if !report.OK() {
		fmt.Fprintln(stderr, "validation failed")
	} else {
		fmt.Fprintln(stdout, "OK with warnings")
	}
}

// newValidateCommand wires the subcommand into cobra.
func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a Nimbusfab project directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runValidate(cmd.OutOrStdout(), cmd.ErrOrStderr(), args)
			if code != 0 {
				return errors.New("")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
