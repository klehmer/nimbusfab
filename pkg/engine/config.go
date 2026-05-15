package engine

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kratus8990/cloud-infra-manager/pkg/cloud"
	"github.com/kratus8990/cloud-infra-manager/pkg/components"
	"github.com/kratus8990/cloud-infra-manager/pkg/cost/collector"
	"github.com/kratus8990/cloud-infra-manager/pkg/cost/estimator"
	"github.com/kratus8990/cloud-infra-manager/pkg/inventory"
	"github.com/kratus8990/cloud-infra-manager/pkg/secrets"
)

// Config carries all engine dependencies. A nil InventoryRepo activates
// no-inventory mode: Plan / Apply still work, but no run history, drift, or
// cost-actuals storage is available.
type Config struct {
	Logger          *slog.Logger
	InventoryRepo   inventory.Repo            // nil => no-inventory mode
	SecretsBackend  secrets.Backend
	CloudAdapters   map[string]cloud.Adapter  // keyed by cloud short name
	ComponentTypes  components.Registry
	Estimator       estimator.Estimator
	Collector       collector.Collector
	TofuBinary      string                    // path to `tofu` binary; defaults to PATH lookup
	WorkDir         string                    // root of per-deployment workspaces
}

// New constructs an Engine from cfg. Implementations live alongside this
// package and are not exported; callers depend only on the Engine interface.
//
// Implementation deferred to the per-subsystem specs.
func New(ctx context.Context, cfg Config) (Engine, error) {
	if cfg.SecretsBackend == nil {
		return nil, errors.New("engine.New: SecretsBackend is required")
	}
	if len(cfg.CloudAdapters) == 0 {
		return nil, errors.New("engine.New: at least one cloud adapter is required")
	}
	if cfg.ComponentTypes == nil {
		return nil, errors.New("engine.New: ComponentTypes registry is required")
	}
	return nil, errors.New("engine.New: not implemented yet")
}
