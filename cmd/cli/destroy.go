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

func newDestroyCommand() *cobra.Command {
	var stack string
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "destroy [path]",
		Short: "Tear down a stack via tofu destroy",
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
			code := runDestroy(cmd.Context(), destroyArgs{
				ProjectPath: projectPath, Stack: stack, AutoApprove: autoApprove,
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
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
	_ = cmd.MarkFlagRequired("stack")
	return cmd
}

type destroyArgs struct {
	ProjectPath, Stack string
	AutoApprove        bool
	Adapters           cloud.Registry
	Runner             tofu.Runner
	WorkRoot           string
	Stdout, Stderr     io.Writer
}

func runDestroy(ctx context.Context, in destroyArgs) int {
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
	res, err := eng.DestroyWithPlan(ctx, plan, engine.DestroyOpts{AutoApprove: true})
	if err != nil {
		fmt.Fprintf(in.Stderr, "destroy: %v\n", err)
		return 1
	}
	fmt.Fprintf(in.Stdout, "Destroy %s\n", res.Status)
	for _, r := range res.TargetResults {
		marker := "[ok]"
		if r.Status == provisioner.RunStatusFailed {
			marker = "[fail]"
		}
		fmt.Fprintf(in.Stdout, "  %s %s  %s/%s\n", marker, r.Component, r.Cloud, r.Region)
	}
	if res.Status != provisioner.ApplySucceeded {
		return 1
	}
	return 0
}
