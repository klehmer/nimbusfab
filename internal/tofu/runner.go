// Package tofu wraps the `tofu` CLI as a subprocess. All shelling out to
// OpenTofu flows through this package; no other package may invoke the
// binary directly. The Runner is responsible for workspace layout, argument
// construction, stream parsing, and exit-code mapping.
package tofu

import (
	"context"
	"io"
	"time"
)

// Runner runs OpenTofu commands against a workspace directory.
type Runner interface {
	Init(ctx context.Context, ws Workspace) error
	Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error)
	Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error
	Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error
	Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error) // JSON
	StateShow(ctx context.Context, ws Workspace) ([]byte, error)             // `tofu show -json`
	StateRm(ctx context.Context, ws Workspace, address string) error
	StateMv(ctx context.Context, ws Workspace, from, to string) error
	Version(ctx context.Context) (string, error)
}

// Workspace is one (deployment_target_id) directory on disk plus its backend
// configuration. The runner writes main.tf.json + backend.tf.json and runs
// tofu commands inside.
type Workspace struct {
	Dir         string
	Vars        map[string]any
	Environment map[string]string
	Stdout      io.Writer
	Stderr      io.Writer
}

// PlanOpts adjust the plan invocation.
type PlanOpts struct {
	Destroy      bool
	Refresh      bool
	Targets      []string
	OutFile      string        // plan binary path; required
	Timeout      time.Duration
}

// ApplyOpts adjust the apply invocation.
type ApplyOpts struct {
	AutoApprove bool
	Timeout     time.Duration
}

// DestroyOpts adjust the destroy invocation.
type DestroyOpts struct {
	AutoApprove bool
	Timeout     time.Duration
}

// PlanArtifact captures the outputs of a plan.
type PlanArtifact struct {
	PlanFile   string
	JSONPlan   []byte // `tofu show -json plan`
	HasChanges bool
}
