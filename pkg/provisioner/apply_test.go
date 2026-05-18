package provisioner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestApply_SingleTargetHappyPath(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	fakeRunner := tofu.NewFakeRunner()
	fakeRunner.PlanFileContents = []byte("FAKE-PLAN")
	fakeRunner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0","serial":1,"values":{"outputs":{"vpc_id":{"value":"vpc-0xyz","type":"string"}},"root_module":{"resources":[{"address":"aws_vpc.web","type":"aws_vpc","name":"web","values":{"id":"vpc-0xyz","cidr_block":"10.0.0.0/16"}}]}}}`)
	fakeRunner.OutputReturn = map[string]any{"vpc_id": "vpc-0xyz"}

	p, err := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: reg,
		Runner:   fakeRunner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "x",
		Stacks:     map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network",
			Spec:    map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}

	planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-t",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	applyRes, err := p.Apply(context.Background(), provisioner.ApplyInput{
		PlanResult: planRes,
		OrgID:      "local",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applyRes.Status != provisioner.ApplySucceeded {
		t.Errorf("Status = %q, want succeeded", applyRes.Status)
	}
	if len(applyRes.TargetResults) != 1 {
		t.Fatalf("TargetResults len = %d", len(applyRes.TargetResults))
	}
	tr := applyRes.TargetResults[0]
	if tr.Status != provisioner.RunStatusSucceeded {
		t.Errorf("target status = %q", tr.Status)
	}
	if tr.Outputs["vpc_id"] != "vpc-0xyz" {
		t.Errorf("outputs not captured: %v", tr.Outputs)
	}
	if tr.State == nil || len(tr.State.Resources) != 1 {
		t.Errorf("state not captured: %+v", tr.State)
	}
	if len(fakeRunner.ApplyCalls) != 1 {
		t.Errorf("Apply runner calls = %d, want 1", len(fakeRunner.ApplyCalls))
	}
}

// TestProvisionerApply_TopoOrderAndRealVarsRebound verifies that when Apply is
// given a Project, it:
//  1. Applies upstream (web-network) before downstream (web-app).
//  2. Re-plans the downstream target with real output values extracted from the
//     upstream terraform.tfstate written by the OnApply hook.
func TestProvisionerApply_TopoOrderAndRealVarsRebound(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{
					{Component: "web-network", Output: "subnet_ids", As: "subnetId"},
					{Component: "web-network", Output: "vpc_id", As: "vpcId"},
				},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}

	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	// After network applies, drop a state file so app's apply can ExtractOutputs.
	fake.OnApply = func(ws tofu.Workspace, planFile string) {
		if strings.HasSuffix(ws.Dir, "/web-network") {
			state := `{"version":4,"outputs":{"subnet_ids":{"value":["subnet-real"],"type":["list","string"]},"vpc_id":{"value":"vpc-real","type":"string"},"route_table_ids":{"value":[],"type":["list","string"]}}}`
			_ = os.WriteFile(filepath.Join(ws.Dir, "terraform.tfstate"), []byte(state), 0o600)
		}
	}

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	workRoot := t.TempDir()
	p, err := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: reg, Runner: fake})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	planRes, err := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := p.Apply(ctx, provisioner.ApplyInput{
		PlanResult: planRes,
		Project:    project,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	netIdx, appIdx := -1, -1
	for i, ac := range fake.ApplyCalls {
		switch {
		case strings.HasSuffix(ac.Workspace.Dir, "/web-network"):
			netIdx = i
		case strings.HasSuffix(ac.Workspace.Dir, "/web-app"):
			appIdx = i
		}
	}
	if netIdx == -1 || appIdx == -1 || netIdx >= appIdx {
		t.Fatalf("ordering: netIdx=%d appIdx=%d (want network before app)", netIdx, appIdx)
	}

	// The compute target's re-plan call must have received the REAL subnet_ids
	// value, not a placeholder.
	var rebindCall *tofu.PlanCall
	for i, pc := range fake.PlanCalls {
		if strings.HasSuffix(pc.Workspace.Dir, "/web-app") {
			rebindCall = &fake.PlanCalls[i]
		}
	}
	if rebindCall == nil {
		t.Fatalf("no Plan call against web-app workspace")
	}
	v, _ := rebindCall.Workspace.Vars["upstream_web_network_subnet_ids"].(string)
	if !strings.Contains(v, "subnet-real") {
		t.Errorf("re-plan did not get real value: got %q", v)
	}
}

// TestProvisionerApply_DownstreamBlockedOnUpstreamFailure verifies that when
// an upstream target fails, all downstream dependents are marked
// RunStatusBlocked without being attempted.
func TestProvisionerApply_DownstreamBlockedOnUpstreamFailure(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{
					{Component: "web-network", Output: "subnet_ids", As: "subnetId"},
					{Component: "web-network", Output: "vpc_id", As: "vpcId"},
				},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	fake.ApplyError = errors.New("simulated apply failure")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, err := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	planRes, err := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	appRes, err := p.Apply(ctx, provisioner.ApplyInput{
		PlanResult:     planRes,
		Project:        project,
		PartialFailure: provisioner.PartialFailureLeave,
	})
	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}

	var appStatus provisioner.RunStatus
	for _, tr := range appRes.TargetResults {
		if tr.Component == "web-app" {
			appStatus = tr.Status
		}
	}
	if appStatus != provisioner.RunStatusBlocked {
		t.Errorf("expected web-app blocked, got %q", appStatus)
	}

	// web-app should not have been attempted (no Apply call for its workspace).
	for _, ac := range fake.ApplyCalls {
		if strings.HasSuffix(ac.Workspace.Dir, "/web-app") {
			t.Errorf("web-app should not have been applied when upstream failed")
		}
	}
}
