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

func seededRepo(t *testing.T, seed func(context.Context, *sqlite.Repo)) *sqlite.Repo {
	t.Helper()
	r, err := sqlite.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "default", Name: "default"})
	if seed != nil {
		seed(ctx, r)
	}
	return r
}

// decode parses the JSON envelope; returns the "data" subtree or fails.
func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\nbody: %s", err, body)
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope missing 'data' key: %s", body)
	}
	return data
}

func TestListProjects_Empty(t *testing.T) {
	h := &api.Handlers{Repo: seededRepo(t, nil), OrgID: "default"}
	rec := httptest.NewRecorder()
	h.ListProjects(rec, httptest.NewRequest("GET", "/api/v1/projects", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	data := decode(t, rec.Body.String())
	projects, ok := data["projects"].([]any)
	if !ok {
		t.Fatalf("data.projects is not an array: %v", data["projects"])
	}
	if len(projects) != 0 {
		t.Errorf("len(projects) = %d, want 0", len(projects))
	}
	// Defensive: confirm it serialized as [] not null.
	if !strings.Contains(rec.Body.String(), `"projects":[]`) {
		t.Errorf("empty projects should serialize as []: %s", rec.Body.String())
	}
}

func TestListProjects_OneProject(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
	})
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	h.ListProjects(rec, httptest.NewRequest("GET", "/api/v1/projects", nil))
	if !strings.Contains(rec.Body.String(), `"name":"demo"`) {
		t.Errorf("body missing project name: %s", rec.Body.String())
	}
}

func TestGetProject_Found(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev", StateBackendKind: "local"})
		_ = r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Name: "web-net", Type: "network", UpdatedAt: time.Now().UTC()})
	})
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/projects/p-1", nil)
	req.SetPathValue("id", "p-1")
	h.GetProject(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"name":"demo"`, `"name":"dev"`, `"name":"web-net"`, `"type":"network"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
}

func TestGetProject_NotFound(t *testing.T) {
	h := &api.Handlers{Repo: seededRepo(t, nil), OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/projects/missing", nil)
	req.SetPathValue("id", "missing")
	h.GetProject(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"ErrNotFound"`) {
		t.Errorf("body missing ErrNotFound: %s", rec.Body.String())
	}
}

func TestGetDeployment_Found(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-aws", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "aws", Region: "us-east-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-azure", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "azure", Region: "eastus", Status: "succeeded", StartedAt: time.Now().UTC()})
	})
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/deployments/d-1", nil)
	req.SetPathValue("id", "d-1")
	h.GetDeployment(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"cloud":"aws"`, `"cloud":"azure"`, `"componentName":"web-net"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
}

func TestGetDeployment_NotFound(t *testing.T) {
	h := &api.Handlers{Repo: seededRepo(t, nil), OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/deployments/missing", nil)
	req.SetPathValue("id", "missing")
	h.GetDeployment(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetRun_Found(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-aws", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "aws", Region: "us-east-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.Runs().Create(ctx, inventory.Run{ID: "r-1", OrgID: "default", DeploymentTargetID: "t-aws", Kind: "apply", Status: "succeeded", ExitCode: 0, StartedAt: time.Now().UTC()})
	})
	h := &api.Handlers{Repo: r, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/runs/r-1", nil)
	req.SetPathValue("id", "r-1")
	h.GetRun(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"id":"r-1"`, `"kind":"apply"`, `"status":"succeeded"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
}

func TestGetRun_NotFound(t *testing.T) {
	h := &api.Handlers{Repo: seededRepo(t, nil), OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/runs/missing", nil)
	req.SetPathValue("id", "missing")
	h.GetRun(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestEnvelope_DataKey(t *testing.T) {
	h := &api.Handlers{Repo: seededRepo(t, nil), OrgID: "default"}
	rec := httptest.NewRecorder()
	h.ListProjects(rec, httptest.NewRequest("GET", "/api/v1/projects", nil))
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := env["data"]; !ok {
		t.Errorf("envelope missing 'data' key: %v", env)
	}
}
