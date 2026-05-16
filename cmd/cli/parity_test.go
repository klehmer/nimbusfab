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

func TestParityCommand_FullStackFixture(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	var stdout, stderr bytes.Buffer
	code := runParity(context.Background(), parityArgs{
		ProjectPath: "testdata/full-stack-project",
		Stack:       "dev",
		Adapters:    reg, Runner: tofu.NewFakeRunner(), Inventory: inventory.NewNullRepo(),
		WorkRoot: t.TempDir(),
		Stdout:   &stdout, Stderr: &stderr,
	})
	if code != 0 {
		t.Errorf("exit %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Parity score:") {
		t.Errorf("no parity score in output: %s", out)
	}
}
