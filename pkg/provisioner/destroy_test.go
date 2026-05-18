package provisioner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestDestroy_HappyPath(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-d",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	res, err := p.Destroy(context.Background(), provisioner.DestroyInput{
		PlanResult: planRes, OrgID: "local",
	})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if res.Status != provisioner.ApplySucceeded {
		t.Errorf("Status = %q, want succeeded", res.Status)
	}
	if len(runner.DestroyCalls) != 2 {
		t.Errorf("Destroy calls = %d, want 2", len(runner.DestroyCalls))
	}
}

func TestProvisionerDestroy_ReverseTopoOrder(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake})
	planRes, _ := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})

	// Provide Project to opt into the new toposort-aware destroy path.
	_, err := p.Destroy(ctx, provisioner.DestroyInput{
		PlanResult: planRes,
		Project:    project,
	})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	// Destroy order: app first, then network.
	if len(fake.DestroyCalls) < 2 {
		t.Fatalf("got %d destroy calls", len(fake.DestroyCalls))
	}
	firstDir := fake.DestroyCalls[0].Dir
	lastDir := fake.DestroyCalls[len(fake.DestroyCalls)-1].Dir
	if !strings.HasSuffix(firstDir, "/web-app") || !strings.HasSuffix(lastDir, "/web-network") {
		t.Errorf("destroy order: first=%s last=%s (want web-app first, web-network last)", firstDir, lastDir)
	}
}
