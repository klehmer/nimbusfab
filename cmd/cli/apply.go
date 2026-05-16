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
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

type applyArgs struct {
	ProjectPath    string
	Stack          string
	AutoApprove    bool
	PartialFailure string
	Adapters       cloud.Registry
	Runner         tofu.Runner
	WorkRoot       string
	Stdout         io.Writer
	Stderr         io.Writer
}

func newApplyCommand() *cobra.Command {
	var stack, partialFailure string
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "apply [path]",
		Short: "Validate, plan, then apply against a stack",
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
			code := runApply(cmd.Context(), applyArgs{
				ProjectPath:    projectPath,
				Stack:          stack,
				AutoApprove:    autoApprove,
				PartialFailure: partialFailure,
				Adapters:       reg,
				Runner:         tofu.NewExecRunner(),
				Stdout:         cmd.OutOrStdout(),
				Stderr:         cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
	cmd.Flags().StringVar(&partialFailure, "partial-failure", "leave", "leave | rollback | retry-failed")
	_ = cmd.MarkFlagRequired("stack")
	return cmd
}

func runApply(ctx context.Context, in applyArgs) int {
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
	report, err := validator.New().Validate(ctx, project)
	if err != nil {
		fmt.Fprintf(in.Stderr, "validator: %v\n", err)
		return 2
	}
	if report != nil && !report.OK() {
		for _, issue := range report.Issues {
			fmt.Fprintln(in.Stderr, issue.String())
		}
		return 1
	}
	eng, err := engine.New(ctx, engine.Config{
		CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot,
	})
	if err != nil {
		fmt.Fprintf(in.Stderr, "engine: %v\n", err)
		return 1
	}
	plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
	if err != nil {
		fmt.Fprintf(in.Stderr, "plan: %v\n", err)
		return 1
	}
	fmt.Fprintf(in.Stdout, "Planning %d targets... done\n", len(plan.Targets))
	res, err := eng.ApplyWithPlan(ctx, plan, engine.ApplyOpts{
		AutoApprove:    true,
		PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
	})
	if err != nil {
		fmt.Fprintf(in.Stderr, "apply: %v\n", err)
		return 1
	}
	var succeeded, failed, reverted, skipped int
	for _, r := range res.TargetResults {
		switch r.Status {
		case provisioner.RunStatusSucceeded:
			succeeded++
		case provisioner.RunStatusFailed:
			failed++
		case provisioner.RunStatusReverted:
			reverted++
		case provisioner.RunStatusSkipped:
			skipped++
		}
		marker := "[ok]"
		switch r.Status {
		case provisioner.RunStatusFailed:
			marker = "[fail]"
		case provisioner.RunStatusReverted:
			marker = "[rollback]"
		case provisioner.RunStatusSkipped:
			marker = "[skip]"
		}
		fmt.Fprintf(in.Stdout, "  %s %s  %s/%s  status=%s\n", marker, r.Component, r.Cloud, r.Region, r.Status)
		if r.Error != nil {
			fmt.Fprintf(in.Stdout, "      %v\n", r.Error)
		}
	}
	fmt.Fprintf(in.Stdout, "\nApply %s: %d succeeded, %d failed, %d reverted, %d skipped\n",
		res.Status, succeeded, failed, reverted, skipped)
	if res.Status != provisioner.ApplySucceeded {
		return 1
	}
	return 0
}
