package webapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi"
	"github.com/klehmer/nimbusfab/internal/webapi/auth"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func newServer(t *testing.T, seed func(context.Context, *sqlite.Repo)) (*httptest.Server, *sqlite.Repo) {
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
	h, err := webapi.New(webapi.Config{Repo: r, OrgID: "default"})
	if err != nil {
		t.Fatalf("webapi.New: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, r
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	client := srv.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

func TestRouter_Healthz(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/healthz")
	if resp.StatusCode != 200 || body != "ok" {
		t.Errorf("healthz: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestRouter_Readyz(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/readyz")
	if resp.StatusCode != 200 || body != "ready" {
		t.Errorf("readyz: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestRouter_RootRedirect(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, _ := get(t, srv, "/")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/ui/projects" {
		t.Errorf("Location = %q, want /ui/projects", loc)
	}
}

func TestRouter_AssetsStylesheet(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/assets/style.css")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if !strings.Contains(body, ".topbar") {
		t.Errorf("body doesn't look like style.css")
	}
}

func TestRouter_UIProjects(t *testing.T) {
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
	})
	resp, body := get(t, srv, "/ui/projects")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "demo") {
		t.Errorf("body missing project name")
	}
}

func TestRouter_UIProjectDetailMissing(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, _ := get(t, srv, "/ui/projects/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestNew_NilRepoRejected(t *testing.T) {
	_, err := webapi.New(webapi.Config{Repo: nil})
	if err == nil {
		t.Error("expected error for nil repo")
	}
}

// --- HTTP Phase 1: /api/v1/* ---

func TestRouter_APIProjectsEmpty(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/api/v1/projects")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"projects":[]`) {
		t.Errorf("body missing empty projects array: %s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestRouter_APIProjectMissing(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/api/v1/projects/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, `"code":"ErrNotFound"`) {
		t.Errorf("body missing error code: %s", body)
	}
}

// Auth Phase 1: legacy bearer-token tests removed in favor of session-and-PAT
// integration tests below. The APIToken Config field is deprecated; default
// AuthMode=disabled covers the dev path. AuthMode=local exercises real auth.

func TestRouter_APIRequiresAuth_LocalMode_401(t *testing.T) {
	srv := newServerLocalMode(t, nil)
	resp, body := get(t, srv, "/api/v1/projects")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(body, "ErrUnauthorized") {
		t.Errorf("body missing ErrUnauthorized: %s", body)
	}
}

func TestRouter_UIRequiresAuth_LocalMode_RedirectsToLogin(t *testing.T) {
	srv := newServerLocalMode(t, nil)
	resp, _ := get(t, srv, "/ui/projects")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302 redirect", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/auth/login" {
		t.Errorf("Location = %q, want /auth/login", loc)
	}
}

// newServerLocalMode builds a test server with AuthMode=local + a fixed
// session key. Tests can then either POST /auth/login to acquire a cookie
// or use Bearer PAT auth.
func newServerLocalMode(t *testing.T, seed func(context.Context, *sqlite.Repo)) *httptest.Server {
	t.Helper()
	r, err := sqlite.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
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
	h, err := webapi.New(webapi.Config{
		Repo: r, OrgID: "default",
		AuthMode:   "local",
		SessionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("webapi.New: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// --- HTTP Phase 2 router mounts ---

func TestRouter_NoEngine_NoMutatingRoutes(t *testing.T) {
	// When Config.Engine is nil, POST /api/v1/.../applies should 404 —
	// the route was never registered.
	srv, _ := newServer(t, nil)
	resp, _ := post(t, srv, "/api/v1/deployments/anything/applies", `{}`)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 404 or 405", resp.StatusCode)
	}
}

// post is a helper paralleling get() for POST requests.
func post(t *testing.T, srv *httptest.Server, path, body string) (*http.Response, string) {
	t.Helper()
	client := srv.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(respBody)
}

// --- UI Phase 2: deployment detail page renders script tag ---

func TestRouter_DeploymentDetailHasScriptTag(t *testing.T) {
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "succeeded", StartedAt: time.Now().UTC()})
	})
	resp, body := get(t, srv, "/ui/deployments/d-1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{`src="/assets/app.js"`, `nimbusfab.attachDeploymentActions`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestRouter_AppJSServedFromAssets(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/assets/app.js")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}
	if !strings.Contains(body, "attachDeploymentActions") {
		t.Errorf("app.js body missing expected function")
	}
}

func TestRouter_APIDeploymentCostsEmpty(t *testing.T) {
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1", Status: "planned", StartedAt: time.Now()})
	})
	resp, body := get(t, srv, "/api/v1/deployments/d-1/costs")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"total":0`) || !strings.Contains(body, `"targets":[]`) {
		t.Errorf("body: %s", body)
	}
}

func TestRouter_DriftAPI(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/api/v1/drift")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"records":[]`) || !strings.Contains(body, `"total":0`) {
		t.Errorf("body: %s", body)
	}
}

func TestRouter_DriftUI(t *testing.T) {
	srv, _ := newServer(t, nil)
	resp, body := get(t, srv, "/ui/drift")
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Drift overview") {
		t.Errorf("body missing 'Drift overview': %s", body)
	}
}

func TestRouter_LoginFlow_WithSeededUser(t *testing.T) {
	srv := newServerLocalMode(t, func(ctx context.Context, r *sqlite.Repo) {
		// Seed a real user with a known bcrypt password.
		hash, _ := authHash("hunter2")
		_ = r.Users().Create(ctx, inventory.User{
			ID: "u-1", OrgID: "default", Email: "alice@example.com",
			IsLocal: true, PasswordHash: hash,
		})
	})
	// Use a cookie jar to capture the session.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // observe each redirect
		},
	}
	// 1. POST /auth/login with valid creds → 302 to /ui/projects + Set-Cookie.
	form := url.Values{}
	form.Set("email", "alice@example.com")
	form.Set("password", "hunter2")
	resp, err := client.PostForm(srv.URL+"/auth/login", form)
	if err != nil {
		t.Fatalf("POST /auth/login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/ui/projects" {
		t.Errorf("Location = %q, want /ui/projects", loc)
	}

	// 2. Authenticated request: GET /ui/projects should now succeed.
	req2, _ := http.NewRequest("GET", srv.URL+"/ui/projects", nil)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GET /ui/projects: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("authenticated GET /ui/projects status = %d, want 200", resp2.StatusCode)
	}

	// 3. Bad password: 302 to /auth/login?error=invalid.
	form.Set("password", "wrong")
	resp3, _ := client.PostForm(srv.URL+"/auth/login", form)
	resp3.Body.Close()
	if loc := resp3.Header.Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("bad password Location = %q, want error param", loc)
	}
}

// authHash wraps internal/webapi/auth.HashPassword for the integration test.
func authHash(plain string) ([]byte, error) {
	return auth.HashPassword(plain)
}

// --- Graph page tests ---

func TestUI_DeploymentGraph_Renders(t *testing.T) {
	// Seed a deployment with two components (one with a ref) and a deployment
	// target so the graph has at least one edge and one status badge.
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-graph", OrgID: "default", Name: "graphdemo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-graph", OrgID: "default", ProjectID: "p-graph", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{
			ID: "d-graph", OrgID: "default", ProjectID: "p-graph", StackID: "s-graph",
			Status: "succeeded", StartedAt: time.Now().UTC(),
		})
		// net component — no refs
		netIR := `{"name":"net","type":"network","spec":{},"targets":[{"cloud":"aws","region":"us-east-1","credentialRef":"default"}]}`
		_ = r.Components().Upsert(ctx, inventory.Component{
			ID: "c-net", OrgID: "default", ProjectID: "p-graph", StackID: "s-graph",
			Name: "net", Type: "network", IRJSON: []byte(netIR),
		})
		// app component — refs net
		appIR := `{"name":"app","type":"compute","spec":{},"targets":[{"cloud":"aws","region":"us-east-1","credentialRef":"default"}],"refs":[{"component":"net","output":"vpc_id","as":"vpcId"}]}`
		_ = r.Components().Upsert(ctx, inventory.Component{
			ID: "c-app", OrgID: "default", ProjectID: "p-graph", StackID: "s-graph",
			Name: "app", Type: "compute", IRJSON: []byte(appIR),
		})
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: "dt-1", OrgID: "default", DeploymentID: "d-graph",
			ComponentName: "net", Cloud: "aws", Region: "us-east-1", Status: "succeeded",
			StartedAt: time.Now().UTC(),
		})
	})
	resp, body := get(t, srv, "/ui/deployments/d-graph/graph")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "<svg") {
		t.Errorf("response missing <svg>; body:\n%s", body)
	}
	if !strings.Contains(body, "graph-toolbar") {
		t.Errorf("response missing graph-toolbar; body:\n%s", body)
	}
}

func TestUI_ProjectGraph_NoDeploymentPlaceholder(t *testing.T) {
	// A project with no deployments should show the empty-state placeholder.
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-empty", OrgID: "default", Name: "emptyproject", CreatedAt: time.Now().UTC()})
	})
	resp, body := get(t, srv, "/ui/projects/p-empty/graph")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "nimbusfab plan") {
		t.Errorf("expected 'nimbusfab plan' placeholder copy; body:\n%s", body)
	}
}

func newTestServerWithSeedData(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newServer(t, func(ctx context.Context, r *sqlite.Repo) {
		_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "default", Name: "demo", CreatedAt: time.Now().UTC()})
		_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "default", ProjectID: "p-1", Name: "dev"})
		_ = r.Deployments().Create(ctx, inventory.Deployment{
			ID: "d-1", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
			Status: "succeeded", StartedAt: time.Now().UTC(),
		})
		netIR := `{"name":"net","type":"network","spec":{},"targets":[{"cloud":"aws","region":"us-east-1","credentialRef":"default"}]}`
		_ = r.Components().Upsert(ctx, inventory.Component{
			ID: "c-net", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
			Name: "net", Type: "network", IRJSON: []byte(netIR),
		})
		appIR := `{"name":"app","type":"compute","spec":{},"targets":[{"cloud":"aws","region":"us-east-1","credentialRef":"default"}],"refs":[{"component":"net","output":"vpc_id","as":"vpcId"}]}`
		_ = r.Components().Upsert(ctx, inventory.Component{
			ID: "c-app", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
			Name: "app", Type: "compute", IRJSON: []byte(appIR),
		})
	})
	return srv
}

func getWithCookie(t *testing.T, srv *httptest.Server, path, cookie string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func TestUI_DeploymentGraph_DirectionCookie(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()

	// Default (no cookie, no query) → top-down.
	resp, bodyTB := getWithCookie(t, srv, "/ui/deployments/d-1/graph", "")
	if resp.StatusCode != 200 {
		t.Fatalf("default status=%d", resp.StatusCode)
	}

	// With ?dir=lr → left-right. The SVG should have a different bounding
	// box (width and height swap roles).
	resp, bodyLR := getWithCookie(t, srv, "/ui/deployments/d-1/graph?dir=lr", "")
	if resp.StatusCode != 200 {
		t.Fatalf("?dir=lr status=%d", resp.StatusCode)
	}
	if bodyTB == bodyLR {
		t.Errorf("TB and LR responses are identical; toggle has no effect")
	}

	// Cookie nf_graph_dir=lr and no query → LR. Check the rendered toolbar
	// shows LR as active.
	_, bodyCookie := getWithCookie(t, srv, "/ui/deployments/d-1/graph", "nf_graph_dir=lr")
	if !strings.Contains(bodyCookie, `class="seg active" data-dir="lr"`) {
		t.Errorf("cookie path did not render LR active; body:\n%s", bodyCookie)
	}

	// Query param overrides cookie: cookie=lr but ?dir=tb → TB.
	_, bodyOverride := getWithCookie(t, srv, "/ui/deployments/d-1/graph?dir=tb", "nf_graph_dir=lr")
	if !strings.Contains(bodyOverride, `class="seg active" data-dir="tb"`) {
		t.Errorf("query override failed; body should have TB active:\n%s", bodyOverride)
	}
}
