package engine_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEngine_Plan_OneAWSNetwork(t *testing.T) {
	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		t.Fatalf("register aws: %v", err)
	}
	fakeRunner := tofu.NewFakeRunner()

	eng, err := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg,
		TofuRunner:    fakeRunner,
		WorkRoot:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "x",
		Stacks: map[string]ir.Stack{
			"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}},
		},
		Components: []ir.Component{{
			Name: "web", Type: "network",
			Spec:    map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	res, err := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Targets) != 1 || res.Targets[0].PrimitiveCount != 1 {
		t.Fatalf("unexpected PlanResult shape: %+v", res)
	}
}

func TestEngine_New_RequiresAdapters(t *testing.T) {
	_, err := engine.New(context.Background(), engine.Config{})
	if err == nil {
		t.Fatal("New(no Adapters): nil err, want non-nil")
	}
}
