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

func TestPlan_SingleAWSNetworkTarget(t *testing.T) {
	workRoot := t.TempDir()
	fakeRunner := tofu.NewFakeRunner()
	fakeRunner.PlanFileContents = []byte("FAKE-PLAN-BIN")

	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		t.Fatalf("register aws: %v", err)
	}

	p, err := provisioner.New(provisioner.Config{
		WorkRoot: workRoot,
		Adapters: reg,
		Runner:   fakeRunner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "test-project",
		Stacks: map[string]ir.Stack{
			"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}},
		},
		Components: []ir.Component{{
			Name: "web-network",
			Type: "network",
			Spec: map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{
				Cloud:  "aws",
				Region: "us-east-1",
				Spec:   map[string]any{"cidr": "10.0.0.0/16"},
			}},
		}},
	}

	res, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project:      project,
		Stack:        "dev",
		OrgID:        "local",
		DeploymentID: "dep-test",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Targets) != 1 {
		t.Fatalf("Targets len = %d, want 1", len(res.Targets))
	}
	tp := res.Targets[0]
	if tp.Component != "web-network" || tp.Cloud != "aws" || tp.Region != "us-east-1" {
		t.Errorf("target identity wrong: %+v", tp)
	}
	// Phase 3: network emit produces VPC + IGW + RT + subnets + RTAs.
	if tp.PrimitiveCount < 1 {
		t.Errorf("PrimitiveCount = %d, want >=1", tp.PrimitiveCount)
	}
	for _, f := range []string{"main.tf.json", "provider.tf.json", "backend.tf.json", "versions.tf.json"} {
		if _, err := os.Stat(filepath.Join(tp.WorkspaceDir, f)); err != nil {
			t.Errorf("missing workspace file %s: %v", f, err)
		}
	}
	if len(fakeRunner.InitCalls) != 1 {
		t.Errorf("Init calls = %d, want 1", len(fakeRunner.InitCalls))
	}
	if len(fakeRunner.PlanCalls) != 1 {
		t.Errorf("Plan calls = %d, want 1", len(fakeRunner.PlanCalls))
	}
	if !strings.HasPrefix(tp.PlanFile, tp.WorkspaceDir) {
		t.Errorf("PlanFile %q not under workspace %q", tp.PlanFile, tp.WorkspaceDir)
	}
}

func TestPlan_UnknownAdapterFails(t *testing.T) {
	p, err := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: cloud.NewRegistry(),
		Runner:   tofu.NewFakeRunner(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Plan(context.Background(), provisioner.PlanInput{
		Project: &ir.Project{
			APIVersion: ir.APIVersionV1Alpha1,
			Name:       "x",
			Stacks:     map[string]ir.Stack{"dev": {Name: "dev"}},
			Components: []ir.Component{{
				Name: "n", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
			}},
		},
		Stack:        "dev",
		OrgID:        "local",
		DeploymentID: "dep-x",
	})
	if err == nil {
		t.Fatal("Plan: nil err, want adapter-unknown error")
	}
}

func TestPlan_TargetFilter(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: reg,
		Runner:   tofu.NewFakeRunner(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "x",
		Stacks:     map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "a", Type: "network", Spec: map[string]any{}, Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
			{Name: "b", Type: "network", Spec: map[string]any{}, Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		},
	}
	res, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project:      project,
		Stack:        "dev",
		OrgID:        "local",
		DeploymentID: "dep-x",
		Targets:      []provisioner.TargetFilter{{Component: "b"}},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Targets) != 1 || res.Targets[0].Component != "b" {
		t.Errorf("filter not applied: %+v", res.Targets)
	}
}
