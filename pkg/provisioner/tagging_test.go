package provisioner

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestInjectFrameworkTags_AddsAllThree(t *testing.T) {
	p := ir.ResourcePrimitive{
		ID:         "web.aws-us-east-1.vpc",
		Cloud:      "aws",
		TofuType:   "aws_vpc",
		TofuName:   "web",
		Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
		Tags:       map[string]string{"Owner": "data-team"},
	}
	ctx := tagContext{Component: "web", DeploymentID: "dep-123", OrgID: "org-abc"}
	out := injectFrameworkTags(p, ctx)
	for _, k := range []string{"infra:component", "infra:deployment_id", "infra:org_id"} {
		if _, ok := out.Tags[k]; !ok {
			t.Errorf("missing required tag %q", k)
		}
	}
	if out.Tags["Owner"] != "data-team" {
		t.Errorf("user tag clobbered: Owner=%q", out.Tags["Owner"])
	}
	if out.Tags["infra:component"] != "web" {
		t.Errorf("infra:component = %q, want \"web\"", out.Tags["infra:component"])
	}
	if out.Tags["infra:org_id"] != "org-abc" {
		t.Errorf("infra:org_id = %q, want \"org-abc\"", out.Tags["infra:org_id"])
	}
}

func TestInjectFrameworkTags_DefaultOrgID(t *testing.T) {
	p := ir.ResourcePrimitive{Tags: map[string]string{}}
	out := injectFrameworkTags(p, tagContext{Component: "x", DeploymentID: "y"})
	if out.Tags["infra:org_id"] != "local" {
		t.Errorf("default org_id = %q, want \"local\"", out.Tags["infra:org_id"])
	}
}

func TestInjectFrameworkTags_DoesNotMutateInput(t *testing.T) {
	original := map[string]string{"X": "y"}
	p := ir.ResourcePrimitive{Tags: original}
	_ = injectFrameworkTags(p, tagContext{Component: "c", DeploymentID: "d", OrgID: "o"})
	if _, hasInfra := original["infra:component"]; hasInfra {
		t.Error("input map mutated")
	}
}
