package sqlite_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func openMemory(t *testing.T) *sqlite.Repo {
	t.Helper()
	r, err := sqlite.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return r
}

func TestRepo_Ping(t *testing.T) {
	r := openMemory(t)
	if err := r.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestRepo_OrgsRoundTrip(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	if err := r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "test"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	o, err := r.Orgs().Get(ctx, "org-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if o == nil || o.Name != "test" {
		t.Errorf("got %+v", o)
	}
	list, _ := r.Orgs().List(ctx)
	if len(list) != 1 {
		t.Errorf("list len = %d", len(list))
	}
}

func TestRepo_OrgsDuplicate(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "x", Name: "x"})
	if err := r.Orgs().Create(ctx, inventory.Org{ID: "x", Name: "x"}); err == nil {
		t.Error("duplicate Create should fail")
	}
}
