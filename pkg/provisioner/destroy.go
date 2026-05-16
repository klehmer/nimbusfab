package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
	if in.PlanResult == nil {
		return nil, fmt.Errorf("provisioner.Destroy: PlanResult required (Phase 2 does not yet resolve deployments from inventory)")
	}
	if in.PartialFailure == "" {
		in.PartialFailure = PartialFailureLeave
	}

	sems := newSemaphores(resolveCaps(concurrencyCaps{
		Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
	}))

	plan := in.PlanResult
	work := func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
		startedAt := time.Now().UTC()
		tp := findTargetPlan(plan, comp.Name, t.Cloud, t.Region)
		if tp == nil {
			return TargetApplyResult{
				Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
				Status:     RunStatusFailed,
				Error:      fmt.Errorf("destroy: no plan for %s/%s/%s", comp.Name, t.Cloud, t.Region),
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}
		}
		env, envErr := resolveEnvFor(ctx, rp.cfg.SecretsBackend, tp.CredentialRef)
		if envErr != nil {
			return TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
				RunID:      "run-" + uuid.NewString(),
				Status:     RunStatusFailed,
				Error:      envErr,
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}
		}
		ws := tofu.Workspace{Dir: tp.WorkspaceDir, Environment: env}
		emit(in.EventSink, RunEvent{
			Timestamp:          time.Now().UTC(),
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
			Kind: RunEventStart, Message: "destroy starting",
		})
		if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: true}); err != nil {
			return TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
				RunID:      "run-" + uuid.NewString(),
				Status:     RunStatusFailed,
				Error:      fmt.Errorf("tofu destroy: %w", err),
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}
		}
		return TargetApplyResult{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
			RunID:      "run-" + uuid.NewString(),
			Status:     RunStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: time.Now().UTC(),
		}
	}

	// Destroy walks components in REVERSE order: dependents first.
	componentsOrdered := componentsFromPlan(plan)
	reverseComponents(componentsOrdered)

	var results []TargetApplyResult
	for _, comp := range componentsOrdered {
		results = append(results, runComponent(ctx, comp, sems, work)...)
	}
	return &ApplyResult{
		DeploymentID:  plan.DeploymentID,
		Status:        summarizeApplyStatus(results),
		TargetResults: results,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

func reverseComponents(cs []ir.Component) {
	for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
		cs[i], cs[j] = cs[j], cs[i]
	}
}
