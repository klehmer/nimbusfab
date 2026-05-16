package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// errNotImplemented is the engine-package equivalent of provisioner's stub
// error. Methods that haven't been wired up yet return this so the CLI prints
// a friendly message instead of crashing.
var errNotImplemented = errors.New("engine: not implemented yet")

func (e *runtimeEngine) LoadProject(ctx context.Context, path string) (*ir.Project, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) Validate(ctx context.Context, project *ir.Project) (*ValidationReport, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) Plan(ctx context.Context, project *ir.Project, stack string, opts PlanOpts) (*PlanResult, error) {
	runner := e.cfg.TofuRunner
	if runner == nil {
		runner = tofu.NewExecRunner()
	}
	workRoot := e.cfg.WorkRoot
	if workRoot == "" {
		workRoot = e.cfg.WorkDir
	}
	if workRoot == "" {
		workRoot = filepath.Join(os.TempDir(), "nimbusfab")
	}
	p, err := provisioner.New(provisioner.Config{
		WorkRoot: workRoot,
		Adapters: e.cfg.CloudAdapters,
		Runner:   runner,
	})
	if err != nil {
		return nil, fmt.Errorf("engine.Plan: %w", err)
	}
	return p.Plan(ctx, provisioner.PlanInput{
		Project:        project,
		Stack:          stack,
		OrgID:          e.orgID(),
		DeploymentID:   "dep-" + uuid.NewString(),
		PartialFailure: opts.PartialFailure,
		Refresh:        opts.RefreshState,
		Targets:        opts.Targets,
	})
}

func (e *runtimeEngine) Apply(ctx context.Context, planID string, opts ApplyOpts) (string, error) {
	return "", errNotImplemented
}

func (e *runtimeEngine) Destroy(ctx context.Context, deploymentID string, opts DestroyOpts) (string, error) {
	return "", errNotImplemented
}

func (e *runtimeEngine) Import(ctx context.Context, project *ir.Project, mapping ImportMap) (*ImportResult, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) GetRun(ctx context.Context, runID string) (*Run, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) StreamRun(ctx context.Context, runID string) (<-chan RunEvent, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) EstimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) GetCostActuals(ctx context.Context, query CostQuery) (*CostReport, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) DetectDrift(ctx context.Context, deploymentID string) (*DriftReport, error) {
	// Phase 2: inventory-resolved deployments are not yet wired. Callers
	// use DetectDriftWithPlan instead. The inventory phase replaces this stub.
	return nil, errNotImplemented
}

// newProvisioner constructs a provisioner with the engine's deps. Local helper.
func (e *runtimeEngine) newProvisioner() (provisioner.Provisioner, error) {
	runner := e.cfg.TofuRunner
	if runner == nil {
		runner = tofu.NewExecRunner()
	}
	workRoot := e.cfg.WorkRoot
	if workRoot == "" {
		workRoot = e.cfg.WorkDir
	}
	if workRoot == "" {
		workRoot = filepath.Join(os.TempDir(), "nimbusfab")
	}
	return provisioner.New(provisioner.Config{
		WorkRoot: workRoot, Adapters: e.cfg.CloudAdapters, Runner: runner,
	})
}

// ApplyWithPlan is the Phase-2 surface: caller passes the PlanResult directly
// since inventory persistence isn't wired. Becomes Apply(planID) in the
// inventory phase.
func (e *runtimeEngine) ApplyWithPlan(ctx context.Context, plan *PlanResult, opts ApplyOpts) (*ApplyResult, error) {
	p, err := e.newProvisioner()
	if err != nil {
		return nil, err
	}
	return p.Apply(ctx, provisioner.ApplyInput{
		PlanResult:     plan,
		OrgID:          e.orgID(),
		PartialFailure: opts.PartialFailure,
		AutoApprove:    opts.AutoApprove,
	})
}

// DestroyWithPlan mirrors ApplyWithPlan for destroys.
func (e *runtimeEngine) DestroyWithPlan(ctx context.Context, plan *PlanResult, opts DestroyOpts) (*ApplyResult, error) {
	p, err := e.newProvisioner()
	if err != nil {
		return nil, err
	}
	return p.Destroy(ctx, provisioner.DestroyInput{
		PlanResult:  plan,
		OrgID:       e.orgID(),
		AutoApprove: opts.AutoApprove,
	})
}

// DetectDriftWithPlan runs drift detection against an in-memory plan.
func (e *runtimeEngine) DetectDriftWithPlan(ctx context.Context, plan *PlanResult) (*DriftReport, error) {
	p, err := e.newProvisioner()
	if err != nil {
		return nil, err
	}
	return p.DetectDrift(ctx, provisioner.DriftInput{
		PlanResult: plan,
		OrgID:      e.orgID(),
	})
}

// orgID returns the OrgID this engine is scoped to. v1 returns "local" in
// no-inventory mode; full multi-tenancy lands when inventory persistence does.
func (e *runtimeEngine) orgID() string {
	if e.cfg.InventoryRepo == nil {
		return "local"
	}
	return "local"
}
