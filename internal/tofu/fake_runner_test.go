package tofu

import (
	"context"
	"testing"
)

func TestFakeRunner_RecordsCalls(t *testing.T) {
	r := NewFakeRunner()
	ws := Workspace{Dir: t.TempDir()}
	ctx := context.Background()

	if err := r.Init(ctx, ws); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := r.Plan(ctx, ws, PlanOpts{OutFile: "/tmp/x.plan"}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if v, err := r.Version(ctx); err != nil || v != "OpenTofu v1.7.0" {
		t.Fatalf("Version: v=%q err=%v", v, err)
	}
	if len(r.InitCalls) != 1 || len(r.PlanCalls) != 1 {
		t.Fatalf("call recording: init=%d plan=%d", len(r.InitCalls), len(r.PlanCalls))
	}
}

func TestFakeRunner_PlanWritesPlanFile(t *testing.T) {
	r := NewFakeRunner()
	r.PlanFileContents = []byte("FAKE-PLAN-BIN")
	ws := Workspace{Dir: t.TempDir()}
	planFile := ws.Dir + "/plan.bin"
	if _, err := r.Plan(context.Background(), ws, PlanOpts{OutFile: planFile}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Verify the file was actually written.
	if _, err := readFileBytes(planFile); err != nil {
		t.Errorf("plan file not written: %v", err)
	}
}

func readFileBytes(p string) ([]byte, error) {
	return readAll(p)
}
