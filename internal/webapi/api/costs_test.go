package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi/api"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// seedDeploymentWithCosts creates a deployment with one target, one plan
// run, and two cost-estimate rows. Returns the deployment ID.
func seedDeploymentWithCosts(t *testing.T, r *sqlite.Repo) string {
	t.Helper()
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "default", Name: "default"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now()})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "planned", StartedAt: time.Now()})
	_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
		ID: "t-1", OrgID: "default", DeploymentID: "d-1",
		ComponentName: "web-app", Cloud: "aws", Region: "us-east-1",
		CredentialRef: "x", Status: "planned", StartedAt: time.Now(),
	})
	_ = r.Runs().Create(ctx, inventory.Run{
		ID: "r-1", OrgID: "default", DeploymentTargetID: "t-1",
		Kind: "plan", Status: "succeeded", StartedAt: time.Now(),
	})
	_ = r.CostEstimates().BulkInsert(ctx, []inventory.CostEstimate{
		{RunID: "r-1", OrgID: "default", PrimitiveID: "ec2-a", Currency: "USD",
			UnitPrice: 0.0416, Units: 730, UnitOfMeasure: "Hrs", Subtotal: 30.37,
			PricingKeyJSON: []byte(`{"sku":"t3.small"}`)},
		{RunID: "r-1", OrgID: "default", PrimitiveID: "ebs-a", Currency: "USD",
			UnitPrice: 0.10, Units: 30, UnitOfMeasure: "GB-Mo", Subtotal: 3.00,
			PricingKeyJSON: []byte(`{"sku":"ebs-gp3"}`)},
	})
	return "d-1"
}

func TestGetDeploymentCosts_Found(t *testing.T) {
	r := seededRepo(t, nil) // org + nothing else; seed within
	depID := seedDeploymentWithCosts(t, r)

	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/deployments/"+depID+"/costs", nil)
	req.SetPathValue("id", depID)
	h.GetDeploymentCosts(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["currency"] != "USD" {
		t.Errorf("currency = %v", data["currency"])
	}
	total := data["total"].(float64)
	if total < 33.0 || total > 34.0 {
		t.Errorf("total = %.2f, want ~33.37", total)
	}
	targets := data["targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	target := targets[0].(map[string]any)
	if target["componentName"] != "web-app" || target["cloud"] != "aws" {
		t.Errorf("target metadata wrong: %+v", target)
	}
	prims := target["primitives"].([]any)
	if len(prims) != 2 {
		t.Errorf("primitives len = %d, want 2", len(prims))
	}
}

func TestGetDeploymentCosts_NoCostsYet(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "planned", StartedAt: time.Now()})
	})
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/deployments/d-1/costs", nil)
	req.SetPathValue("id", "d-1")
	h.GetDeploymentCosts(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"targets":[]`) {
		t.Errorf("body should have empty targets array: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Errorf("body should have total:0: %s", rec.Body.String())
	}
}

func TestGetDeploymentCosts_NotFound(t *testing.T) {
	r := seededRepo(t, nil)
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/deployments/missing/costs", nil)
	req.SetPathValue("id", "missing")
	h.GetDeploymentCosts(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ErrNotFound") {
		t.Errorf("body missing ErrNotFound: %s", rec.Body.String())
	}
}
