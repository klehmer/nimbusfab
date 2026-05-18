package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/graph"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

type graphArgs struct {
	ProjectPath string
	Stack       string
	Direction   string
	OutPath     string
	Stdout      io.Writer
	Stderr      io.Writer
}

func newGraphCommand() *cobra.Command {
	var stack, direction, outPath string
	cmd := &cobra.Command{
		Use:   "graph [path]",
		Short: "Render a SVG dependency graph for a project; no inventory / cloud creds required",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."
			if len(args) == 1 {
				projectPath = args[0]
			}
			code := runGraph(graphArgs{
				ProjectPath: projectPath,
				Stack:       stack,
				Direction:   direction,
				OutPath:     outPath,
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&stack, "stack", "dev", "stack name")
	cmd.Flags().StringVar(&direction, "dir", "tb", "layout direction: tb (top-down) or lr (left-right)")
	cmd.Flags().StringVar(&outPath, "out", "", "write SVG to file; default stdout")
	return cmd
}

// runGraph is the testable entry point. Returns exit code per spec:
// 0 success / 1 IO / 2 validator failure / 3 pairing failure.
func runGraph(args graphArgs) int {
	ctx := context.Background()
	stdout := args.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := args.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	project, err := loader.New().Load(ctx, args.ProjectPath)
	if err != nil {
		fmt.Fprintf(stderr, "load: %v\n", err)
		return 1
	}

	// Run the validator so structural ref errors surface at this entry point too.
	report, err := validator.New(components.DefaultRegistry()).Validate(ctx, project)
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 2
	}
	if report != nil && !report.OK() {
		for _, i := range report.Issues {
			fmt.Fprintf(stderr, "validator: %s\n", i.String())
		}
		return 2
	}

	pairErrors := upstream.PreflightPairing(project.Components)
	for _, pe := range pairErrors {
		fmt.Fprintf(stderr, "pairing: %s in %s/%s references %s.%s but no upstream target matches\n",
			pe.Component, pe.Cloud, pe.Region, pe.Ref.Component, pe.Ref.Output)
	}

	gComps, gPairs := graph.FromIR(project.Components, pairErrors)

	out, err := graph.Layout(graph.Input{
		Components:    gComps,
		PairingErrors: gPairs,
		Direction:     args.Direction,
	})
	if err != nil {
		fmt.Fprintf(stderr, "layout: %v\n", err)
		return 1
	}
	svg := graph.RenderSVG(out)

	if args.OutPath != "" {
		if err := os.WriteFile(args.OutPath, svg, 0o644); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", args.OutPath, err)
			return 1
		}
	} else {
		_, _ = stdout.Write(svg)
		_, _ = stdout.Write([]byte("\n"))
	}

	if len(pairErrors) > 0 {
		return 3
	}
	return 0
}
