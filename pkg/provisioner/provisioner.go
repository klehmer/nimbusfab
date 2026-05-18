package provisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/secrets"
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
	WorkRoot       string
	Adapters       cloud.Registry
	Runner         tofu.Runner
	SecretsBackend secrets.Backend    // optional; nil = pass empty env to runner
	Components     components.Registry // optional; nil = DefaultRegistry()
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
	if cfg.Components == nil {
		cfg.Components = components.DefaultRegistry()
	}
	return &runtimeProvisioner{cfg: cfg}, nil
}

type runtimeProvisioner struct {
	cfg Config
}

// Apply / Destroy / DetectDrift are implemented in apply.go / destroy.go /
// drift.go respectively.
