package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_HappyPath(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout missing OK marker: %s", stdout.String())
	}
}

func TestValidate_RejectsBadAPIVersion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v999
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "ErrUnknownAPIVersion") {
		t.Errorf("stderr missing ErrUnknownAPIVersion: %s", stderr.String())
	}
}

func TestValidate_RejectsBadName(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: "Has Spaces"
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "ErrSchema") {
		t.Errorf("output missing ErrSchema: %s", combined)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestValidate_ExitCodes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	if exit := runValidate(&stdout, &stderr, []string{root}); exit != 0 {
		t.Errorf("clean project exit = %d, want 0", exit)
	}

	// Make project bad: missing stacks.
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
`))
	stdout.Reset()
	stderr.Reset()
	if exit := runValidate(&stdout, &stderr, []string{root}); exit == 0 {
		t.Error("missing stacks should fail validation")
	}
}

// Phase 4 fixtures: each writes a project with one malformed component and
// asserts the expected error code surfaces in CLI output.

func writePhase4Project(t *testing.T, root, componentYAML string) {
	t.Helper()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`))
	if err := os.MkdirAll(filepath.Join(root, "components"), 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	mustWrite(t, filepath.Join(root, "components", "c.yaml"), []byte(componentYAML))
}

func TestValidate_Phase4_MissingRequiredField(t *testing.T) {
	root := t.TempDir()
	writePhase4Project(t, root, `
apiVersion: infra.dev/v1alpha1
name: web
type: network
spec:
  subnetCount: 2
targets:
  - cloud: aws
    region: us-east-1
`)
	var stdout, stderr bytes.Buffer
	if exit := runValidate(&stdout, &stderr, []string{root}); exit == 0 {
		t.Fatal("expected non-zero exit for missing-cidr")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "ErrValidatorTypeSpec") {
		t.Errorf("output missing ErrValidatorTypeSpec: %s", combined)
	}
}

func TestValidate_Phase4_UnknownType(t *testing.T) {
	root := t.TempDir()
	writePhase4Project(t, root, `
apiVersion: infra.dev/v1alpha1
name: misnamed
type: storrage
spec: {}
targets:
  - cloud: aws
    region: us-east-1
`)
	var stdout, stderr bytes.Buffer
	if exit := runValidate(&stdout, &stderr, []string{root}); exit == 0 {
		t.Fatal("expected non-zero exit for unknown-type")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "ErrValidatorUnknownType") {
		t.Errorf("output missing ErrValidatorUnknownType: %s", combined)
	}
}

func TestValidate_Phase4_WrongTypeValue(t *testing.T) {
	root := t.TempDir()
	writePhase4Project(t, root, `
apiVersion: infra.dev/v1alpha1
name: web
type: network
spec:
  cidr: 10.0.0.0/16
  subnetCount: "two"
targets:
  - cloud: aws
    region: us-east-1
`)
	var stdout, stderr bytes.Buffer
	if exit := runValidate(&stdout, &stderr, []string{root}); exit == 0 {
		t.Fatal("expected non-zero exit for wrong-type")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "ErrValidatorTypeSpec") {
		t.Errorf("output missing ErrValidatorTypeSpec: %s", combined)
	}
	if !strings.Contains(combined, "subnetCount") {
		t.Errorf("output missing field name 'subnetCount': %s", combined)
	}
}
