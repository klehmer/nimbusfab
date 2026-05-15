// Command nimbusfab is the Nimbusfab platform CLI. It instantiates an
// in-process Engine and dispatches commands. Phase 1 wires only the
// `validate` subcommand; later phases add `show`, `plan`, `apply`, etc.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "nimbusfab",
		Short:         "Multi-cloud Infrastructure-as-Code framework over OpenTofu",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newValidateCommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
