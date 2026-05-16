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

func TestPlanCommand_FullStackFixture(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
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
}
