package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// seedDriftFixture builds a complete FK chain:
//
//	org → project → stack → deployment → N targets → drift_status rows.
//
// Returns the deployment ID.
func seedDriftFixture(t *testing.T, r interface {
	Orgs() inventory.OrgRepo
	Projects() inventory.ProjectRepo
	Stacks() inventory.StackRepo
	Deployments() inventory.DeploymentRepo
	DeploymentTargets() inventory.DeploymentTargetRepo
	DriftStatus() inventory.DriftStatusRepo
}, orgID, projectID string) (deploymentID string) {
	t.Helper()
	ctx := context.Background()
	stackID := orgID + "-stack"
	deploymentID = orgID + "-dep"

	_ = r.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: orgID})
	_ = r.Projects().Create(ctx, inventory.Project{ID: projectID, OrgID: orgID, Name: projectID})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: stackID, OrgID: orgID, ProjectID: projectID, Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{
		ID: deploymentID, OrgID: orgID, ProjectID: projectID, StackID: stackID,
		Status: "succeeded", StartedAt: time.Now().UTC(),
	})

	base := time.Now().UTC().Truncate(time.Second)
	targets := []struct {
		id, component, cloud, region string
		hasDrift                     bool
	}{
		{"tgt-web", "web", "aws", "us-east-1", true},
		{"tgt-db", "db", "aws", "us-east-1", false},
	}
	for i, tgt := range targets {
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: tgt.id, OrgID: orgID, DeploymentID: deploymentID,
			ComponentName: tgt.component, Cloud: tgt.cloud, Region: tgt.region,
			CredentialRef: "cred", Status: "succeeded", StartedAt: time.Now().UTC(),
		})
		_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
			DeploymentTargetID: tgt.id,
			OrgID:              orgID,
			DetectedAt:         base.Add(time.Duration(i) * time.Second),
			HasDrift:           tgt.hasDrift,
			SummaryJSON:        []byte(`{"component":"` + tgt.component + `"}`),
		})
	}
	return deploymentID
}

func TestDriftStatus_LatestByDeployment(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	depID := seedDriftFixture(t, r, "org-d", "proj-d")

	got, err := r.DriftStatus().LatestByDeployment(ctx, "org-d", depID)
	if err != nil {
		t.Fatalf("LatestByDeployment: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	// Both records should have joined metadata populated.
	for _, rec := range got {
		if rec.ComponentName == "" {
			t.Errorf("ComponentName empty for target %s", rec.DeploymentTargetID)
		}
		if rec.Cloud != "aws" {
			t.Errorf("Cloud = %q, want aws", rec.Cloud)
		}
		if rec.Region != "us-east-1" {
			t.Errorf("Region = %q, want us-east-1", rec.Region)
		}
		if rec.DeploymentID != depID {
			t.Errorf("DeploymentID = %q, want %q", rec.DeploymentID, depID)
		}
	}

	// tgt-web has drift; verify HasDrift is preserved.
	hasDriftMap := map[string]bool{}
	for _, rec := range got {
		hasDriftMap[rec.ComponentName] = rec.HasDrift
	}
	if !hasDriftMap["web"] {
		t.Errorf("web component should have drift")
	}
	if hasDriftMap["db"] {
		t.Errorf("db component should not have drift")
	}

	// Wrong deployment ID returns empty, no error.
	none, err := r.DriftStatus().LatestByDeployment(ctx, "org-d", "no-such-dep")
	if err != nil {
		t.Fatalf("wrong deploymentID: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("wrong deploymentID returned %d rows", len(none))
	}

	// Wrong org returns empty.
	none2, _ := r.DriftStatus().LatestByDeployment(ctx, "other-org", depID)
	if len(none2) != 0 {
		t.Errorf("wrong org returned %d rows", len(none2))
	}
}

func TestDriftStatus_ListByProject(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	// Two projects under the same org; each has one deployment with two targets.
	seedDriftFixture(t, r, "org-p", "proj-X")

	// Add a second project under the same org.
	stackY := "org-p-stackY"
	depY := "org-p-depY"
	_ = r.Projects().Create(ctx, inventory.Project{ID: "proj-Y", OrgID: "org-p", Name: "proj-Y"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: stackY, OrgID: "org-p", ProjectID: "proj-Y", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{
		ID: depY, OrgID: "org-p", ProjectID: "proj-Y", StackID: stackY,
		Status: "succeeded", StartedAt: time.Now().UTC(),
	})
	_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
		ID: "tgt-Y1", OrgID: "org-p", DeploymentID: depY,
		ComponentName: "cache", Cloud: "azure", Region: "eastus",
		CredentialRef: "cred", Status: "succeeded", StartedAt: time.Now().UTC(),
	})
	_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
		DeploymentTargetID: "tgt-Y1", OrgID: "org-p",
		DetectedAt: time.Now().UTC(), HasDrift: true, SummaryJSON: []byte(`{}`),
	})

	// proj-X has 2 targets; proj-Y has 1.
	gotX, err := r.DriftStatus().ListByProject(ctx, "org-p", "proj-X")
	if err != nil {
		t.Fatalf("ListByProject proj-X: %v", err)
	}
	if len(gotX) != 2 {
		t.Fatalf("proj-X: len = %d, want 2", len(gotX))
	}
	for _, rec := range gotX {
		if rec.ComponentName == "" || rec.Cloud == "" || rec.Region == "" {
			t.Errorf("joined fields empty: %+v", rec)
		}
	}

	gotY, err := r.DriftStatus().ListByProject(ctx, "org-p", "proj-Y")
	if err != nil {
		t.Fatalf("ListByProject proj-Y: %v", err)
	}
	if len(gotY) != 1 {
		t.Fatalf("proj-Y: len = %d, want 1", len(gotY))
	}
	if gotY[0].ComponentName != "cache" || gotY[0].Cloud != "azure" {
		t.Errorf("proj-Y record wrong: %+v", gotY[0])
	}

	// Non-existent project returns empty without error.
	none, err := r.DriftStatus().ListByProject(ctx, "org-p", "no-such-proj")
	if err != nil {
		t.Fatalf("no-such-proj: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("non-existent project returned %d rows", len(none))
	}

	// Wrong org returns empty.
	if other, _ := r.DriftStatus().ListByProject(ctx, "other-org", "proj-X"); len(other) != 0 {
		t.Errorf("wrong-org returned %d rows", len(other))
	}
}
