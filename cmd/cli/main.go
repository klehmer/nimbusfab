// Command nimbusfab is the Nimbusfab platform CLI. It instantiates an
// in-process Engine and dispatches commands.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagInventoryDSN string
	flagNoInventory  bool
)

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
	root.AddCommand(newValidateCommand())
	root.AddCommand(newPlanCommand())
	root.AddCommand(newApplyCommand())
	root.AddCommand(newDestroyCommand())
	root.AddCommand(newDriftCommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
