package provisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/state/bridge"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

func (rp *runtimeProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	if in.PlanResult == nil {
		return nil, fmt.Errorf("provisioner.Apply: PlanResult required")
	}
	if in.PartialFailure == "" {
		in.PartialFailure = PartialFailureLeave
	}
	if in.MaxRetries <= 0 {
		in.MaxRetries = 1
	}

	// When a Project is provided, use the toposort-aware path that re-plans
	// each target with real upstream output values and propagates blocked status.
	if in.Project != nil {
		return rp.applyToposorted(ctx, in)
	}

	sems := newSemaphores(resolveCaps(concurrencyCaps{
		Global:   in.MaxConcurrentTargets,
		PerCloud: in.MaxConcurrentPerCloud,
	}))

	work := rp.applyWorker(in)
	plan := in.PlanResult

	componentsOrdered := componentsFromPlan(plan)
	var results []TargetApplyResult
	for _, comp := range componentsOrdered {
		results = append(results, runComponent(ctx, comp, sems, work)...)
	}
	results = rp.maybeRetry(ctx, in, results, work, sems)
	if in.PartialFailure == PartialFailureRollback && hasAnyFailure(results) {
		results = rp.rollback(ctx, in, results)
	}
	return &ApplyResult{
		DeploymentID:  plan.DeploymentID,
		Status:        summarizeApplyStatus(results),
		TargetResults: results,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

// applyToposorted implements the toposort-aware apply path.  Targets are
// applied in component-dependency order.  Before each target is applied its
// workspace is re-planned with real output values extracted from already-applied
// upstream state files.  If any upstream target failed or was blocked, all
// downstream targets in the same component dependency chain are marked
// RunStatusBlocked and skipped.
func (rp *runtimeProvisioner) applyToposorted(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	plan := in.PlanResult

	// Build TargetIdent list from the plan.
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
		return nil, fmt.Errorf("provisioner.Apply: toposort: %w", err)
	}

	byIdent := indexTargetsByIdent(plan.Targets)

	// Track status per TargetIdent so downstream can check upstream status.
	statusByIdent := map[upstream.TargetIdent]RunStatus{}

	var results []TargetApplyResult

	for _, ident := range ordered {
		tp, ok := byIdent[ident]
		if !ok {
			continue
		}

		comp := findComponent(in.Project, ident.Component)

		// Check if any upstream of this component (same cloud/region) is failed or blocked.
		upstreamFailed := false
		for _, ref := range comp.Refs {
			upIdent := upstream.TargetIdent{
				Component: ref.Component,
				Cloud:     ident.Cloud,
				Region:    ident.Region,
			}
			if st, exists := statusByIdent[upIdent]; exists {
				if st == RunStatusFailed || st == RunStatusBlocked {
					upstreamFailed = true
					break
				}
			}
		}

		if upstreamFailed {
			blockedResult := TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Status:             RunStatusBlocked,
				StartedAt:          time.Now().UTC(),
				FinishedAt:         time.Now().UTC(),
			}
			statusByIdent[ident] = RunStatusBlocked
			results = append(results, blockedResult)
			continue
		}

		// Build real vars from upstream state files.
		vars, varErr := rp.buildRealVars(comp, ident, byIdent)
		if varErr != nil {
			failedResult := TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Status:             RunStatusFailed,
				Error:              varErr,
				StartedAt:          time.Now().UTC(),
				FinishedAt:         time.Now().UTC(),
			}
			statusByIdent[ident] = RunStatusFailed
			results = append(results, failedResult)
			continue
		}

		startedAt := time.Now().UTC()

		// Re-plan with real upstream vars.
		ws := tofu.Workspace{Dir: tp.WorkspaceDir, Vars: vars}
		planFile := filepath.Join(tp.WorkspaceDir, "plan.bin")
		if _, planErr := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{OutFile: planFile}); planErr != nil {
			failedResult := TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Status:             RunStatusFailed,
				Error:              fmt.Errorf("tofu re-plan: %w", planErr),
				StartedAt:          startedAt,
				FinishedAt:         time.Now().UTC(),
			}
			statusByIdent[ident] = RunStatusFailed
			results = append(results, failedResult)
			continue
		}

		// Apply.
		if applyErr := rp.cfg.Runner.Apply(ctx, ws, planFile, tofu.ApplyOpts{AutoApprove: in.AutoApprove}); applyErr != nil {
			failedResult := TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Status:             RunStatusFailed,
				Error:              fmt.Errorf("tofu apply: %w", applyErr),
				StartedAt:          startedAt,
				FinishedAt:         time.Now().UTC(),
			}
			statusByIdent[ident] = RunStatusFailed
			results = append(results, failedResult)
			continue
		}

		// Capture state and outputs post-apply.
		stateBytes, err := rp.cfg.Runner.StateShow(ctx, ws)
		var snap *StateSnapshot
		if err == nil {
			if bs, perr := bridge.Parse(stateBytes); perr == nil && bs != nil {
				snap = bridgeToProvisioner(bs, tp.DeploymentTargetID)
			}
		}
		outputs, _ := rp.cfg.Runner.Output(ctx, ws)

		statusByIdent[ident] = RunStatusSucceeded
		results = append(results, TargetApplyResult{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          ident.Component,
			Cloud:              ident.Cloud,
			Region:             ident.Region,
			RunID:              "run-" + uuid.NewString(),
			Status:             RunStatusSucceeded,
			Outputs:            outputs,
			State:              snap,
			StartedAt:          startedAt,
			FinishedAt:         time.Now().UTC(),
		})
	}

	return &ApplyResult{
		DeploymentID:  plan.DeploymentID,
		Status:        summarizeApplyStatus(results),
		TargetResults: results,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

// buildRealVars reads upstream state files and returns a vars map with real
// values substituted for each upstream ref on comp. Returns the original
// placeholder vars when the component has no refs.
func (rp *runtimeProvisioner) buildRealVars(
	comp ir.Component,
	ident upstream.TargetIdent,
	byIdent map[upstream.TargetIdent]TargetPlan,
) (map[string]any, error) {
	if len(comp.Refs) == 0 {
		return nil, nil
	}
	vars := map[string]any{}
	for _, ref := range comp.Refs {
		upIdent := upstream.TargetIdent{
			Component: ref.Component,
			Cloud:     ident.Cloud,
			Region:    ident.Region,
		}
		upTP, ok := byIdent[upIdent]
		if !ok {
			return nil, fmt.Errorf("%w: %s in %s/%s needs %s",
				upstream.ErrCrossTargetRefUnsupported, ident.Component, ident.Cloud, ident.Region, ref.Component)
		}
		stateFile := filepath.Join(upTP.WorkspaceDir, "terraform.tfstate")
		stateBytes, readErr := os.ReadFile(stateFile)
		if readErr != nil {
			return nil, fmt.Errorf("%w: %s: %v", upstream.ErrUpstreamStateUnreadable, ref.Component, readErr)
		}
		outputs, parseErr := upstream.ExtractOutputs(stateBytes)
		if parseErr != nil {
			return nil, parseErr
		}
		val, exists := outputs[ref.Output]
		if !exists {
			return nil, fmt.Errorf("%w: %s.%s", upstream.ErrUpstreamOutputMissing, ref.Component, ref.Output)
		}
		hcl, fmtErr := upstream.FormatHCLValue(val)
		if fmtErr != nil {
			return nil, fmt.Errorf("FormatHCLValue %s.%s: %w", ref.Component, ref.Output, fmtErr)
		}
		vars[upstream.VarName(ref.Component, ref.Output)] = hcl
	}
	return vars, nil
}

func hasAnyFailure(rs []TargetApplyResult) bool {
	for _, r := range rs {
		if r.Status == RunStatusFailed {
			return true
		}
	}
	return false
}

func (rp *runtimeProvisioner) maybeRetry(ctx context.Context, in ApplyInput, results []TargetApplyResult, work targetWorker, sems *semaphores) []TargetApplyResult {
	if in.PartialFailure != PartialFailureRetryFailed {
		return results
	}
	for attempt := 1; attempt <= in.MaxRetries; attempt++ {
		if !hasAnyFailure(results) {
			break
		}
		for i, r := range results {
			if r.Status != RunStatusFailed {
				continue
			}
			comp := ir.Component{Name: r.Component, Targets: []ir.DeploymentTarget{{
				Cloud: r.Cloud, Region: r.Region,
			}}}
			retried := runComponent(ctx, comp, sems, work)
			if len(retried) > 0 {
				results[i] = retried[0]
			}
		}
	}
	return results
}

func (rp *runtimeProvisioner) rollback(ctx context.Context, in ApplyInput, results []TargetApplyResult) []TargetApplyResult {
	plan := in.PlanResult
	for i, r := range results {
		if r.Status != RunStatusSucceeded {
			continue
		}
		tp := findTargetPlan(plan, r.Component, r.Cloud, r.Region)
		if tp == nil {
			continue
		}
		env, envErr := resolveEnvFor(ctx, rp.cfg.SecretsBackend, tp.CredentialRef)
		if envErr != nil {
			results[i].Status = RunStatusFailed
			results[i].Error = fmt.Errorf("rollback destroy: %w (original status: succeeded)", envErr)
			continue
		}
		ws := tofu.Workspace{Dir: tp.WorkspaceDir, Environment: env}
		if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: true}); err != nil {
			results[i].Status = RunStatusFailed
			results[i].Error = fmt.Errorf("rollback destroy failed: %w (original status: succeeded)", err)
			continue
		}
		results[i].Status = RunStatusReverted
		results[i].FinishedAt = time.Now().UTC()
	}
	return results
}

func (rp *runtimeProvisioner) applyWorker(in ApplyInput) targetWorker {
	plan := in.PlanResult
	return func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
		startedAt := time.Now().UTC()
		tp := findTargetPlan(plan, comp.Name, t.Cloud, t.Region)
		if tp == nil {
			return TargetApplyResult{
				Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
				Status:     RunStatusFailed,
				Error:      fmt.Errorf("apply: no plan for %s/%s/%s", comp.Name, t.Cloud, t.Region),
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}
		}
		emit(in.EventSink, RunEvent{
			Timestamp:          time.Now().UTC(),
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
			Kind: RunEventStart, Message: "apply starting",
		})
		env, envErr := resolveEnvFor(ctx, rp.cfg.SecretsBackend, tp.CredentialRef)
		if envErr != nil {
			emit(in.EventSink, RunEvent{
				Timestamp: time.Now().UTC(), DeploymentTargetID: tp.DeploymentTargetID,
				Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
				Kind: RunEventFailure, Message: envErr.Error(),
			})
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
		if err := rp.cfg.Runner.Apply(ctx, ws, tp.PlanFile, tofu.ApplyOpts{AutoApprove: in.AutoApprove}); err != nil {
			emit(in.EventSink, RunEvent{
				Timestamp: time.Now().UTC(), DeploymentTargetID: tp.DeploymentTargetID,
				Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
				Kind: RunEventFailure, Message: err.Error(),
			})
			return TargetApplyResult{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
				RunID:      "run-" + uuid.NewString(),
				Status:     RunStatusFailed,
				Error:      fmt.Errorf("tofu apply: %w", err),
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}
		}
		stateBytes, err := rp.cfg.Runner.StateShow(ctx, ws)
		var snap *StateSnapshot
		if err == nil {
			if bs, perr := bridge.Parse(stateBytes); perr == nil && bs != nil {
				snap = bridgeToProvisioner(bs, tp.DeploymentTargetID)
			}
		}
		outputs, _ := rp.cfg.Runner.Output(ctx, ws)
		emit(in.EventSink, RunEvent{
			Timestamp: time.Now().UTC(), DeploymentTargetID: tp.DeploymentTargetID,
			Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
			Kind: RunEventSuccess, Message: "apply complete",
		})
		return TargetApplyResult{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
			RunID:      "run-" + uuid.NewString(),
			Status:     RunStatusSucceeded,
			Outputs:    outputs,
			State:      snap,
			StartedAt:  startedAt,
			FinishedAt: time.Now().UTC(),
		}
	}
}

func findTargetPlan(plan *PlanResult, component, cloud, region string) *TargetPlan {
	for i := range plan.Targets {
		tp := &plan.Targets[i]
		if tp.Component == component && tp.Cloud == cloud && tp.Region == region {
			return tp
		}
	}
	return nil
}

// componentsFromPlan reconstructs a []ir.Component shape from the flat plan
// target list, preserving component order. Used by Apply/Destroy/Drift to
// drive the orchestrator.
func componentsFromPlan(plan *PlanResult) []ir.Component {
	seen := map[string]bool{}
	var out []ir.Component
	for _, tp := range plan.Targets {
		if !seen[tp.Component] {
			seen[tp.Component] = true
			out = append(out, ir.Component{Name: tp.Component})
		}
	}
	for i, c := range out {
		for _, tp := range plan.Targets {
			if tp.Component == c.Name {
				out[i].Targets = append(out[i].Targets, ir.DeploymentTarget{
					Cloud: tp.Cloud, Region: tp.Region,
				})
			}
		}
	}
	return out
}

func summarizeApplyStatus(results []TargetApplyResult) ApplyStatus {
	var succeeded, failed, skipped int
	for _, r := range results {
		switch r.Status {
		case RunStatusSucceeded:
			succeeded++
		case RunStatusFailed:
			failed++
		case RunStatusSkipped, RunStatusBlocked:
			skipped++
		case RunStatusReverted:
			failed++ // a reverted target is a failure outcome from the caller's POV
		}
	}
	switch {
	case failed == 0 && skipped == 0:
		return ApplySucceeded
	case succeeded == 0:
		return ApplyFailed
	default:
		return ApplyPartialFailure
	}
}

func emit(ch chan<- RunEvent, e RunEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
	}
}

// bridgeToProvisioner converts the bridge-package types to the provisioner
// public StateSnapshot type. Trivial structural copy.
func bridgeToProvisioner(bs *bridge.Snapshot, deploymentTargetID string) *StateSnapshot {
	out := &StateSnapshot{
		DeploymentTargetID: deploymentTargetID,
		TofuVersion:        bs.TofuVersion,
		SerialNumber:       bs.SerialNumber,
		Outputs:            bs.Outputs,
		CapturedAt:         bs.CapturedAt,
	}
	for _, r := range bs.Resources {
		out.Resources = append(out.Resources, StateResource{
			Address:         r.Address,
			Type:            r.Type,
			Name:            r.Name,
			CloudResourceID: r.CloudResourceID,
			AttributesHash:  r.AttributesHash,
			Attributes:      r.Attributes,
		})
	}
	return out
}

// indexTargetsByIdent builds a lookup map from TargetIdent to TargetPlan.
func indexTargetsByIdent(targets []TargetPlan) map[upstream.TargetIdent]TargetPlan {
	out := map[upstream.TargetIdent]TargetPlan{}
	for _, t := range targets {
		out[upstream.TargetIdent{Component: t.Component, Cloud: t.Cloud, Region: t.Region}] = t
	}
	return out
}

// findComponent returns the ir.Component with the given name, or a zero value.
func findComponent(p *ir.Project, name string) ir.Component {
	for _, c := range p.Components {
		if c.Name == name {
			return c
		}
	}
	return ir.Component{}
}
