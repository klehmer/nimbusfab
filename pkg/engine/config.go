package engine

import (
	"context"
	"errors"
	"log/slog"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/cost/collector"
	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/secrets"
)

// Config carries all engine dependencies. A nil InventoryRepo activates
// no-inventory mode: Plan / Apply still work, but no run history, drift, or
// cost-actuals storage is available.
type Config struct {
	Logger         *slog.Logger
	InventoryRepo  inventory.Repo
	SecretsBackend secrets.Backend
	CloudAdapters  cloud.Registry
	ComponentTypes components.Registry
	Estimator      estimator.Estimator
	Collector      collector.Collector
	TofuRunner     tofu.Runner
	TofuBinary     string // (deprecated) path; prefer TofuRunner
	WorkRoot       string // root of per-deployment workspaces; "" = OS tempdir
	WorkDir        string // (deprecated) prefer WorkRoot
}

// New constructs an Engine wired against the supplied dependencies.
// A nil InventoryRepo activates no-inventory mode (NewNullRepo).
func New(ctx context.Context, cfg Config) (Engine, error) {
	if cfg.CloudAdapters == nil {
		return nil, errors.New("engine.New: CloudAdapters registry is required")
	}
	if cfg.InventoryRepo == nil {
		cfg.InventoryRepo = inventory.NewNullRepo()
	}
	return &runtimeEngine{cfg: cfg}, nil
}

type runtimeEngine struct {
	cfg Config
}
