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
	"github.com/klehmer/nimbusfab/pkg/parity"
)

type parityArgs struct {
	ProjectPath    string
	Stack          string
	Component      string
	Adapters       cloud.Registry
	Runner         tofu.Runner
	Inventory      inventory.Repo
	WorkRoot       string
	Stdout, Stderr io.Writer
}

func newParityCommand() *cobra.Command {
	var stack, component string
	cmd := &cobra.Command{
		Use:   "parity [path]",
		Short: "Show parity report for a project's components across cloud targets",
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
			code := runParity(cmd.Context(), parityArgs{
				ProjectPath: projectPath, Stack: stack, Component: component,
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
	cmd.Flags().StringVar(&component, "component", "", "limit output to one component")
	_ = cmd.MarkFlagRequired("stack")
	return cmd
}

func runParity(ctx context.Context, in parityArgs) int {
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
	rules, _ := parity.LoadRulesFromFile(in.ProjectPath + "/parity.yaml")
	pEngine, perr := parity.NewEngine()
	if perr != nil {
		fmt.Fprintf(in.Stderr, "parity engine: %v\n", perr)
		return 1
	}
	for i := range plan.ParityReports {
		rep := &plan.ParityReports[i]
		if in.Component != "" && rep.Component != in.Component {
			continue
		}
		parity.RenderText(in.Stdout, rep)
		violations, _ := pEngine.EvaluateRules(ctx, rep, rules)
		parity.RenderViolations(in.Stdout, violations)
		fmt.Fprintln(in.Stdout)
	}
	return 0
}
