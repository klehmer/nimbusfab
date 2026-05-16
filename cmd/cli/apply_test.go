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

func TestApplyCommand_HappyPath(t *testing.T) {
	// Phase 1 secrets backend is wired into the CLI default; satisfy the
	// fixture's credentialRef so resolution succeeds.
	t.Setenv("NIMBUSFAB_SECRET_AWS_DEV", `{"AWS_ACCESS_KEY_ID":"test","AWS_SECRET_ACCESS_KEY":"test"}`)

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

	var stdout, stderr bytes.Buffer
	code := runApply(context.Background(), applyArgs{
		PositionalArg: "testdata/network-only-project",
		Stack:         "dev",
		AutoApprove:   true,
		Adapters:      reg, Runner: runner, Inventory: inventory.NewNullRepo(), WorkRoot: t.TempDir(),
		Stdout: &stdout, Stderr: &stderr,
	})
	if code != 0 {
		t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Apply succeeded") {
		t.Errorf("stdout missing summary: %s", stdout.String())
	}
	// Phase 1: confirm credentialRef resolved into the runner's Workspace.Environment.
	if len(runner.ApplyCalls) == 0 {
		t.Fatalf("no apply calls recorded")
	}
	gotEnv := runner.ApplyCalls[0].Workspace.Environment
	if gotEnv["AWS_ACCESS_KEY_ID"] != "test" {
		t.Errorf("AWS_ACCESS_KEY_ID not propagated; env=%v", gotEnv)
	}
}

func TestDestroyCommand_HappyPath(t *testing.T) {
	// Phase 1 secrets backend is wired into the CLI default; satisfy the
	// fixture's credentialRef so resolution succeeds.
	t.Setenv("NIMBUSFAB_SECRET_AWS_DEV", `{"AWS_ACCESS_KEY_ID":"test","AWS_SECRET_ACCESS_KEY":"test"}`)

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	var stdout, stderr bytes.Buffer
	code := runDestroy(context.Background(), destroyArgs{
		PositionalArg: "testdata/network-only-project",
		Stack:         "dev",
		AutoApprove:   true,
		Adapters:      reg, Runner: tofu.NewFakeRunner(), Inventory: inventory.NewNullRepo(), WorkRoot: t.TempDir(),
		Stdout: &stdout, Stderr: &stderr,
	})
	if code != 0 {
		t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
	}
}

func TestDriftCommand_NoDrift(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := tofu.NewFakeRunner()
	runner.DriftPlan = &tofu.PlanArtifact{JSONPlan: []byte(`{"resource_changes":[]}`), HasChanges: false}
	var stdout, stderr bytes.Buffer
	code := runDrift(context.Background(), driftArgs{
		PositionalArg: "testdata/network-only-project",
		Stack:         "dev",
		Adapters:      reg, Runner: runner, Inventory: inventory.NewNullRepo(), WorkRoot: t.TempDir(),
		Stdout: &stdout, Stderr: &stderr,
	})
	if code != 0 {
		t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No drift") {
		t.Errorf("stdout missing summary: %s", stdout.String())
	}
}
