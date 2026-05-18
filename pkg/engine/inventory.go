package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// ensureOrgProjectStack upserts org/project/stack rows for inventory mode.
// Returns the IDs. In no-inventory mode returns synthetic IDs; the engine's
// downstream code doesn't need them in that case.
func (e *runtimeEngine) ensureOrgProjectStack(ctx context.Context, project *ir.Project, stackName string) (orgID, projectID, stackID string, err error) {
	orgID = e.orgID()
	if inventory.IsNullRepo(e.cfg.InventoryRepo) {
		return orgID, "local-" + project.Name, "local-" + project.Name + "-" + stackName, nil
	}
	// Create org if missing; ignore duplicate error.
	_ = e.cfg.InventoryRepo.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: orgID})

	existing, _ := e.cfg.InventoryRepo.Projects().List(ctx, orgID)
	for _, p := range existing {
		if p.Name == project.Name {
			projectID = p.ID
			break
		}
	}
	if projectID == "" {
		projectID = "proj-" + uuid.NewString()
		if err = e.cfg.InventoryRepo.Projects().Create(ctx, inventory.Project{
			ID: projectID, OrgID: orgID, Name: project.Name,
		}); err != nil {
			return "", "", "", fmt.Errorf("project upsert: %w", err)
		}
	}

	stack := project.Stacks[stackName]
	cfgJSON, _ := json.Marshal(stack.StateBackend.Config)
	s, _ := e.cfg.InventoryRepo.Stacks().GetByName(ctx, orgID, projectID, stackName)
	if s == nil {
		stackID = "stk-" + uuid.NewString()
	} else {
		stackID = s.ID
	}
	if err = e.cfg.InventoryRepo.Stacks().Upsert(ctx, inventory.Stack{
		ID: stackID, OrgID: orgID, ProjectID: projectID, Name: stackName,
		StateBackendKind: stack.StateBackend.Kind,
		StateBackendCfg:  cfgJSON,
	}); err != nil {
		return "", "", "", fmt.Errorf("stack upsert: %w", err)
	}
	return orgID, projectID, stackID, nil
}

// persistPlan writes deployment + targets + plan runs for a freshly computed
// PlanResult. Mutates plan.DeploymentID and each TargetPlan.DeploymentTargetID.
func (e *runtimeEngine) persistPlan(ctx context.Context, project *ir.Project, stackName string, opts PlanOpts, plan *provisioner.PlanResult) error {
	if inventory.IsNullRepo(e.cfg.InventoryRepo) {
		return nil
	}
	orgID, projectID, stackID, err := e.ensureOrgProjectStack(ctx, project, stackName)
	if err != nil {
		return err
	}
	for _, comp := range project.Components {
		irJSON, _ := json.Marshal(comp)
		if err := e.cfg.InventoryRepo.Components().Upsert(ctx, inventory.Component{
			ID: "cmp-" + uuid.NewString(), OrgID: orgID, ProjectID: projectID, StackID: stackID,
			Name: comp.Name, Type: comp.Type, IRJSON: irJSON,
		}); err != nil {
			return fmt.Errorf("component upsert: %w", err)
		}
	}
	var driftSecs int
	if stack, ok := project.Stacks[stackName]; ok && stack.Drift != nil && stack.Drift.Interval != "" {
		if d, err := time.ParseDuration(stack.Drift.Interval); err == nil {
			driftSecs = int(d.Seconds())
		}
	}
	deploymentID := "dep-" + uuid.NewString()
	if err := e.cfg.InventoryRepo.Deployments().Create(ctx, inventory.Deployment{
		ID: deploymentID, OrgID: orgID, ProjectID: projectID, StackID: stackID,
		Status: "planned", PartialFailurePolicy: string(opts.PartialFailure),
		StartedAt:            time.Now().UTC(),
		DriftIntervalSeconds: driftSecs,
	}); err != nil {
		return fmt.Errorf("deployment create: %w", err)
	}
	plan.DeploymentID = deploymentID
	now := time.Now().UTC()

	// Per-target plan-run IDs are retained so cost estimates can attach to
	// the right run. The Dashboards Phase 1 view reads them via
	// CostEstimates.ListByDeployment which JOINs runs→targets.
	planRuns := make([]planRunRef, 0, len(plan.Targets))

	for i := range plan.Targets {
		tp := &plan.Targets[i]
		targetID := "tgt-" + uuid.NewString()
		tp.DeploymentTargetID = targetID
		if err := e.cfg.InventoryRepo.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: targetID, OrgID: orgID, DeploymentID: deploymentID,
			ComponentName: tp.Component, Cloud: tp.Cloud, Region: tp.Region,
			CredentialRef: "",
			WorkspacePath: tp.WorkspaceDir, PlanFile: tp.PlanFile,
			Status:    "planned",
			StartedAt: now,
		}); err != nil {
			return fmt.Errorf("target create: %w", err)
		}
		runFinished := now
		runID := "run-" + uuid.NewString()
		if err := e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
			ID: runID, OrgID: orgID, DeploymentTargetID: targetID,
			Kind: "plan", Status: "succeeded", StartedAt: now, FinishedAt: &runFinished,
		}); err != nil {
			return fmt.Errorf("plan run create: %w", err)
		}
		planRuns = append(planRuns, planRunRef{
			RunID:      runID,
			TargetID:   targetID,
			Cloud:      tp.Cloud,
			Region:     tp.Region,
			Primitives: tp.RawPrimitives,
		})
	}

	// Persist cost estimates. Failure here is non-fatal: planning succeeded;
	// the deployment is usable; only the dashboard view is impacted. Log via
	// the engine's logger when one is configured.
	if err := e.persistCostEstimates(ctx, orgID, planRuns); err != nil {
		if e.cfg.Logger != nil {
			e.cfg.Logger.Warn("cost estimate persistence failed", "deployment", deploymentID, "err", err)
		}
	}
	return nil
}

// planRunRef bundles what persistCostEstimates needs to map estimator
// output back to inventory rows.
type planRunRef struct {
	RunID      string
	TargetID   string
	Cloud      string
	Region     string
	Primitives []ir.ResourcePrimitive
}

// reconstitutePlan rebuilds a PlanResult from inventory rows for
// Apply/Destroy/Drift by deployment ID.
func (e *runtimeEngine) reconstitutePlan(ctx context.Context, deploymentID string) (*provisioner.PlanResult, *inventory.Deployment, error) {
	orgID := e.orgID()
	d, err := e.cfg.InventoryRepo.Deployments().Get(ctx, orgID, deploymentID)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, inventory.ErrDeploymentNotFound
	}
	targets, err := e.cfg.InventoryRepo.DeploymentTargets().ListByDeployment(ctx, orgID, deploymentID)
	if err != nil {
		return nil, nil, fmt.Errorf("list targets: %w", err)
	}
	plan := &provisioner.PlanResult{
		DeploymentID:   deploymentID,
		PartialFailure: provisioner.PartialFailurePolicy(d.PartialFailurePolicy),
		GeneratedAt:    d.StartedAt,
	}
	for _, t := range targets {
		plan.Targets = append(plan.Targets, provisioner.TargetPlan{
			DeploymentTargetID: t.ID,
			Component:          t.ComponentName,
			Cloud:              t.Cloud,
			Region:             t.Region,
			WorkspaceDir:       t.WorkspacePath,
			PlanFile:           t.PlanFile,
		})
	}
	return plan, d, nil
}

// errDeploymentNotApplyable wraps a friendlier message around the status-
// mismatch error.
func errDeploymentNotApplyable(d *inventory.Deployment) error {
	return fmt.Errorf("%w: deployment %s is in status %q (expected 'planned')",
		inventory.ErrDeploymentWrongStatus, d.ID, d.Status)
}
