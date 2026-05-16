package engine_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestEngine_ApplyWithPlan(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network", Spec: map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	plan, _ := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	res, err := eng.ApplyWithPlan(context.Background(), plan, engine.ApplyOpts{})
	if err != nil {
		t.Fatalf("ApplyWithPlan: %v", err)
	}
	if res.Status != engine.ApplySucceeded {
		t.Errorf("Status = %q", res.Status)
	}
}

func TestEngine_ApplyWithPlan_PlumbsEventSink(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network", Spec: map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	plan, _ := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	sink := make(chan provisioner.RunEvent, 16)
	_, err := eng.ApplyWithPlan(context.Background(), plan, engine.ApplyOpts{EventSink: sink})
	if err != nil {
		t.Fatalf("ApplyWithPlan: %v", err)
	}
	// Drain whatever events landed; we just want to assert >0.
	close(sink)
	count := 0
	for range sink {
		count++
	}
	if count == 0 {
		t.Error("expected at least one RunEvent on the sink")
	}
}

func TestEngine_DestroyWithPlan(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network", Spec: map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	plan, _ := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	res, err := eng.DestroyWithPlan(context.Background(), plan, engine.DestroyOpts{})
	if err != nil {
		t.Fatalf("DestroyWithPlan: %v", err)
	}
	if res.Status != engine.ApplySucceeded {
		t.Errorf("Status = %q", res.Status)
	}
}
