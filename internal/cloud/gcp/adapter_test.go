package gcp_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_NameAndSupport(t *testing.T) {
	a := gcp.New()
	if a.Name() != "gcp" {
		t.Errorf("Name = %q, want gcp", a.Name())
	}
	if got := len(a.SupportedComponentTypes()); got != 4 {
		t.Errorf("SupportedComponentTypes = %d entries, want 4", got)
	}
	if len(a.SupportedAPIVersions()) == 0 {
		t.Error("SupportedAPIVersions must be non-empty")
	}
	if len(a.TierOneSchema()) == 0 {
		t.Error("TierOneSchema must be non-empty")
	}
}

func TestAdapter_Validate(t *testing.T) {
	a := gcp.New()
	cases := []struct {
		name   string
		region string
		want   bool // true => expect issues
	}{
		{"empty", "", true},
		{"aws-style", "us-east-1", true},
		{"azure-style", "eastus", true},
		{"valid-us", "us-central1", false},
		{"valid-eu", "europe-west1", false},
		{"valid-asia", "asia-east1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := a.Validate(context.Background(), ir.DeploymentTarget{Region: tc.region})
			gotIssues := len(issues) > 0
			if gotIssues != tc.want {
				t.Errorf("Validate(%q): gotIssues=%v want=%v (issues=%v)", tc.region, gotIssues, tc.want, issues)
			}
		})
	}
}

func TestAdapter_DefaultStateBackend(t *testing.T) {
	a := gcp.New()
	sb, err := a.DefaultStateBackend(context.Background(), ir.DeploymentTarget{Region: "us-central1"})
	if err != nil {
		t.Fatalf("DefaultStateBackend: %v", err)
	}
	if sb.Kind != "gcs" {
		t.Errorf("Kind = %q, want gcs", sb.Kind)
	}
	if sb.Config["bucket"] != "nimbusfab-state" {
		t.Errorf("bucket = %v", sb.Config["bucket"])
	}
	if sb.Config["prefix"] != "gcp/us-central1" {
		t.Errorf("prefix = %v", sb.Config["prefix"])
	}
}

func TestAdapter_ProviderBlock(t *testing.T) {
	a := gcp.New()
	pb, _ := a.ProviderBlock(context.Background(), ir.DeploymentTarget{Region: "us-central1"}, cloud.Credentials{})
	g, ok := pb["google"].(map[string]any)
	if !ok {
		t.Fatalf("missing google block: %v", pb)
	}
	if g["region"] != "us-central1" {
		t.Errorf("region = %v", g["region"])
	}
	if _, has := g["project"]; has {
		t.Errorf("unexpected project key with no spec.project: %v", g)
	}
}

func TestAdapter_ProviderBlock_ProjectFromSpec(t *testing.T) {
	a := gcp.New()
	pb, _ := a.ProviderBlock(context.Background(), ir.DeploymentTarget{
		Region: "us-central1",
		Spec:   map[string]any{"project": "my-project"},
	}, cloud.Credentials{})
	g := pb["google"].(map[string]any)
	if g["project"] != "my-project" {
		t.Errorf("project = %v, want my-project", g["project"])
	}
}

func TestAdapter_Emit_UnsupportedType(t *testing.T) {
	a := gcp.New()
	_, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "exotic"},
	}, cloud.ResolvedRefs{})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}
