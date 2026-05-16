package provisioner

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestNew_RequiresWorkRoot(t *testing.T) {
	_, err := New(Config{Adapters: cloud.NewRegistry(), Runner: tofu.NewFakeRunner()})
	if err == nil {
		t.Fatal("New(no WorkRoot): nil err, want non-nil")
	}
}

func TestNew_RequiresAdapters(t *testing.T) {
	_, err := New(Config{WorkRoot: t.TempDir(), Runner: tofu.NewFakeRunner()})
	if err == nil {
		t.Fatal("New(no Adapters): nil err, want non-nil")
	}
}

func TestNew_RequiresRunner(t *testing.T) {
	_, err := New(Config{WorkRoot: t.TempDir(), Adapters: cloud.NewRegistry()})
	if err == nil {
		t.Fatal("New(no Runner): nil err, want non-nil")
	}
}

func TestRuntimeProvisioner_RequiresPlanResult(t *testing.T) {
	// Phase 2 implements Apply/Destroy/DetectDrift, but all three need a
	// PlanResult since inventory lookup isn't wired yet. Verify they reject
	// missing input cleanly rather than panicking.
	p, err := New(Config{
		WorkRoot: t.TempDir(),
		Adapters: cloud.NewRegistry(),
		Runner:   tofu.NewFakeRunner(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if _, err := p.Apply(ctx, ApplyInput{}); err == nil {
		t.Error("Apply with no PlanResult: nil err, want non-nil")
	}
	if _, err := p.Destroy(ctx, DestroyInput{}); err == nil {
		t.Error("Destroy with no PlanResult: nil err, want non-nil")
	}
	if _, err := p.DetectDrift(ctx, DriftInput{}); err == nil {
		t.Error("DetectDrift with no PlanResult: nil err, want non-nil")
	}
	_ = errors.New // keep import even if unused after edit
}
