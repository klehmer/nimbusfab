package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestCostEstimateCommand_FullStackFixture(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
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
}
