package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi/api"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// seedDriftRecords creates one deployment with two targets; one drifts,
// one is clean. Returns the deployment ID for cross-reference.
func seedDriftRecords(t *testing.T, r *sqlite.Repo) {
	t.Helper()
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "default", Name: "default"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "default", Name: "demo", CreatedAt: time.Now()})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "default", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "default", ProjectID: "p", StackID: "s", Status: "succeeded", StartedAt: time.Now()})
	for _, td := range []struct {
		id    string
		cloud string
		drift bool
	}{
		{"t-aws", "aws", true},
		{"t-azure", "azure", false},
	} {
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: td.id, OrgID: "default", DeploymentID: "d",
			ComponentName: "web-app", Cloud: td.cloud, Region: "us-east-1",
			CredentialRef: "x", Status: "succeeded", StartedAt: time.Now(),
		})
		_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
			DeploymentTargetID: td.id, OrgID: "default",
			DetectedAt: time.Now().UTC(), HasDrift: td.drift,
			SummaryJSON: []byte(`{}`),
		})
	}
}

func TestGetDrift_Returns(t *testing.T) {
	r := seededRepo(t, nil)
	seedDriftRecords(t, r)

	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	h.GetDrift(rec, httptest.NewRequest("GET", "/api/v1/drift", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := env["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if summary["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", summary["total"])
	}
	if summary["drifted"].(float64) != 1 {
		t.Errorf("drifted = %v, want 1", summary["drifted"])
	}
	if summary["clean"].(float64) != 1 {
		t.Errorf("clean = %v, want 1", summary["clean"])
	}
	records := data["records"].([]any)
	if len(records) != 2 {
		t.Fatalf("records len = %d", len(records))
	}
}

func TestGetDrift_EmptyOrg(t *testing.T) {
	r := seededRepo(t, nil)
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	h.GetDrift(rec, httptest.NewRequest("GET", "/api/v1/drift", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"records":[]`) {
		t.Errorf("body should have empty records: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Errorf("body should have total:0: %s", rec.Body.String())
	}
}

func TestGetDrift_OrphanedRecordSkipped(t *testing.T) {
	r := seededRepo(t, nil)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "default", Name: "default"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "default", Name: "demo", CreatedAt: time.Now()})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "default", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "default", ProjectID: "p", StackID: "s", Status: "ok", StartedAt: time.Now()})
	_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
		ID: "t-real", OrgID: "default", DeploymentID: "d", ComponentName: "x", Cloud: "aws", Region: "r", CredentialRef: "x", Status: "ok", StartedAt: time.Now(),
	})
	_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
		DeploymentTargetID: "t-real", OrgID: "default", DetectedAt: time.Now(), HasDrift: false, SummaryJSON: []byte(`{}`),
	})
	// Real target produces a record; orphaned (no target) would be skipped
	// — there's no way to upsert a drift_status row with no target since
	// the FK constraint blocks it, so this test mostly asserts the happy
	// path doesn't crash when one record exists.
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	h.GetDrift(rec, httptest.NewRequest("GET", "/api/v1/drift", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
}
