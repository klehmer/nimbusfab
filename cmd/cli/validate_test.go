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
