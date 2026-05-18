package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error) {
	if in.PlanResult == nil {
		return nil, fmt.Errorf("provisioner.DetectDrift: PlanResult required (Phase 2)")
	}
	if in.Project != nil {
		return rp.detectDriftToposorted(ctx, in)
	}
	return rp.detectDriftExisting(ctx, in)
}

// detectDriftExisting is the original concurrent drift path (no Project provided).
func (rp *runtimeProvisioner) detectDriftExisting(ctx context.Context, in DriftInput) (*DriftReport, error) {
	sems := newSemaphores(resolveCaps(concurrencyCaps{
		Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
	}))
	plan := in.PlanResult

	slots := make([]TargetDriftReport, len(plan.Targets))

	work := func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
		idx := findPlanIndex(plan, comp.Name, t.Cloud, t.Region)
		tp := plan.Targets[idx]
		env, envErr := resolveEnvFor(ctx, rp.cfg.SecretsBackend, tp.CredentialRef)
		if envErr != nil {
			slots[idx] = TargetDriftReport{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
				Error: envErr,
			}
			return TargetApplyResult{}
		}
		ws := tofu.Workspace{Dir: tp.WorkspaceDir, Environment: env}
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

// detectDriftToposorted runs drift detection in forward dependency order.
// For each target, it attempts to read real upstream output values from state
// files.  If any upstream state is unavailable the target is marked Skipped
// so callers can distinguish "could not check" from "no drift".
func (rp *runtimeProvisioner) detectDriftToposorted(ctx context.Context, in DriftInput) (*DriftReport, error) {
	plan := in.PlanResult

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
		return nil, fmt.Errorf("provisioner.DetectDrift: toposort: %w", err)
	}

	byIdent := indexTargetsByIdent(plan.Targets)

	var reports []TargetDriftReport

	for _, ident := range ordered {
		tp, ok := byIdent[ident]
		if !ok {
			continue
		}

		comp := findComponent(in.Project, ident.Component)

		// Attempt to build real vars from upstream state files.
		// If any upstream state is missing, mark as skipped.
		vars, skipped := buildDriftVars(comp, ident, byIdent)
		if skipped {
			reports = append(reports, TargetDriftReport{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Skipped:            true,
			})
			continue
		}

		ws := tofu.Workspace{Dir: tp.WorkspaceDir, Vars: vars}
		driftFile := filepath.Join(tp.WorkspaceDir, "drift.bin")
		artifact, planErr := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{
			OutFile:     driftFile,
			RefreshOnly: true,
		})
		if planErr != nil {
			reports = append(reports, TargetDriftReport{
				DeploymentTargetID: tp.DeploymentTargetID,
				Component:          ident.Component,
				Cloud:              ident.Cloud,
				Region:             ident.Region,
				Error:              planErr,
			})
			continue
		}

		rep := TargetDriftReport{
			DeploymentTargetID: tp.DeploymentTargetID,
			Component:          ident.Component,
			Cloud:              ident.Cloud,
			Region:             ident.Region,
			HasDrift:           artifact.HasChanges,
		}
		rep.Drifted, rep.Gone, rep.Discovered = extractDrift(artifact.JSONPlan)
		reports = append(reports, rep)
	}

	return &DriftReport{
		DeploymentID:  plan.DeploymentID,
		GeneratedAt:   time.Now().UTC(),
		TargetReports: reports,
	}, nil
}

// buildDriftVars reads upstream state files and returns a vars map with real
// output values for each ref on comp.  If any upstream state is unavailable
// it returns (nil, true) signalling the caller should skip this target.
func buildDriftVars(
	comp ir.Component,
	ident upstream.TargetIdent,
	byIdent map[upstream.TargetIdent]TargetPlan,
) (vars map[string]any, skipped bool) {
	if len(comp.Refs) == 0 {
		return nil, false
	}
	vars = map[string]any{}
	for _, ref := range comp.Refs {
		upIdent := upstream.TargetIdent{
			Component: ref.Component,
			Cloud:     ident.Cloud,
			Region:    ident.Region,
		}
		upTP, ok := byIdent[upIdent]
		if !ok {
			return nil, true
		}
		stateBytes, readErr := os.ReadFile(filepath.Join(upTP.WorkspaceDir, "terraform.tfstate"))
		if readErr != nil {
			return nil, true
		}
		outputs, parseErr := upstream.ExtractOutputs(stateBytes)
		if parseErr != nil {
			return nil, true
		}
		val, exists := outputs[ref.Output]
		if !exists {
			return nil, true
		}
		hcl, fmtErr := upstream.FormatHCLValue(val)
		if fmtErr != nil {
			return nil, true
		}
		vars[upstream.VarName(ref.Component, ref.Output)] = hcl
	}
	return vars, false
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
