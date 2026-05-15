// Command nimbusfab is the Nimbusfab platform CLI. It instantiates an
// in-process Engine (SQLite inventory by default, --no-inventory disables
// persistence) and dispatches commands. The command surface deliberately
// mirrors what the web backend exposes over REST so the two stay aligned.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is the entry point for tests; main() forwards os.Args. Wired to Cobra
// in the full implementation; this stub returns a helpful message until
// cmd/cli has its real surface.
func run(_ context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println(`nimbusfab: the Nimbusfab platform CLI

Commands (skeletal):
  init                 scaffold project.yaml
  validate             validate a project
  plan [stack]         plan changes against a stack
  apply [stack]        apply a plan (synchronous; --detach for async)
  up [stack]           plan + apply with -auto-approve guard
  destroy [stack]      tear down a deployment
  cost estimate        re-emit cost estimate from last plan
  cost actual          show actual costs from the dashboard
  drift                detect drift against last-known state
  import               import existing cloud resources
  state {show,rm,mv}   tofu state passthrough
  serve                start the web backend in-process

Global flags:
  --no-inventory       run without SQLite; drift / cost actuals disabled
  --inventory-dsn URL  override default inventory location
  --org NAME           scope to a specific org (multi-tenant mode)
  --stack NAME         default stack for stack-aware commands
  --json               emit machine-readable JSON output

Implementation deferred to the per-subsystem specs.`)
		return nil
	}
	return fmt.Errorf("nimbusfab: command %q not implemented yet", args[0])
}
