package loader

import (
	"context"
	"os"
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

func TestLoad_MultiFile(t *testing.T) {
	proj, err := New().Load(context.Background(), "testdata/multi-file")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if proj.Name != "orders-multi" {
		t.Errorf("Name = %q", proj.Name)
	}
	if len(proj.Components) != 2 {
		t.Fatalf("Components = %d, want 2", len(proj.Components))
	}
	names := map[string]bool{}
	for _, c := range proj.Components {
		names[c.Name] = true
	}
	if !names["web-network"] || !names["orders-db"] {
		t.Errorf("missing expected components: %v", names)
	}
	if len(proj.Comps) != 1 {
		t.Fatalf("Compositions = %d, want 1", len(proj.Comps))
	}
	if proj.Comps[0].Kind != "TunedPostgres" {
		t.Errorf("Composition kind = %q", proj.Comps[0].Kind)
	}
}

func TestLoad_DuplicateComponent(t *testing.T) {
	// Same component name appearing in both project.yaml inline and components/*.yaml.
	dir := t.TempDir()
	mustWrite(t, dir+"/project.yaml", []byte(`
apiVersion: infra.dev/v1alpha1
name: dup
stacks:
  dev: {}
components:
  - name: same
    type: network
    spec: { cidrBlock: 10.0.0.0/16 }
    targets:
      - cloud: aws
        region: us-east-1
        credentialRef: aws-dev
`))
	mustMkdir(t, dir+"/components")
	mustWrite(t, dir+"/components/same.yaml", []byte(`
apiVersion: infra.dev/v1alpha1
name: same
type: network
spec: { cidrBlock: 10.1.0.0/16 }
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
`))
	_, err := New().Load(context.Background(), dir)
	if err == nil {
		t.Fatal("expected duplicate-component error, got nil")
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
