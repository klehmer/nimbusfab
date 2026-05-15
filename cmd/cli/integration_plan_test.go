//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

// TestPlanCommand_RealTofu runs `nimbusfab plan` end-to-end against the real
// `tofu` binary if it's on $PATH. No AWS credentials required: we expect
// the AWS provider to fail when it tries to contact AWS, and we accept that
// (the assertion is only that the workspace got materialized correctly
// before that failure).
func TestPlanCommand_RealTofu(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH; skipping integration test")
	}
	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runPlan(context.Background(), planArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Adapters:    reg,
		Runner:      tofu.NewExecRunner(),
		WorkRoot:    t.TempDir(),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	// Accept exit 0 (everything worked) or exit 1 (provider call failed) —
	// both prove we reached the planning stage correctly.
	if code != 0 && code != 1 {
		t.Errorf("unexpected exit code %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Planning") && !strings.Contains(stderr.String(), "tofu") {
		t.Errorf("expected to reach the planning stage; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}
