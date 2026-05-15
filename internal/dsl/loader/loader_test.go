package loader

import (
	"context"
	"testing"
)

func TestLoad_SingleFile(t *testing.T) {
	ctx := context.Background()
	proj, err := New().Load(ctx, "testdata/single-file")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if proj.APIVersion != "infra.dev/v1alpha1" {
		t.Errorf("APIVersion = %q", proj.APIVersion)
	}
	if proj.Name != "orders" {
		t.Errorf("Name = %q", proj.Name)
	}
	if len(proj.Stacks) != 2 {
		t.Errorf("Stacks count = %d, want 2", len(proj.Stacks))
	}
	if len(proj.Components) != 1 {
		t.Fatalf("Components count = %d, want 1", len(proj.Components))
	}
	if proj.Components[0].Name != "web-network" {
		t.Errorf("Component[0].Name = %q", proj.Components[0].Name)
	}
}

func TestLoad_MissingProjectYAML(t *testing.T) {
	_, err := New().Load(context.Background(), "testdata/does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing project.yaml, got nil")
	}
}
