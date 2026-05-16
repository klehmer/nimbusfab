package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestCostEstimateCommand_FullStackFixture(t *testing.T) {
	reg, err := defaultCloudRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runCostEstimate(context.Background(), costEstimateArgs{
		ProjectPath: "testdata/full-stack-project",
		Stack:       "dev",
		Adapters:    reg, Runner: tofu.NewFakeRunner(), Inventory: inventory.NewNullRepo(),
		WorkRoot: t.TempDir(),
		Stdout:   &stdout, Stderr: &stderr,
	})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Total: $") {
		t.Errorf("no Total line: %s", out)
	}
	if !strings.Contains(out, "/month") {
		t.Errorf("expected per-month label: %s", out)
	}
	// Phase 5: estimate should include AWS, Azure, and GCP subtotals.
	for _, target := range []string{"aws/us-east-1", "azure/eastus", "gcp/us-central1"} {
		if !strings.Contains(out, target) {
			t.Errorf("missing target %q in cost output:\n%s", target, out)
		}
	}
}
