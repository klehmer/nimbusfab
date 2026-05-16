package inventory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestNullRepo_WritesAreNoOp(t *testing.T) {
	r := inventory.NewNullRepo()
	ctx := context.Background()

	if err := r.Migrate(ctx); err != nil {
		t.Errorf("Migrate: %v", err)
	}
	if err := r.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
	if err := r.Orgs().Create(ctx, inventory.Org{ID: "x"}); err != nil {
		t.Errorf("Orgs.Create: %v", err)
	}
	if err := r.Projects().Create(ctx, inventory.Project{ID: "x"}); err != nil {
		t.Errorf("Projects.Create: %v", err)
	}
	if err := r.Deployments().Create(ctx, inventory.Deployment{ID: "x", StartedAt: time.Now()}); err != nil {
		t.Errorf("Deployments.Create: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNullRepo_ReadsReturnRequired(t *testing.T) {
	r := inventory.NewNullRepo()
	ctx := context.Background()

	if _, err := r.Orgs().Get(ctx, "x"); !errors.Is(err, inventory.ErrInventoryRequired) {
		t.Errorf("Orgs.Get: want ErrInventoryRequired, got %v", err)
	}
	if _, err := r.Projects().Get(ctx, "x", "y"); !errors.Is(err, inventory.ErrInventoryRequired) {
		t.Errorf("Projects.Get: want ErrInventoryRequired, got %v", err)
	}
	if _, err := r.Deployments().Get(ctx, "x", "y"); !errors.Is(err, inventory.ErrInventoryRequired) {
		t.Errorf("Deployments.Get: want ErrInventoryRequired, got %v", err)
	}
}

func TestIsNullRepo(t *testing.T) {
	if !inventory.IsNullRepo(inventory.NewNullRepo()) {
		t.Error("IsNullRepo(NewNullRepo()) = false")
	}
}
