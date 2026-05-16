package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func newDestroyCommand() *cobra.Command {
	var stack string
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "destroy [deployment-id | path]",
		Short: "Destroy by deployment ID (preferred), or validate-plan-destroy against a stack",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
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
			code := runDestroy(cmd.Context(), destroyArgs{
				PositionalArg: arg, Stack: stack, AutoApprove: autoApprove,
				Adapters: reg, Runner: tofu.NewExecRunner(),
				Inventory: repo,
				Stdout:    cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
		SilenceUsage: true, SilenceErrors: true,
	}
	cmd.Flags().StringVar(&stack, "stack", "", "stack to plan + destroy (only when no deployment ID given)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
	return cmd
}

type destroyArgs struct {
	PositionalArg, Stack string
	AutoApprove          bool
	Adapters             cloud.Registry
	Runner               tofu.Runner
	Inventory            inventory.Repo
	WorkRoot             string
	Stdout, Stderr       io.Writer
}

func runDestroy(ctx context.Context, in destroyArgs) int {
	if ctx == nil {
		ctx = context.Background()
	}
	isDeploymentID := strings.HasPrefix(in.PositionalArg, "dep-") && !strings.Contains(in.PositionalArg, "/")

	eng, err := engine.New(ctx, engine.Config{
		CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot, InventoryRepo: in.Inventory,
	})
	if err != nil {
		fmt.Fprintf(in.Stderr, "engine: %v\n", err)
		return 1
	}

	if isDeploymentID {
		_, err := eng.Destroy(ctx, in.PositionalArg, engine.DestroyOpts{AutoApprove: true})
		if err != nil {
			fmt.Fprintf(in.Stderr, "destroy: %v\n", err)
			return 1
		}
		fmt.Fprintf(in.Stdout, "Destroyed deployment %s\n", in.PositionalArg)
		return 0
	}

	if in.Stack == "" {
		fmt.Fprintln(in.Stderr, "error: --stack required when no deployment ID given")
		return 2
	}
	projectPath := in.PositionalArg
	if projectPath == "" {
		projectPath = "."
	}
	project, err := loader.New().Load(ctx, projectPath)
	if err != nil {
		fmt.Fprintf(in.Stderr, "load: %v\n", err)
		return 1
	}
	rep, err := validator.New(components.DefaultRegistry()).Validate(ctx, project)
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
