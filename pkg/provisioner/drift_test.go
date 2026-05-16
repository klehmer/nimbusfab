package provisioner_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
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
