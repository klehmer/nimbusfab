package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/inventory"
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
	p, err := e.newProvisioner()
	if err != nil {
		return nil, fmt.Errorf("engine.Plan: %w", err)
	}
	res, err := p.Plan(ctx, provisioner.PlanInput{
		Project:        project,
		Stack:          stack,
		OrgID:          e.orgID(),
		DeploymentID:   "dep-" + uuid.NewString(),
		PartialFailure: opts.PartialFailure,
		Refresh:        opts.RefreshState,
		Targets:        opts.Targets,
	})
	if err != nil {
		return nil, err
	}
	if err := e.persistPlan(ctx, project, stack, opts, res); err != nil {
		return nil, fmt.Errorf("engine.Plan: persist: %w", err)
	}
	return res, nil
}

func (e *runtimeEngine) Apply(ctx context.Context, planID string, opts ApplyOpts) (string, error) {
	if inventory.IsNullRepo(e.cfg.InventoryRepo) {
		return "", inventory.ErrInventoryRequired
	}
	plan, d, err := e.reconstitutePlan(ctx, planID)
	if err != nil {
		return "", err
	}
	if d.Status != "planned" {
		return "", errDeploymentNotApplyable(d)
	}
	res, err := e.ApplyWithPlan(ctx, plan, opts)
	if err != nil {
		return "", err
	}
	finished := time.Now().UTC()
	_ = e.cfg.InventoryRepo.Deployments().UpdateStatus(ctx, e.orgID(), planID, string(res.Status), &finished)
	for _, tr := range res.TargetResults {
		ft := tr.FinishedAt
		_ = e.cfg.InventoryRepo.DeploymentTargets().UpdateStatus(ctx, e.orgID(), tr.DeploymentTargetID, string(tr.Status), &ft)
		_ = e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
			ID: "run-" + uuid.NewString(), OrgID: e.orgID(), DeploymentTargetID: tr.DeploymentTargetID,
			Kind: "apply", Status: string(tr.Status), StartedAt: tr.StartedAt, FinishedAt: &ft,
		})
	}
	return planID, nil
}

func (e *runtimeEngine) Destroy(ctx context.Context, deploymentID string, opts DestroyOpts) (string, error) {
	if inventory.IsNullRepo(e.cfg.InventoryRepo) {
		return "", inventory.ErrInventoryRequired
	}
	plan, _, err := e.reconstitutePlan(ctx, deploymentID)
	if err != nil {
		return "", err
	}
	res, err := e.DestroyWithPlan(ctx, plan, opts)
	if err != nil {
		return "", err
	}
	finished := time.Now().UTC()
	_ = e.cfg.InventoryRepo.Deployments().UpdateStatus(ctx, e.orgID(), deploymentID, "destroyed", &finished)
	for _, tr := range res.TargetResults {
		finalStatus := "destroyed"
		if tr.Status == provisioner.RunStatusFailed {
			finalStatus = "failed"
		}
		ft := tr.FinishedAt
		_ = e.cfg.InventoryRepo.DeploymentTargets().UpdateStatus(ctx, e.orgID(), tr.DeploymentTargetID, finalStatus, &ft)
		_ = e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
			ID: "run-" + uuid.NewString(), OrgID: e.orgID(), DeploymentTargetID: tr.DeploymentTargetID,
			Kind: "destroy", Status: string(tr.Status), StartedAt: tr.StartedAt, FinishedAt: &ft,
		})
	}
	return deploymentID, nil
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
	return e.estimateCost(ctx, plan)
}

func (e *runtimeEngine) GetCostActuals(ctx context.Context, query CostQuery) (*CostReport, error) {
	return nil, errNotImplemented
}

func (e *runtimeEngine) DetectDrift(ctx context.Context, deploymentID string) (*DriftReport, error) {
	if inventory.IsNullRepo(e.cfg.InventoryRepo) {
		return nil, inventory.ErrInventoryRequired
	}
	plan, _, err := e.reconstitutePlan(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	rep, err := e.DetectDriftWithPlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, tr := range rep.TargetReports {
		summary, _ := json.Marshal(tr)
		_ = e.cfg.InventoryRepo.DriftStatus().Upsert(ctx, inventory.DriftRecord{
			DeploymentTargetID: tr.DeploymentTargetID, OrgID: e.orgID(),
			DetectedAt: now, HasDrift: tr.HasDrift, SummaryJSON: summary,
		})
	}
	return rep, nil
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
		WorkRoot:       workRoot,
		Adapters:       e.cfg.CloudAdapters,
		Runner:         runner,
		SecretsBackend: e.cfg.SecretsBackend,
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
