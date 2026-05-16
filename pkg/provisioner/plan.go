package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

func (rp *runtimeProvisioner) Plan(ctx context.Context, in PlanInput) (*PlanResult, error) {
	if in.Project == nil {
		return nil, fmt.Errorf("provisioner.Plan: PlanInput.Project required")
	}
	if in.Stack == "" {
		return nil, fmt.Errorf("provisioner.Plan: PlanInput.Stack required")
	}
	stack, ok := in.Project.Stacks[in.Stack]
	if !ok {
		return nil, fmt.Errorf("provisioner.Plan: stack %q not found in project", in.Stack)
	}
	if in.DeploymentID == "" {
		in.DeploymentID = "dep-" + uuid.NewString()
	}
	if in.PartialFailure == "" {
		in.PartialFailure = PartialFailureLeave
	}

	res := &PlanResult{
		DeploymentID:   in.DeploymentID,
		Stack:          in.Stack,
		PartialFailure: in.PartialFailure,
		GeneratedAt:    time.Now().UTC(),
	}

	for _, comp := range in.Project.Components {
		for _, target := range comp.Targets {
			if !matchesFilter(in.Targets, comp.Name, target.Cloud, target.Region) {
				continue
			}
			tp, err := rp.planOne(ctx, in, stack, comp, target)
			if err != nil {
				return nil, fmt.Errorf("provisioner.Plan: %s/%s/%s: %w",
					comp.Name, target.Cloud, target.Region, err)
			}
			if tp.HasChanges {
				res.HasChanges = true
			}
			res.Targets = append(res.Targets, tp)
		}
	}
	// Aggregate parity reports per component.
	if pEngine, err := parity.NewEngine(); err == nil {
		res.ParityReports = aggregateParityReports(ctx, pEngine, in.Project, res.Targets)
	}
	return res, nil
}

// aggregateParityReports groups TargetPlans by component, picks each target's
// primary profile (matching the component class), and asks the parity engine
// to Compare. Errors from Compare are silently dropped — parity is informative
// and shouldn't fail Plan.
func aggregateParityReports(ctx context.Context, e parity.Engine, project *ir.Project, targets []TargetPlan) []parity.ParityReport {
	byComp := map[string][]TargetPlan{}
	for _, tp := range targets {
		byComp[tp.Component] = append(byComp[tp.Component], tp)
	}
	var out []parity.ParityReport
	for compName, comps := range byComp {
		var compType, size string
		for _, c := range project.Components {
			if c.Name == compName {
				compType = c.Type
				if sz, ok := c.Spec["size"].(string); ok {
					size = sz
				}
				break
			}
		}
		var perTarget []parity.TargetProfile
		for _, tp := range comps {
			if prof, ok := pickPrimaryProfile(tp.PrimitiveProfiles, compType); ok {
				perTarget = append(perTarget, prof)
			}
		}
		if len(perTarget) == 0 {
			continue
		}
		rep, err := e.Compare(ctx, parity.CompareInput{
			Component: compName, Type: compType, Size: size, Targets: perTarget,
		})
		if err == nil && rep != nil {
			out = append(out, *rep)
		}
	}
	return out
}

// pickPrimaryProfile finds the profile whose Class matches the component type
// (e.g., aws_db_instance for a "database" component, not the subnet_group).
func pickPrimaryProfile(profiles []parity.TargetProfile, compType string) (parity.TargetProfile, bool) {
	for _, p := range profiles {
		if p.Profile.Class == compType {
			return p, true
		}
	}
	for _, p := range profiles {
		if p.Profile.Class != "" {
			return p, true
		}
	}
	return parity.TargetProfile{}, false
}

func (rp *runtimeProvisioner) planOne(ctx context.Context, in PlanInput, stack ir.Stack, comp ir.Component, target ir.DeploymentTarget) (TargetPlan, error) {
	adapter, ok := rp.cfg.Adapters.Get(target.Cloud)
	if !ok {
		return TargetPlan{}, fmt.Errorf("no adapter registered for cloud %q", target.Cloud)
	}

	target.Spec = mergeTargetSpec(comp.Spec, target.Spec)
	if target.Spec == nil {
		target.Spec = map[string]any{}
	}
	target.Spec["__component"] = comp.Name
	target.Spec["__type"] = comp.Type

	primitives, err := adapter.Emit(ctx, target, cloud.ResolvedRefs{})
	if err != nil {
		return TargetPlan{}, fmt.Errorf("adapter Emit: %w", err)
	}
	tagCtx := tagContext{Component: comp.Name, DeploymentID: in.DeploymentID, OrgID: in.OrgID}
	for i, p := range primitives {
		primitives[i] = injectFrameworkTags(p, tagCtx)
	}

	// Collect parity profiles per primitive (drop unavailable ones).
	var profiles []parity.TargetProfile
	for _, p := range primitives {
		prof, perr := adapter.Profile(ctx, p)
		if perr != nil {
			continue
		}
		profiles = append(profiles, parity.TargetProfile{
			Cloud:   target.Cloud,
			Region:  target.Region,
			Profile: prof,
		})
	}

	backend := stack.StateBackend
	if backend.Kind == "" {
		backend, err = adapter.DefaultStateBackend(ctx, target)
		if err != nil {
			return TargetPlan{}, fmt.Errorf("DefaultStateBackend: %w", err)
		}
	}

	providerBlock, err := adapter.ProviderBlock(ctx, target, cloud.Credentials{Ref: target.CredentialRef})
	if err != nil {
		return TargetPlan{}, fmt.Errorf("ProviderBlock: %w", err)
	}

	deploymentTargetID := uuid.NewString()
	workspaceDir := filepath.Join(
		rp.cfg.WorkRoot,
		in.DeploymentID,
		fmt.Sprintf("%s-%s", target.Cloud, target.Region),
		comp.Name,
	)

	// Cross-component refs: assume same backend kind/config in Phase 2.
	// Cross-stack refs are v2.
	var upstreamRefs []UpstreamStateRef
	for _, ref := range comp.Refs {
		upstreamRefs = append(upstreamRefs, UpstreamStateRef{
			Component: ref.Component,
			Backend:   backend,
		})
	}

	layout := WorkspaceLayout{
		Dir:            workspaceDir,
		ProviderName:   adapter.Name(),
		ProviderConfig: providerBlock,
		Backend:        backend,
		Primitives:     primitives,
		UpstreamRefs:   upstreamRefs,
	}
	if err := WriteWorkspace(layout); err != nil {
		return TargetPlan{}, fmt.Errorf("WriteWorkspace: %w", err)
	}

	ws := tofu.Workspace{Dir: workspaceDir}
	if err := rp.cfg.Runner.Init(ctx, ws); err != nil {
		return TargetPlan{}, fmt.Errorf("tofu init: %w", err)
	}
	planFile := filepath.Join(workspaceDir, "plan.bin")
	artifact, err := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{
		OutFile: planFile,
		Refresh: in.Refresh,
	})
	if err != nil {
		return TargetPlan{}, fmt.Errorf("tofu plan: %w", err)
	}

	adds, changes, destroys := summarizeJSONPlan(artifact.JSONPlan)

	return TargetPlan{
		DeploymentTargetID: deploymentTargetID,
		Component:          comp.Name,
		Cloud:              target.Cloud,
		Region:             target.Region,
		WorkspaceDir:       workspaceDir,
		PrimitiveCount:     len(primitives),
		PlanFile:           planFile,
		HasChanges:         artifact.HasChanges,
		Adds:               adds,
		Changes:            changes,
		Destroys:           destroys,
		Tags:               frameworkTags(comp.Name, in.DeploymentID, in.OrgID),
		PrimitiveProfiles:  profiles,
		RawPrimitives:      primitives,
	}, nil
}

func matchesFilter(filters []TargetFilter, component, cloudName, region string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if (f.Component == "" || f.Component == component) &&
			(f.Cloud == "" || f.Cloud == cloudName) &&
			(f.Region == "" || f.Region == region) {
			return true
		}
	}
	return false
}

func mergeTargetSpec(base, override map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func frameworkTags(component, deploymentID, orgID string) map[string]string {
	if orgID == "" {
		orgID = "local"
	}
	return map[string]string{
		"infra:component":     component,
		"infra:deployment_id": deploymentID,
		"infra:org_id":        orgID,
	}
}

func summarizeJSONPlan(jsonPlan []byte) (adds, changes, destroys int) {
	var p struct {
		ResourceChanges []struct {
			Change struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	_ = json.Unmarshal(jsonPlan, &p)
	for _, rc := range p.ResourceChanges {
		for _, a := range rc.Change.Actions {
			switch a {
			case "create":
				adds++
			case "update":
				changes++
			case "delete":
				destroys++
			}
		}
	}
	return
}
