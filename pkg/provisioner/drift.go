package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error) {
	if in.PlanResult == nil {
		return nil, fmt.Errorf("provisioner.DetectDrift: PlanResult required (Phase 2)")
	}
	sems := newSemaphores(resolveCaps(concurrencyCaps{
		Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
	}))
	plan := in.PlanResult

	slots := make([]TargetDriftReport, len(plan.Targets))

	work := func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
		idx := findPlanIndex(plan, comp.Name, t.Cloud, t.Region)
		tp := plan.Targets[idx]
		ws := tofu.Workspace{Dir: tp.WorkspaceDir}
		driftFile := filepath.Join(tp.WorkspaceDir, "drift.bin")
		artifact, err := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{
			OutFile:     driftFile,
			RefreshOnly: true,
		})
		if err != nil {
			slots[idx] = TargetDriftReport{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
				Error: err,
			}
			return TargetApplyResult{}
		}
		rep := TargetDriftReport{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
			HasDrift: artifact.HasChanges,
		}
		rep.Drifted, rep.Gone, rep.Discovered = extractDrift(artifact.JSONPlan)
		slots[idx] = rep
		return TargetApplyResult{}
	}

	componentsOrdered := componentsFromPlan(plan)
	for _, comp := range componentsOrdered {
		_ = runComponent(ctx, comp, sems, work)
	}

	return &DriftReport{
		DeploymentID:  plan.DeploymentID,
		GeneratedAt:   time.Now().UTC(),
		TargetReports: slots,
	}, nil
}

func findPlanIndex(plan *PlanResult, component, cloud, region string) int {
	for i, tp := range plan.Targets {
		if tp.Component == component && tp.Cloud == cloud && tp.Region == region {
			return i
		}
	}
	return -1
}

func extractDrift(jsonPlan []byte) (drifted, gone, discovered []DriftedResource) {
	var p struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string       `json:"actions"`
				Before  map[string]any `json:"before"`
				After   map[string]any `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	_ = json.Unmarshal(jsonPlan, &p)
	for _, rc := range p.ResourceChanges {
		kind := "drift"
		if len(rc.Change.Actions) > 0 {
			switch rc.Change.Actions[0] {
			case "delete":
				kind = "gone"
			case "create":
				kind = "discovered"
			}
		}
		dr := DriftedResource{
			Address:          rc.Address,
			Kind:             kind,
			AttributesBefore: rc.Change.Before,
			AttributesAfter:  rc.Change.After,
			DiffSummary:      fmt.Sprintf("%v", rc.Change.Actions),
		}
		switch kind {
		case "gone":
			gone = append(gone, dr)
		case "discovered":
			discovered = append(discovered, dr)
		default:
			drifted = append(drifted, dr)
		}
	}
	return
}
