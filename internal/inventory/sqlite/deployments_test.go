package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// seedDeploymentPrereqs creates the org/project/stack chain required by
// the deployments FK constraints and returns their IDs.
func seedDeploymentPrereqs(t *testing.T, r interface {
	Orgs() inventory.OrgRepo
	Projects() inventory.ProjectRepo
	Stacks() inventory.StackRepo
}, orgID, projectID, stackID string) {
	t.Helper()
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: orgID})
	_ = r.Projects().Create(ctx, inventory.Project{ID: projectID, OrgID: orgID, Name: projectID})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: stackID, OrgID: orgID, ProjectID: projectID, Name: "dev"})
}

func TestDeployments_DriftIntervalSeconds_Roundtrip(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	seedDeploymentPrereqs(t, r, "org-1", "proj-1", "stack-1")

	dep := inventory.Deployment{
		ID:                   "dep-1",
		OrgID:                "org-1",
		ProjectID:            "proj-1",
		StackID:              "stack-1",
		Status:               "planned",
		DriftIntervalSeconds: 1800,
		StartedAt:            time.Now().UTC(),
	}
	if err := r.Deployments().Create(ctx, dep); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := r.Deployments().Get(ctx, "org-1", "dep-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("deployment not found")
	}
	if got.DriftIntervalSeconds != 1800 {
		t.Errorf("DriftIntervalSeconds = %d, want 1800", got.DriftIntervalSeconds)
	}

	// Verify zero-value round-trips cleanly (default).
	dep2 := inventory.Deployment{
		ID:        "dep-2",
		OrgID:     "org-1",
		ProjectID: "proj-1",
		StackID:   "stack-1",
		Status:    "planned",
		// DriftIntervalSeconds intentionally zero
		StartedAt: time.Now().UTC(),
	}
	if err := r.Deployments().Create(ctx, dep2); err != nil {
		t.Fatalf("Create dep2: %v", err)
	}
	got2, _ := r.Deployments().Get(ctx, "org-1", "dep-2")
	if got2 == nil || got2.DriftIntervalSeconds != 0 {
		t.Errorf("zero DriftIntervalSeconds not preserved: %+v", got2)
	}
}

func TestDeployments_ListAll(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	seedDeploymentPrereqs(t, r, "org-A", "proj-A", "stack-A")
	seedDeploymentPrereqs(t, r, "org-B", "proj-B", "stack-B")

	base := time.Now().UTC().Truncate(time.Second)

	// Create two deployments under org-A.
	for i, d := range []inventory.Deployment{
		{ID: "d-A1", OrgID: "org-A", ProjectID: "proj-A", StackID: "stack-A",
			Status: "planned", DriftIntervalSeconds: 3600, StartedAt: base.Add(-2 * time.Minute)},
		{ID: "d-A2", OrgID: "org-A", ProjectID: "proj-A", StackID: "stack-A",
			Status: "succeeded", DriftIntervalSeconds: 0, StartedAt: base.Add(-1 * time.Minute)},
	} {
		if err := r.Deployments().Create(ctx, d); err != nil {
			t.Fatalf("Create dep %d: %v", i, err)
		}
	}

	// Create one deployment under org-B (must not appear in org-A results).
	_ = r.Deployments().Create(ctx, inventory.Deployment{
		ID: "d-B1", OrgID: "org-B", ProjectID: "proj-B", StackID: "stack-B",
		Status: "planned", StartedAt: base,
	})

	got, err := r.Deployments().ListAll(ctx, "org-A")
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Results should be newest-first.
	if got[0].ID != "d-A2" {
		t.Errorf("first result should be d-A2 (newest); got %q", got[0].ID)
	}
	if got[1].ID != "d-A1" {
		t.Errorf("second result should be d-A1; got %q", got[1].ID)
	}
	// DriftIntervalSeconds is preserved.
	if got[1].DriftIntervalSeconds != 3600 {
		t.Errorf("d-A1.DriftIntervalSeconds = %d, want 3600", got[1].DriftIntervalSeconds)
	}

	// Org isolation: org-B should see only its one deployment.
	gotB, err := r.Deployments().ListAll(ctx, "org-B")
	if err != nil {
		t.Fatalf("ListAll org-B: %v", err)
	}
	if len(gotB) != 1 || gotB[0].ID != "d-B1" {
		t.Errorf("org-B isolation: %+v", gotB)
	}

	// Non-existent org returns empty slice without error.
	gotNone, err := r.Deployments().ListAll(ctx, "no-such-org")
	if err != nil {
		t.Fatalf("ListAll no-such-org: %v", err)
	}
	if len(gotNone) != 0 {
		t.Errorf("non-existent org returned %d rows", len(gotNone))
	}
}
