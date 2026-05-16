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
	return 0
}
