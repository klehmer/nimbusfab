package ui_test

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi/ui"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestNewRenderer_ParsesTemplates(t *testing.T) {
	r, err := ui.NewRenderer(inventory.NewNullRepo(), "default", "")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	if r == nil {
		t.Fatal("renderer is nil")
	}
}

func TestAssetsFS_ContainsStylesheet(t *testing.T) {
	assets, err := ui.AssetsFS()
	if err != nil {
		t.Fatalf("AssetsFS: %v", err)
	}
	data, err := fs.ReadFile(assets, "style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	if len(data) == 0 {
		t.Error("style.css is empty")
	}
	// UI Phase 2: assert new classes are present.
	for _, want := range []string{".actions", ".log-pane", ".log-line"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("style.css missing UI Phase 2 class %q", want)
		}
	}
}

func TestAssetsFS_ContainsAppJS(t *testing.T) {
	assets, err := ui.AssetsFS()
	if err != nil {
		t.Fatalf("AssetsFS: %v", err)
	}
	data, err := fs.ReadFile(assets, "app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	if len(data) == 0 {
		t.Error("app.js is empty")
	}
	for _, want := range []string{"attachDeploymentActions", "EventSource", "window.nimbusfab"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("app.js missing %q", want)
		}
	}
}

// seededRepo opens an in-memory SQLite repo with one org and the provided
// seed function applied. Returned repo is closed at test cleanup.
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
	if err := r.Orgs().Create(ctx, inventory.Org{ID: "default", Name: "default"}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if seed != nil {
		seed(ctx, r)
	}
	return r
}

func TestListProjects_EmptyRepo(t *testing.T) {
	r := seededRepo(t, nil)
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	rend.ListProjects(rec, httptest.NewRequest("GET", "/ui/projects", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No projects registered yet") {
		t.Errorf("expected empty-state copy; got: %s", body)
	}
}

func TestListProjects_OneProject(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
	})
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	rend.ListProjects(rec, httptest.NewRequest("GET", "/ui/projects", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "demo") {
		t.Errorf("body missing project name; got: %s", body)
	}
	if !strings.Contains(body, `href="/ui/projects/p-1"`) {
		t.Errorf("body missing project link; got: %s", body)
	}
}

func TestProjectDetail_Renders(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev", StateBackendKind: "local"})
		_ = r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Name: "web-net", Type: "network", UpdatedAt: time.Now().UTC()})
	})
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/projects/p-1", nil)
	req.SetPathValue("id", "p-1")
	rend.ProjectDetail(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"demo", "dev", "web-net", "network"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
}

func TestProjectDetail_NotFound(t *testing.T) {
	r := seededRepo(t, nil)
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/projects/missing", nil)
	req.SetPathValue("id", "missing")
	rend.ProjectDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "project not found") {
		t.Errorf("body missing 404 message; got: %s", rec.Body.String())
	}
}

func TestDeploymentDetail_Renders(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-aws", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "aws", Region: "us-east-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-azure", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "azure", Region: "eastus", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.Runs().Create(ctx, inventory.Run{ID: "r-1", OrgID: "default", DeploymentTargetID: "t-aws", Kind: "apply", Status: "succeeded", StartedAt: time.Now().UTC()})
	})
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/deployments/d-1", nil)
	req.SetPathValue("id", "d-1")
	rend.DeploymentDetail(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"web-net", "aws", "azure", "us-east-1", "eastus", `href="/ui/runs/r-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
	// UI Phase 2 additions: action bar + log pane + script tag.
	for _, want := range []string{
		`data-action="applies"`,
		`data-action="destroys"`,
		`data-action="drifts"`,
		`id="event-log"`,
		`src="/assets/app.js"`,
		`nimbusfab.attachDeploymentActions`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing UI Phase 2 element %q", want)
		}
	}
}

func TestDeploymentDetail_APITokenWiredIntoScriptTag(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
	})

	// With token set: attribute appears.
	withToken, _ := ui.NewRenderer(r, "default", "topsecret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/deployments/d-1", nil)
	req.SetPathValue("id", "d-1")
	withToken.DeploymentDetail(rec, req)
	if !strings.Contains(rec.Body.String(), `data-api-token="topsecret"`) {
		t.Errorf("with APIToken: body missing data-api-token attribute")
	}

	// Without token: attribute absent.
	noToken, _ := ui.NewRenderer(r, "default", "")
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/ui/deployments/d-1", nil)
	req2.SetPathValue("id", "d-1")
	noToken.DeploymentDetail(rec2, req2)
	if strings.Contains(rec2.Body.String(), `data-api-token`) {
		t.Errorf("without APIToken: body should not contain data-api-token attribute")
	}
}

func TestDeploymentDetail_NotFound(t *testing.T) {
	r := seededRepo(t, nil)
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/deployments/missing", nil)
	req.SetPathValue("id", "missing")
	rend.DeploymentDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRunDetail_Renders(t *testing.T) {
	r := seededRepo(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t-aws", OrgID: "default", DeploymentID: "d-1", ComponentName: "web-net", Cloud: "aws", Region: "us-east-1", Status: "succeeded", StartedAt: time.Now().UTC()})
		_ = r.Runs().Create(ctx, inventory.Run{ID: "r-1", OrgID: "default", DeploymentTargetID: "t-aws", Kind: "apply", Status: "succeeded", ExitCode: 0, StartedAt: time.Now().UTC()})
	})
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/runs/r-1", nil)
	req.SetPathValue("id", "r-1")
	rend.RunDetail(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"r-1", "apply", "succeeded"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got: %s", want, body)
		}
	}
}

func TestRunDetail_NotFound(t *testing.T) {
	r := seededRepo(t, nil)
	rend, _ := ui.NewRenderer(r, "default", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/runs/missing", nil)
	req.SetPathValue("id", "missing")
	rend.RunDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
