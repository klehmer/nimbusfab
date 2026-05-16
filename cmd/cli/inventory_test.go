package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestPlanThenApplyByID(t *testing.T) {
	repo, _ := sqlite.Open("sqlite::memory:")
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

	var planOut, planErr bytes.Buffer
	code := runPlan(context.Background(), planArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Adapters:    reg, Runner: runner, Inventory: repo, WorkRoot: t.TempDir(),
		Stdout: &planOut, Stderr: &planErr,
	})
	if code != 0 {
		t.Fatalf("plan: exit %d stderr=%s", code, planErr.String())
	}
	if !strings.Contains(planOut.String(), "Deployment ID:") {
		t.Fatalf("plan output missing deployment ID:\n%s", planOut.String())
	}
	var deploymentID string
	for _, line := range strings.Split(planOut.String(), "\n") {
		if strings.HasPrefix(line, "Deployment ID:") {
			deploymentID = strings.TrimSpace(strings.TrimPrefix(line, "Deployment ID:"))
			break
		}
	}
	if !strings.HasPrefix(deploymentID, "dep-") {
		t.Fatalf("deployment ID malformed: %q", deploymentID)
	}

	var applyOut, applyErr bytes.Buffer
	code = runApply(context.Background(), applyArgs{
		PositionalArg: deploymentID,
		AutoApprove:   true,
		Adapters:      reg, Runner: runner, Inventory: repo, WorkRoot: t.TempDir(),
		Stdout: &applyOut, Stderr: &applyErr,
	})
	if code != 0 {
		t.Fatalf("apply: exit %d stderr=%s", code, applyErr.String())
	}
	if !strings.Contains(applyOut.String(), "Applied deployment") {
		t.Errorf("apply output: %s", applyOut.String())
	}
}
