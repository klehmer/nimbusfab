package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type planArgs struct {
	ProjectPath string
	Stack       string
	Adapters    cloud.Registry
	Runner      tofu.Runner
	Inventory   inventory.Repo
	WorkRoot    string
	Stdout      io.Writer
	Stderr      io.Writer
}

func newPlanCommand() *cobra.Command {
	var stack string
	cmd := &cobra.Command{
		Use:   "plan [path]",
		Short: "Validate, then plan a project against a stack",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."
			if len(args) == 1 {
				projectPath = args[0]
			}
			reg, err := defaultCloudRegistry()
			if err != nil {
				return err
			}
			repo, err := openInventory(cmd.Context(), flagInventoryDSN, flagNoInventory)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "inventory: %v\n", err)
				os.Exit(1)
			}
			defer repo.Close()
			code := runPlan(cmd.Context(), planArgs{
				ProjectPath: projectPath,
				Stack:       stack,
				Adapters:    reg,
				Runner:      tofu.NewExecRunner(),
				Inventory:   repo,
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
	cmd.Flags().StringVar(&stack, "stack", "", "stack to plan against (required)")
	_ = cmd.MarkFlagRequired("stack")
	return cmd
}

func runPlan(ctx context.Context, in planArgs) int {
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
	report, err := validator.New(components.DefaultRegistry()).Validate(ctx, project)
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
		CloudAdapters: in.Adapters,
		TofuRunner:    in.Runner,
		WorkRoot:      in.WorkRoot,
		InventoryRepo: in.Inventory,
	})
	if err != nil {
		fmt.Fprintf(in.Stderr, "engine: %v\n", err)
		return 1
	}

	result, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
	if err != nil {
		fmt.Fprintf(in.Stderr, "plan: %v\n", err)
		return 1
	}

	fmt.Fprintf(in.Stdout, "Planning %d targets...\n", len(result.Targets))
	for _, tp := range result.Targets {
		fmt.Fprintf(in.Stdout, "  - %s  %s/%s  (+%d ~%d -%d)  workspace=%s\n",
			tp.Component, tp.Cloud, tp.Region, tp.Adds, tp.Changes, tp.Destroys, tp.WorkspaceDir)
	}
	if result.HasChanges {
		fmt.Fprintln(in.Stdout, "\nPlan has changes.")
	} else {
		fmt.Fprintln(in.Stdout, "\nPlan has no changes.")
	}
	if result.DeploymentID != "" && in.Inventory != nil && !inventory.IsNullRepo(in.Inventory) {
		fmt.Fprintf(in.Stdout, "Deployment ID: %s\n", result.DeploymentID)
		fmt.Fprintf(in.Stdout, "  (run `nimbusfab apply %s` to deploy)\n", result.DeploymentID)
	}
	if len(result.ParityReports) > 0 {
		fmt.Fprintln(in.Stdout)
		fmt.Fprintln(in.Stdout, "Parity:")
		for _, rep := range result.ParityReports {
			marker := "OK"
			if rep.Score < 0.7 {
				marker = "DIVERGENT"
			} else if rep.Score < 0.95 {
				marker = "MINOR"
			}
			fmt.Fprintf(in.Stdout, "  [%s] %s  score=%.2f\n", marker, rep.Component, rep.Score)
			for _, w := range rep.Warnings {
				fmt.Fprintf(in.Stdout, "      ! %s\n", w)
			}
		}
	}
	if est, err := eng.EstimateCost(ctx, result); err == nil && est != nil && est.Total > 0 {
		fmt.Fprintln(in.Stdout)
		fmt.Fprintln(in.Stdout, "Cost:")
		fmt.Fprintf(in.Stdout, "  Total estimated: $%.2f/%s\n", est.Total, est.Period)
		for _, t := range est.Targets {
			if t.Subtotal == 0 {
				continue
			}
			fmt.Fprintf(in.Stdout, "    %s/%s  $%.2f/%s\n", t.Cloud, t.Region, t.Subtotal, est.Period)
		}
		for _, w := range est.Warnings {
			fmt.Fprintf(in.Stdout, "  warning: %s\n", w)
		}
	}
	return 0
}
