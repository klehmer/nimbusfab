package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestPlanCommand_FullStackFixture(t *testing.T) {
	reg, err := defaultCloudRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runPlan(context.Background(), planArgs{
		ProjectPath: "testdata/full-stack-project",
		Stack:       "dev",
		Adapters:    reg,
		Runner:      tofu.NewFakeRunner(),
		Inventory:   inventory.NewNullRepo(),
		WorkRoot:    t.TempDir(),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, name := range []string{"web-network", "orders-db", "web-app", "uploads"} {
		if !strings.Contains(out, name) {
			t.Errorf("plan output missing component %q:\n%s", name, out)
		}
	}
	// Phase 5: each component has 3 targets (aws + azure + gcp).
	for _, target := range []string{"aws/us-east-1", "azure/eastus", "gcp/us-central1"} {
		if !strings.Contains(out, target) {
			t.Errorf("plan output missing target %q:\n%s", target, out)
		}
	}
}
