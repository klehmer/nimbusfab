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
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func newCostCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Cost commands (estimate; actuals deferred to cost-collector phase)",
	}
	cmd.AddCommand(newCostEstimateCommand())
	return cmd
}

type costEstimateArgs struct {
	ProjectPath    string
	Stack          string
	Adapters       cloud.Registry
	Runner         tofu.Runner
	Inventory      inventory.Repo
	WorkRoot       string
	Stdout, Stderr io.Writer
}

func newCostEstimateCommand() *cobra.Command {
	var stack string
	cmd := &cobra.Command{
		Use:   "estimate [path]",
		Short: "Pre-deploy cost estimate from bundled price snapshot",
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
			repo, err := openInventory(cmd.Context(), flagInventoryDSN, flagNoInventory)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "inventory: %v\n", err)
				os.Exit(1)
			}
			defer repo.Close()
			code := runCostEstimate(cmd.Context(), costEstimateArgs{
				ProjectPath: projectPath, Stack: stack,
				Adapters: reg, Runner: tofu.NewExecRunner(), Inventory: repo,
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

func runCostEstimate(ctx context.Context, in costEstimateArgs) int {
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
	if rep, vErr := validator.New().Validate(ctx, project); vErr != nil {
		fmt.Fprintf(in.Stderr, "validator: %v\n", vErr)
		return 2
	} else if rep != nil && !rep.OK() {
		for _, i := range rep.Issues {
			fmt.Fprintln(in.Stderr, i.String())
		}
		return 1
	}
	eng, err := engine.New(ctx, engine.Config{
		CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot, InventoryRepo: in.Inventory,
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
	est, err := eng.EstimateCost(ctx, plan)
	if err != nil {
		fmt.Fprintf(in.Stderr, "estimate: %v\n", err)
		return 1
	}
	fmt.Fprintf(in.Stdout, "Total: $%.2f/%s (%s)\n\n", est.Total, est.Period, est.Currency)
	for _, t := range est.Targets {
		if t.Subtotal == 0 && len(t.Primitives) == 0 {
			continue
		}
		fmt.Fprintf(in.Stdout, "%s/%s  $%.2f/%s\n", t.Cloud, t.Region, t.Subtotal, est.Period)
		for _, p := range t.Primitives {
			fmt.Fprintf(in.Stdout, "  - %s  $%.4f x %.0f %s = $%.2f\n",
				p.PrimitiveID, p.UnitPrice, p.Units, p.UnitOfMeasure, p.Subtotal)
		}
	}
	if len(est.Warnings) > 0 {
		fmt.Fprintln(in.Stdout, "\nWarnings:")
		for _, w := range est.Warnings {
			fmt.Fprintf(in.Stdout, "  - %s\n", w)
		}
	}
	return 0
}
