package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestPlanCommand_NetworkOnlyFixture(t *testing.T) {
	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		t.Fatalf("register: %v", err)
	}
	fakeRunner := tofu.NewFakeRunner()

	var stdout, stderr bytes.Buffer
	code := runPlan(context.Background(), planArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Adapters:    reg,
		Runner:      fakeRunner,
		WorkRoot:    t.TempDir(),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "web-network") {
		t.Errorf("stdout missing component name: %s", out)
	}
	if !strings.Contains(out, "aws/us-east-1") {
		t.Errorf("stdout missing target: %s", out)
	}
}

func TestPlanCommand_RequiresStackFlag(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	var stdout, stderr bytes.Buffer
	code := runPlan(context.Background(), planArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "",
		Adapters:    reg,
		Runner:      tofu.NewFakeRunner(),
		WorkRoot:    t.TempDir(),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if code == 0 {
		t.Error("missing --stack should be non-zero exit")
	}
}
