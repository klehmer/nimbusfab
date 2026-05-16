package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEngine_EstimateCost_OneInstance(t *testing.T) {
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
			Name: "web", Type: "compute",
			Spec: map[string]any{"size": "small"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	plan, err := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	est, err := eng.EstimateCost(context.Background(), plan)
	if err != nil {
		t.Fatalf("EstimateCost: %v", err)
	}
	if est.Total <= 0 {
		t.Errorf("Total = %v, want > 0", est.Total)
	}
	if est.Currency != "USD" {
		t.Errorf("Currency = %q", est.Currency)
	}
	// t3.small @ $0.0208/hr × 730 ≈ $15.18/month
	if est.Total < 14 || est.Total > 17 {
		t.Errorf("Total = %v, want ~$15.18/month for t3.small", est.Total)
	}
}

func TestEngine_EstimateCost_FullStack(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web", Type: "compute", Spec: map[string]any{"size": "small"},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
			{Name: "db", Type: "database", Spec: map[string]any{"engine": "postgres", "size": "small"},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
			{Name: "uploads", Type: "storage", Spec: map[string]any{},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		},
	}
	plan, _ := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	est, _ := eng.EstimateCost(context.Background(), plan)
	if est.Total <= 0 {
		t.Errorf("expected non-zero total, got %v (warnings: %v)", est.Total, est.Warnings)
	}
	// Each of compute / database / storage should contribute.
	var hasCompute, hasDB, hasStorage bool
	for _, tgt := range est.Targets {
		for _, p := range tgt.Primitives {
			if strings.Contains(p.PrimitiveID, "instance_0") {
				hasCompute = true
			}
			if strings.Contains(p.PrimitiveID, ".db") {
				hasDB = true
			}
			if strings.Contains(p.PrimitiveID, ".bucket") {
				hasStorage = true
			}
		}
	}
	if !hasCompute || !hasDB || !hasStorage {
		t.Errorf("expected primitives for all three component types: compute=%v db=%v storage=%v",
			hasCompute, hasDB, hasStorage)
	}
}
