package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestParityCommand_FullStackFixture(t *testing.T) {
	reg, err := defaultCloudRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
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
