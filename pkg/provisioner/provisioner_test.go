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

func TestRuntimeProvisioner_DestroyAndDriftNotYetImplemented(t *testing.T) {
	// Apply now works (Phase 2 Task 5). Destroy + DetectDrift land in Tasks
	// 7 and 8; until then they should return ErrNotImplementedYet so callers
	// fail fast rather than receiving a nil result.
	p, err := New(Config{
		WorkRoot: t.TempDir(),
		Adapters: cloud.NewRegistry(),
		Runner:   tofu.NewFakeRunner(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if _, err := p.Destroy(ctx, DestroyInput{}); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("Destroy: want ErrNotImplementedYet, got %v", err)
	}
	if _, err := p.DetectDrift(ctx, DriftInput{}); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("DetectDrift: want ErrNotImplementedYet, got %v", err)
	}
}
