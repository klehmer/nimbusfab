package provisioner_test

import (
	"context"
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

func TestDetectDrift_NoDrift(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.DriftPlan = &tofu.PlanArtifact{
		PlanFile:   "/tmp/drift.bin",
		JSONPlan:   []byte(`{"resource_changes":[]}`),
		HasChanges: false,
	}
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-dr",
	})

	rep, err := p.DetectDrift(context.Background(), provisioner.DriftInput{
		PlanResult: planRes, OrgID: "local",
	})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(rep.TargetReports) != 2 {
		t.Fatalf("TargetReports len = %d", len(rep.TargetReports))
	}
	for _, tr := range rep.TargetReports {
		if tr.HasDrift {
			t.Errorf("expected no drift for %s/%s", tr.Cloud, tr.Region)
		}
	}
}

func TestDetectDrift_WithDrift(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.DriftPlan = &tofu.PlanArtifact{
		PlanFile:   "/tmp/drift.bin",
		JSONPlan:   []byte(`{"resource_changes":[{"address":"aws_vpc.web","change":{"actions":["update"]}}]}`),
		HasChanges: true,
	}
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-dr2",
	})
	rep, err := p.DetectDrift(context.Background(), provisioner.DriftInput{
		PlanResult: planRes, OrgID: "local",
	})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	var driftedTargets int
	for _, tr := range rep.TargetReports {
		if tr.HasDrift {
			driftedTargets++
		}
	}
	if driftedTargets != 2 {
		t.Errorf("driftedTargets = %d, want 2", driftedTargets)
	}
}

func TestProvisionerDrift_ForwardTopoAndUsesRealVars(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{
					{Component: "web-network", Output: "subnet_ids", As: "subnetId"},
					{Component: "web-network", Output: "vpc_id", As: "vpcId"},
				},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	workRoot := t.TempDir()
	p, _ := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: reg, Runner: fake})
	planRes, _ := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})

	// Materialize network's state so drift can read real outputs.
	for _, tp := range planRes.Targets {
		if tp.Component == "web-network" {
			_ = os.WriteFile(filepath.Join(tp.WorkspaceDir, "terraform.tfstate"),
				[]byte(`{"version":4,"outputs":{"subnet_ids":{"value":["subnet-x"],"type":["list","string"]},"vpc_id":{"value":"vpc-x","type":"string"},"route_table_ids":{"value":[],"type":["list","string"]}}}`),
				0o600)
		}
	}

	rep, err := p.DetectDrift(ctx, provisioner.DriftInput{
		PlanResult: planRes,
		Project:    project,
	})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	_ = rep

	// web-app drift plan should have received real subnet_ids.
	var appCall *tofu.PlanCall
	for i, pc := range fake.PlanCalls {
		if strings.HasSuffix(pc.Workspace.Dir, "/web-app") && pc.Opts.RefreshOnly {
			appCall = &fake.PlanCalls[i]
		}
	}
	if appCall == nil {
		t.Fatalf("no refresh-only Plan call against web-app")
	}
	if v, _ := appCall.Workspace.Vars["upstream_web_network_subnet_ids"].(string); !strings.Contains(v, "subnet-x") {
		t.Errorf("drift did not use real var: %v", appCall.Workspace.Vars)
	}
}
