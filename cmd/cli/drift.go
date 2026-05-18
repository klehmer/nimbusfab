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

func newDriftCommand() *cobra.Command {
	var stack string
	cmd := &cobra.Command{
		Use:   "drift [deployment-id | path]",
		Short: "Detect drift by deployment ID (preferred), or plan-then-drift against a stack",
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
			code := runDrift(cmd.Context(), driftArgs{
				PositionalArg: arg, Stack: stack,
				Adapters: reg, Runner: defaultRunner(),
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
	cmd.Flags().StringVar(&stack, "stack", "", "stack to plan + drift (only when no deployment ID given)")
	return cmd
}

type driftArgs struct {
	PositionalArg, Stack string
	Adapters             cloud.Registry
	Runner               tofu.Runner
	Inventory            inventory.Repo
	WorkRoot             string
	Stdout, Stderr       io.Writer
}

func runDrift(ctx context.Context, in driftArgs) int {
	if ctx == nil {
		ctx = context.Background()
	}
	isDeploymentID := strings.HasPrefix(in.PositionalArg, "dep-") && !strings.Contains(in.PositionalArg, "/")

	eng, err := engine.New(ctx, engine.Config{
		CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot, InventoryRepo: in.Inventory,
		SecretsBackend: defaultSecretsBackend(),
	})
	if err != nil {
		fmt.Fprintf(in.Stderr, "engine: %v\n", err)
		return 1
	}

	var drift *provisioner.DriftReport
	if isDeploymentID {
		drift, err = eng.DetectDrift(ctx, in.PositionalArg, engine.DriftOpts{})
	} else {
		if in.Stack == "" {
			fmt.Fprintln(in.Stderr, "error: --stack required when no deployment ID given")
			return 2
		}
		projectPath := in.PositionalArg
		if projectPath == "" {
			projectPath = "."
		}
		project, lerr := loader.New().Load(ctx, projectPath)
		if lerr != nil {
			fmt.Fprintf(in.Stderr, "load: %v\n", lerr)
			return 1
		}
		rep, verr := validator.New(components.DefaultRegistry()).Validate(ctx, project)
		if verr != nil {
			fmt.Fprintf(in.Stderr, "validator: %v\n", verr)
			return 2
		}
		if rep != nil && !rep.OK() {
			for _, i := range rep.Issues {
				fmt.Fprintln(in.Stderr, i.String())
			}
			return 1
		}
		plan, perr := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
		if perr != nil {
			fmt.Fprintf(in.Stderr, "plan: %v\n", perr)
			return 1
		}
		drift, err = eng.DetectDriftWithPlan(ctx, plan)
	}
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
