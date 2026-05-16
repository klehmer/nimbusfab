package provisioner_test

import (
	"context"
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
