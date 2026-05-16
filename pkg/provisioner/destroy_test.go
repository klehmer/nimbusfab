package provisioner_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
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
