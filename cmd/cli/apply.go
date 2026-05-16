package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

type applyArgs struct {
	PositionalArg  string // deployment ID OR project path
	Stack          string
	AutoApprove    bool
	PartialFailure string
	Adapters       cloud.Registry
	Runner         tofu.Runner
	Inventory      inventory.Repo
	WorkRoot       string
	Stdout, Stderr io.Writer
}

func newApplyCommand() *cobra.Command {
	var stack, partialFailure string
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "apply [deployment-id | path]",
		Short: "Apply by deployment ID (preferred), or validate-plan-apply against a stack",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
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
			code := runApply(cmd.Context(), applyArgs{
				PositionalArg:  arg,
				Stack:          stack,
				AutoApprove:    autoApprove,
				PartialFailure: partialFailure,
				Adapters:       reg,
				Runner:         tofu.NewExecRunner(),
				Inventory:      repo,
				Stdout:         cmd.OutOrStdout(),
				Stderr:         cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
		SilenceUsage: true, SilenceErrors: true,
	}
	cmd.Flags().StringVar(&stack, "stack", "", "stack to plan + apply (only when no deployment ID given)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
	cmd.Flags().StringVar(&partialFailure, "partial-failure", "leave", "leave | rollback | retry-failed")
	return cmd
}

func runApply(ctx context.Context, in applyArgs) int {
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
		_, err := eng.Apply(ctx, in.PositionalArg, engine.ApplyOpts{
			AutoApprove:    true,
			PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
		})
		if err != nil {
			fmt.Fprintf(in.Stderr, "apply: %v\n", err)
			return 1
		}
		fmt.Fprintf(in.Stdout, "Applied deployment %s\n", in.PositionalArg)
		return 0
	}

	// Plan + apply form.
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
	plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{
		PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
	})
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
	printApplyResult(in.Stdout, res)
	if res.Status != provisioner.ApplySucceeded {
		return 1
	}
	return 0
}

func printApplyResult(stdout io.Writer, res *provisioner.ApplyResult) {
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
		fmt.Fprintf(stdout, "  %s %s  %s/%s  status=%s\n", marker, r.Component, r.Cloud, r.Region, r.Status)
		if r.Error != nil {
			fmt.Fprintf(stdout, "      %v\n", r.Error)
		}
	}
	fmt.Fprintf(stdout, "\nApply %s: %d succeeded, %d failed, %d reverted, %d skipped\n",
		res.Status, succeeded, failed, reverted, skipped)
}
