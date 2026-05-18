package provisioner_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// TestPlan_PopulatesResolvedRefsFromComponentRefs verifies that the
// provisioner translates a component's declared cross-component refs into
// tofu interpolation strings before calling adapter.Emit. The interpolation
// strings reference `var.upstream_<component>_<output>` which are declared as
// `variable` blocks in the workspace and supplied via `tofu plan -var` flags.
//
// Convention: aliases whose snake-case form is the singular of the upstream
// output name (e.g. as=subnetId for output=subnet_ids) get the [0] subscript
// so a scalar consumer (compute.subnet_id) reads the first element. Aliases
// matching the output name unchanged (e.g. as=subnetIds for output=subnet_ids,
// or as=vpcId for output=vpc_id) get the bare interpolation.
func TestPlan_PopulatesResolvedRefsFromComponentRefs(t *testing.T) {
	capa := &captureAdapter{FakeAdapter: cloud.NewFakeAdapter("aws")}
	reg := cloud.NewRegistry()
	if err := reg.Register(capa); err != nil {
		t.Fatalf("register: %v", err)
	}

	p, err := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: reg,
		Runner:   tofu.NewFakeRunner(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{
				Name:    "web-network",
				Type:    "network",
				Spec:    map[string]any{"cidr": "10.0.0.0/16"},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
			},
			{
				Name: "orders-db", Type: "database",
				Spec:    map[string]any{"engine": "postgres"},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
				Refs: []ir.ComponentRef{
					{Component: "web-network", Output: "subnet_ids", As: "subnetIds"},
					{Component: "web-network", Output: "vpc_id", As: "vpcId"},
					{Component: "web-network", Output: "subnet_ids", As: "subnetId"},
				},
			},
		},
	}
	if _, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-t",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := cloud.ResolvedRefs{
		"subnetIds": "${var.upstream_web_network_subnet_ids}",
		"vpcId":     "${var.upstream_web_network_vpc_id}",
		"subnetId":  "${var.upstream_web_network_subnet_ids[0]}",
	}
	if len(capa.capturedRefs) != len(want) {
		t.Fatalf("capturedRefs has %d entries, want %d: %#v", len(capa.capturedRefs), len(want), capa.capturedRefs)
	}
	for k, v := range want {
		got, ok := capa.capturedRefs[k]
		if !ok {
			t.Errorf("capturedRefs missing %q", k)
			continue
		}
		if got != v {
			t.Errorf("capturedRefs[%q] = %v, want %v", k, got, v)
		}
	}
}
