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
	tags, ok := out.Attributes["tags"].(map[string]string)
	if !ok {
		t.Fatalf("expected tags map in Attributes; got %T", out.Attributes["tags"])
	}
	for _, k := range []string{"infra:component", "infra:deployment_id", "infra:org_id"} {
		if _, ok := tags[k]; !ok {
			t.Errorf("missing required tag %q", k)
		}
	}
	if tags["Owner"] != "data-team" {
		t.Errorf("user tag clobbered: Owner=%q", tags["Owner"])
	}
	if tags["infra:component"] != "web" {
		t.Errorf("infra:component = %q, want \"web\"", tags["infra:component"])
	}
	if tags["infra:org_id"] != "org-abc" {
		t.Errorf("infra:org_id = %q, want \"org-abc\"", tags["infra:org_id"])
	}
}

func TestInjectFrameworkTags_DefaultOrgID(t *testing.T) {
	p := ir.ResourcePrimitive{Cloud: "aws", Tags: map[string]string{}}
	out := injectFrameworkTags(p, tagContext{Component: "x", DeploymentID: "y"})
	tags, ok := out.Attributes["tags"].(map[string]string)
	if !ok {
		t.Fatalf("expected tags map in Attributes; got %T", out.Attributes["tags"])
	}
	if tags["infra:org_id"] != "local" {
		t.Errorf("default org_id = %q, want \"local\"", tags["infra:org_id"])
	}
}

func TestInjectFrameworkTags_DoesNotMutateInput(t *testing.T) {
	original := map[string]string{"X": "y"}
	p := ir.ResourcePrimitive{Cloud: "aws", Tags: original}
	_ = injectFrameworkTags(p, tagContext{Component: "c", DeploymentID: "d", OrgID: "o"})
	if _, hasInfra := original["infra:component"]; hasInfra {
		t.Error("input map mutated")
	}
}

func TestInjectFrameworkTags_AWSDefault(t *testing.T) {
	p := ir.ResourcePrimitive{Cloud: "aws", TofuType: "aws_vpc", TofuName: "net",
		Attributes: map[string]any{}}
	ctx := tagContext{Component: "web", DeploymentID: "dep-1", OrgID: "org-1"}
	out := injectFrameworkTags(p, ctx)
	tags, ok := out.Attributes["tags"].(map[string]string)
	if !ok {
		t.Fatalf("expected tags map; got %T: %v", out.Attributes["tags"], out.Attributes["tags"])
	}
	if tags["infra:component"] != "web" || tags["infra:deployment_id"] != "dep-1" {
		t.Errorf("tags missing framework fields: %+v", tags)
	}
}

func TestInjectFrameworkTags_GCPSkipDefault(t *testing.T) {
	p := ir.ResourcePrimitive{Cloud: "gcp", TofuType: "google_compute_network",
		Attributes: map[string]any{}}
	ctx := tagContext{Component: "web", DeploymentID: "dep-1", OrgID: "org-1"}
	out := injectFrameworkTags(p, ctx)
	if _, present := out.Attributes["tags"]; present {
		t.Error("GCP default should NOT emit tags attribute")
	}
	if _, present := out.Attributes["labels"]; present {
		t.Error("GCP default should NOT emit labels attribute (requires explicit opt-in)")
	}
}

func TestInjectFrameworkTags_GCPLabels(t *testing.T) {
	p := ir.ResourcePrimitive{Cloud: "gcp", TofuType: "google_compute_instance",
		TagAttribute: "labels", Attributes: map[string]any{}}
	ctx := tagContext{Component: "web", DeploymentID: "dep-1", OrgID: "org-1"}
	out := injectFrameworkTags(p, ctx)
	labels, ok := out.Attributes["labels"].(map[string]string)
	if !ok {
		t.Fatalf("expected labels map; got %T", out.Attributes["labels"])
	}
	// GCP label keys: lowercase [a-z0-9_-]; ":" must become "_".
	if _, hasInfraComponent := labels["infra_component"]; !hasInfraComponent {
		t.Errorf("expected sanitized key infra_component; got %v", labels)
	}
	if v := labels["infra_component"]; v != "web" {
		t.Errorf("infra_component=%q want web", v)
	}
}

func TestInjectFrameworkTags_AzureDefault(t *testing.T) {
	p := ir.ResourcePrimitive{Cloud: "azure", TofuType: "azurerm_virtual_network",
		Attributes: map[string]any{}}
	ctx := tagContext{Component: "web", DeploymentID: "dep-1", OrgID: "org-1"}
	out := injectFrameworkTags(p, ctx)
	if _, ok := out.Attributes["tags"].(map[string]string); !ok {
		t.Error("Azure default should emit tags attribute")
	}
}

func TestSanitizeForLabels(t *testing.T) {
	in := map[string]string{
		"infra:component":     "Web-App",
		"infra:deployment_id": "dep-abc-123",
		"infra:org_id":        "org-XYZ",
	}
	got := sanitizeForLabels(in)
	if got["infra_component"] != "web-app" {
		t.Errorf("component=%q", got["infra_component"])
	}
	if got["infra_org_id"] != "org-xyz" {
		t.Errorf("org_id=%q", got["infra_org_id"])
	}
}
