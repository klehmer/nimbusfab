package engine_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func mkProject() *ir.Project {
	return &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "demo",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network",
			Spec:    map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
}

func TestEngine_PlanThenApplyByID(t *testing.T) {
	repo, _ := sqlite.Open("sqlite::memory:")
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

	eng, err := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
		InventoryRepo: repo,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plan, err := eng.Plan(context.Background(), mkProject(), "dev", engine.PlanOpts{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.DeploymentID == "" {
		t.Fatal("DeploymentID empty after persistPlan")
	}

	runID, err := eng.Apply(context.Background(), plan.DeploymentID, engine.ApplyOpts{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if runID != plan.DeploymentID {
		t.Errorf("Apply returned %q, want %q", runID, plan.DeploymentID)
	}

	// Verify the deployment row transitioned to a terminal status.
	d, _ := repo.Deployments().Get(context.Background(), "local", plan.DeploymentID)
	if d == nil || d.FinishedAt == nil {
		t.Errorf("deployment not terminal: %+v", d)
	}
}

func TestEngine_ApplyMissingDeployment(t *testing.T) {
	repo, _ := sqlite.Open("sqlite::memory:")
	defer repo.Close()
	_ = repo.Migrate(context.Background())
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: tofu.NewFakeRunner(), WorkRoot: t.TempDir(),
		InventoryRepo: repo,
	})
	_, err := eng.Apply(context.Background(), "nonexistent", engine.ApplyOpts{})
	if err == nil {
		t.Fatal("expected error for missing deployment")
	}
}

func TestEngine_NoInventory_ApplyByIDRejected(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	eng, _ := engine.New(context.Background(), engine.Config{
		CloudAdapters: reg, TofuRunner: tofu.NewFakeRunner(), WorkRoot: t.TempDir(),
	})
	_, err := eng.Apply(context.Background(), "anything", engine.ApplyOpts{})
	if err == nil {
		t.Fatal("expected ErrInventoryRequired")
	}
}
