package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

func (rp *runtimeProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
	if in.PlanResult == nil {
		return nil, fmt.Errorf("provisioner.Destroy: PlanResult required (Phase 2 does not yet resolve deployments from inventory)")
	}
	if in.PartialFailure == "" {
		in.PartialFailure = PartialFailureLeave
	}

	// When a Project is provided, use the toposort-aware path that destroys
	// downstream components first (reverse dependency order).
	if in.Project != nil {
		return rp.destroyToposorted(ctx, in)
	}

	return rp.destroyExisting(ctx, in)
}

// destroyExisting is the original destroy path: reverses the source order
// from the plan and destroys sequentially with concurrency semaphores.
func (rp *runtimeProvisioner) destroyExisting(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
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

// destroyToposorted destroys targets in reverse-dependency order determined by
// a DAG toposort of in.Project.Components.  Downstream (dependent) components
// are destroyed before the upstream components they depend on.
func (rp *runtimeProvisioner) destroyToposorted(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
	plan := in.PlanResult

	// Build TargetIdent slice from the plan targets.
	idents := make([]upstream.TargetIdent, 0, len(plan.Targets))
	for _, tp := range plan.Targets {
		idents = append(idents, upstream.TargetIdent{
			Component: tp.Component,
			Cloud:     tp.Cloud,
			Region:    tp.Region,
		})
	}

	ordered, err := upstream.ToposortTargets(idents, in.Project.Components)
	if err != nil {
		return nil, fmt.Errorf("provisioner.Destroy: toposort: %w", err)
	}

	// Reverse for destroy: downstream first, upstream last.
	for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	}

	byIdent := indexTargetsByIdent(plan.Targets)

	var results []TargetApplyResult

	for _, ident := range ordered {
		tp, ok := byIdent[ident]
		if !ok {
			continue
		}

		startedAt := time.Now().UTC()

		env, envErr := resolveEnvFor(ctx, rp.cfg.SecretsBackend, tp.CredentialRef)
		if envErr != nil {
			results = append(results, TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component, Cloud: ident.Cloud, Region: ident.Region,
				RunID:      "run-" + uuid.NewString(),
				Status:     RunStatusFailed,
				Error:      envErr,
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			})
			continue
		}

		comp := findComponent(in.Project, ident.Component)
		// Placeholders for variable declarations; real values not needed for destroy.
		placeholders, _ := upstream.PlanPlaceholders(comp.Refs, in.Project.Components, rp.cfg.Components)

		ws := tofu.Workspace{
			Dir:         tp.WorkspaceDir,
			Environment: env,
			Vars:        stringsToAnys(placeholders),
		}

		emit(in.EventSink, RunEvent{
			Timestamp:          time.Now().UTC(),
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          ident.Component, Cloud: ident.Cloud, Region: ident.Region,
			Kind: RunEventStart, Message: "destroy starting",
		})

		if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: in.AutoApprove}); err != nil {
			results = append(results, TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component, Cloud: ident.Cloud, Region: ident.Region,
				RunID:      "run-" + uuid.NewString(),
				Status:     RunStatusFailed,
				Error:      fmt.Errorf("tofu destroy: %w", err),
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			})
			continue
		}

		results = append(results, TargetApplyResult{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          ident.Component, Cloud: ident.Cloud, Region: ident.Region,
			RunID:      "run-" + uuid.NewString(),
			Status:     RunStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: time.Now().UTC(),
		})
	}

	return &ApplyResult{
		DeploymentID:  plan.DeploymentID,
		Status:        summarizeApplyStatus(results),
		TargetResults: results,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

func stringsToAnys(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func reverseComponents(cs []ir.Component) {
	for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
		cs[i], cs[j] = cs[j], cs[i]
	}
}
