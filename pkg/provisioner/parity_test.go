package provisioner_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestPlan_AggregatesParityReports(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: reg,
		Runner:   tofu.NewFakeRunner(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{
				Name: "net", Type: "network",
				Spec:    map[string]any{"cidr": "10.0.0.0/16"},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
			},
			{
				Name: "web", Type: "compute",
				Spec: map[string]any{"size": "small"},
				Refs: []ir.ComponentRef{
					{Component: "net", Output: "subnet_ids", As: "subnetId"},
					{Component: "net", Output: "vpc_id", As: "vpcId"},
				},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
			},
		},
	}
	res, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-p",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.ParityReports) == 0 {
		t.Fatalf("ParityReports len = 0, want >=1")
	}
	var rep *parity.ParityReport
	for i := range res.ParityReports {
		if res.ParityReports[i].Component == "web" {
			rep = &res.ParityReports[i]
		}
	}
	if rep == nil {
		t.Fatalf("no parity report for web component; got %+v", res.ParityReports)
	}
	if rep.Type != "compute" {
		t.Errorf("report type: %+v", rep)
	}
	if rep.Score < 0.99 {
		t.Errorf("score = %f, want ~1.0 for single target", rep.Score)
	}
}
