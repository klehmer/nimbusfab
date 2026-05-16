package provisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

// ErrNotImplementedYet is returned by Provisioner methods that have not yet
// been wired up.
var ErrNotImplementedYet = errors.New("provisioner: not implemented yet")

// Provisioner orchestrates plan/apply/destroy/drift across DeploymentTargets.
type Provisioner interface {
	Plan(ctx context.Context, in PlanInput) (*PlanResult, error)
	Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)
	Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error)
	DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error)
}

// Config carries the dependencies a real Provisioner needs.
type Config struct {
	WorkRoot string
	Adapters cloud.Registry
	Runner   tofu.Runner
}

// New returns a runtime Provisioner wired against the supplied dependencies.
func New(cfg Config) (Provisioner, error) {
	if cfg.WorkRoot == "" {
		return nil, fmt.Errorf("provisioner: Config.WorkRoot is required")
	}
	if cfg.Adapters == nil {
		return nil, fmt.Errorf("provisioner: Config.Adapters is required")
	}
	if cfg.Runner == nil {
		return nil, fmt.Errorf("provisioner: Config.Runner is required")
	}
	return &runtimeProvisioner{cfg: cfg}, nil
}

type runtimeProvisioner struct {
	cfg Config
}

// Phase-2 stubs — DetectDrift lands in drift.go (Task 8); stays here until.

func (*runtimeProvisioner) DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error) {
	return nil, ErrNotImplementedYet
}
