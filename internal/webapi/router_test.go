package webapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi"
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
