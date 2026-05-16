package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
)

func newDriftCommand() *cobra.Command {
	var stack string
	cmd := &cobra.Command{
		Use:   "drift [path]",
		Short: "Detect drift between Tofu state and current cloud state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."
			if len(args) == 1 {
				projectPath = args[0]
			}
			reg := cloud.NewRegistry()
			if err := reg.Register(aws.New()); err != nil {
				return err
			}
			code := runDrift(cmd.Context(), driftArgs{
				ProjectPath: projectPath, Stack: stack,
				Adapters: reg, Runner: tofu.NewExecRunner(),
				Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
		SilenceUsage: true, SilenceErrors: true,
	}
	cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
	_ = cmd.MarkFlagRequired("stack")
	return cmd
}

type driftArgs struct {
	ProjectPath, Stack string
	Adapters           cloud.Registry
	Runner             tofu.Runner
	WorkRoot           string
	Stdout, Stderr     io.Writer
}

func runDrift(ctx context.Context, in driftArgs) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if in.Stack == "" {
		fmt.Fprintln(in.Stderr, "error: --stack is required")
		return 2
	}
	project, err := loader.New().Load(ctx, in.ProjectPath)
	if err != nil {
		fmt.Fprintf(in.Stderr, "load: %v\n", err)
		return 1
	}
	rep, err := validator.New().Validate(ctx, project)
	if err != nil {
		fmt.Fprintf(in.Stderr, "validator: %v\n", err)
		return 2
	}
	if rep != nil && !rep.OK() {
		for _, i := range rep.Issues {
			fmt.Fprintln(in.Stderr, i.String())
		}
		return 1
	}
	eng, err := engine.New(ctx, engine.Config{CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot})
	if err != nil {
		fmt.Fprintf(in.Stderr, "engine: %v\n", err)
		return 1
	}
	plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
	if err != nil {
		fmt.Fprintf(in.Stderr, "plan: %v\n", err)
		return 1
	}
	drift, err := eng.DetectDriftWithPlan(ctx, plan)
	if err != nil {
		fmt.Fprintf(in.Stderr, "drift: %v\n", err)
		return 1
	}
	anyDrift := false
	for _, tr := range drift.TargetReports {
		marker := "[="
		if tr.HasDrift {
			marker = "[!="
			anyDrift = true
		}
		fmt.Fprintf(in.Stdout, "  %s] %s  %s/%s  drift=%v\n", marker, tr.Component, tr.Cloud, tr.Region, tr.HasDrift)
		for _, d := range tr.Drifted {
			fmt.Fprintf(in.Stdout, "      ~ %s  %s\n", d.Address, d.DiffSummary)
		}
		for _, d := range tr.Gone {
			fmt.Fprintf(in.Stdout, "      - %s (gone)\n", d.Address)
		}
		for _, d := range tr.Discovered {
			fmt.Fprintf(in.Stdout, "      + %s (discovered)\n", d.Address)
		}
	}
	if anyDrift {
		fmt.Fprintln(in.Stdout, "\nDrift detected.")
	} else {
		fmt.Fprintln(in.Stdout, "\nNo drift.")
	}
	return 0
}
