package provisioner

import (
	"context"
	"errors"
	"testing"
)

func TestStubProvisioner_AllReturnNotImplemented(t *testing.T) {
	p, err := New(Config{WorkRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if _, err := p.Plan(ctx, PlanInput{}); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("Plan: want ErrNotImplementedYet, got %v", err)
	}
	if _, err := p.Apply(ctx, ApplyInput{}); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("Apply: want ErrNotImplementedYet, got %v", err)
	}
	if _, err := p.Destroy(ctx, DestroyInput{}); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("Destroy: want ErrNotImplementedYet, got %v", err)
	}
}
