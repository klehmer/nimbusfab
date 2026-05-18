// Command nimbusfab is the Nimbusfab platform CLI. It instantiates an
// in-process Engine and dispatches commands.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/tofu"
)

var (
	flagInventoryDSN string
	flagNoInventory  bool
	flagFakeRunner   bool
)

// defaultRunner returns the tofu runner to use for CLI commands. When
// --fake-runner is set, no real tofu subprocess is invoked: workspaces are
// still rendered and persisted, but Plan/Apply/Destroy/Drift return scripted
// success. Useful for demos, smoke tests, and CI without cloud credentials.
// Has no effect on what gets written to inventory — the deployment row and
// all targets persist exactly as in a real run.
func defaultRunner() tofu.Runner {
	if !flagFakeRunner {
		return tofu.NewExecRunner()
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKEPLAN")
	// Make plans look like work: 3 creates per target so adds=3 in the
	// CLI summary. Apply/Destroy still no-op.
	fake.PlanReturn = &tofu.PlanArtifact{
		JSONPlan: []byte(`{"resource_changes":[` +
			`{"change":{"actions":["create"]}},` +
			`{"change":{"actions":["create"]}},` +
			`{"change":{"actions":["create"]}}` +
			`]}`),
		HasChanges: true,
	}
	return fake
}

func main() {
	root := &cobra.Command{
		Use:           "nimbusfab",
		Short:         "Multi-cloud Infrastructure-as-Code framework over OpenTofu",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagInventoryDSN, "inventory-dsn", "",
		"inventory DB DSN (default: sqlite://~/.config/nimbusfab/inventory.db)")
	root.PersistentFlags().BoolVar(&flagNoInventory, "no-inventory", false,
		"disable inventory persistence; all operations are in-process only")
	root.PersistentFlags().BoolVar(&flagFakeRunner, "fake-runner", false,
		"use the in-process FakeRunner instead of real tofu; for demos/CI without cloud creds")
	root.AddCommand(newValidateCommand())
	root.AddCommand(newPlanCommand())
	root.AddCommand(newApplyCommand())
	root.AddCommand(newDestroyCommand())
	root.AddCommand(newDriftCommand())
	root.AddCommand(newParityCommand())
	root.AddCommand(newCostCommand())
	root.AddCommand(newUserCommand())
	root.AddCommand(newPATCommand())
	root.AddCommand(newGraphCommand())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
