package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/state/bridge"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
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
	return &ApplyResult{
		DeploymentID:  plan.DeploymentID,
		Status:        summarizeApplyStatus(results),
		TargetResults: results,
		GeneratedAt:   time.Now().UTC(),
	}, nil
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
		ws := tofu.Workspace{Dir: tp.WorkspaceDir}
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
		case RunStatusSkipped:
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
