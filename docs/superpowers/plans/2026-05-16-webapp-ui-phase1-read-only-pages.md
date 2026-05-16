# Web App UI Phase 1 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** A working read-only web UI. After UI Phase 1, `nimbusfab-server` serves browser-rendered pages at `/ui/projects`, `/ui/projects/{id}`, `/ui/deployments/{id}`, and `/ui/runs/{id}` that read from the inventory repo (SQLite or Postgres). A user can `nimbusfab plan` against a project (inventory enabled) and then visit the server in their browser to see what was planned. No live updates, no mutating actions — those land in HTTP Phase 2 / UI Phase 2.

**Architecture:** New `internal/webapi/ui` package — page handlers + html/template files + embedded CSS. New `internal/webapi/router` that mounts `/ui/*`, `/assets/*`, and the existing `/healthz`. `cmd/server/main.go` constructs an `inventory.Repo` (SQLite by default) and an `engine.Engine`, then mounts the router. Auth is disabled in this phase (`NIMBUSFAB_AUTH_MODE=disabled` is the only supported mode; OIDC lands in Auth Phase 1).

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-webapp-ui-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.
- No new heavyweight dependencies; std-lib `net/http` router via `http.ServeMux` (Go 1.22's pattern routing is sufficient — chi can land in HTTP Phase 1 if needed).

**Out of scope:**
- Mutating endpoints (POST applies/destroys/drifts).
- SSE live log streaming.
- OIDC / cookie sessions / PATs (Auth Phase 1).
- Cost dashboard, parity overview, drift overview pages.
- Audit log writes (no mutations to audit yet).
- Pagination (load all rows; v1 deployment volume is small).

---

## Task 1: Package scaffold + base layout + minimal CSS

**Files:**
- Create: `internal/webapi/ui/templates/layout.html`
- Create: `internal/webapi/ui/templates/error.html`
- Create: `internal/webapi/ui/assets/style.css`
- Create: `internal/webapi/ui/pages.go` (renderer + template loader + asset embed)
- Create: `internal/webapi/ui/pages_test.go`

- [ ] **Step 1: Embed templates and assets**

```go
// internal/webapi/ui/pages.go
package ui

import (
    "embed"
    "fmt"
    "html/template"
    "io"
    "io/fs"
    "net/http"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets
var assetsFS embed.FS

// AssetsFS returns the embedded /assets/* sub-filesystem.
func AssetsFS() (fs.FS, error) {
    return fs.Sub(assetsFS, "assets")
}

// Renderer holds parsed templates and the engine deps page handlers need.
type Renderer struct {
    Repo  inventory.Repo
    OrgID string

    tmpl *template.Template
}

// NewRenderer parses every template under templates/. Templates share the
// "layout" base; page templates define {{block "content"}} and {{block "title"}}.
func NewRenderer(repo inventory.Repo, orgID string) (*Renderer, error) {
    t, err := template.New("").Funcs(funcMap()).ParseFS(templatesFS, "templates/*.html")
    if err != nil {
        return nil, fmt.Errorf("ui: parse templates: %w", err)
    }
    return &Renderer{Repo: repo, OrgID: orgID, tmpl: t}, nil
}

// render writes one page derived from layout.html using the named page template.
func (r *Renderer) render(w http.ResponseWriter, page string, data any) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    if err := r.tmpl.ExecuteTemplate(w, page, data); err != nil {
        // Last-resort: layout itself or page template missing.
        http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
    }
}

func (r *Renderer) renderError(w http.ResponseWriter, status int, msg string) {
    w.WriteHeader(status)
    r.render(w, "error.html", map[string]any{
        "Status":  status,
        "Message": msg,
    })
}

// funcMap exposes a few helpers to templates.
func funcMap() template.FuncMap {
    return template.FuncMap{
        "humanStatus": humanStatus,
        "shortID":     func(id string) string {
            if len(id) > 12 { return id[:12] + "…" }
            return id
        },
    }
}

func humanStatus(s string) string {
    switch s {
    case "succeeded": return "✅ succeeded"
    case "failed": return "❌ failed"
    case "partial_failure": return "⚠️ partial"
    case "planned": return "📋 planned"
    case "running": return "🔄 running"
    }
    return s
}
```

- [ ] **Step 2: Layout template**

```html
<!-- internal/webapi/ui/templates/layout.html -->
{{define "layout"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>{{block "title" .}}nimbusfab{{end}}</title>
  <link rel="stylesheet" href="/assets/style.css">
</head>
<body>
  <header class="topbar">
    <a class="brand" href="/ui/projects">nimbusfab</a>
    <nav>
      <a href="/ui/projects">Projects</a>
    </nav>
  </header>
  <main class="container">
    {{block "content" .}}{{end}}
  </main>
  <footer class="footer">UI Phase 1 — read-only</footer>
</body>
</html>
{{end}}
```

- [ ] **Step 3: Error template**

```html
<!-- internal/webapi/ui/templates/error.html -->
{{define "error.html"}}
{{template "layout" .}}
{{end}}

{{define "title"}}Error {{.Status}}{{end}}
{{define "content"}}
<h1>Error {{.Status}}</h1>
<p class="error">{{.Message}}</p>
<p><a href="/ui/projects">← back to projects</a></p>
{{end}}
```

- [ ] **Step 4: Minimal CSS** in `assets/style.css` — system fonts, simple table, header strip, footer. Target ~3 KB. Hand-author.

```css
* { box-sizing: border-box; }
body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; line-height: 1.5; color: #1a1a1a; }
.topbar { background: #1a1a1a; color: #fff; padding: 0.75rem 1.5rem; display: flex; align-items: center; gap: 2rem; }
.brand { color: #fff; text-decoration: none; font-weight: 700; font-size: 1.1rem; }
.topbar nav a { color: #ddd; text-decoration: none; margin-right: 1rem; }
.topbar nav a:hover { color: #fff; }
.container { max-width: 1100px; margin: 1.5rem auto; padding: 0 1.5rem; }
h1, h2, h3 { margin-top: 1.5rem; }
table { width: 100%; border-collapse: collapse; margin: 1rem 0; }
th, td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid #eee; }
th { background: #fafafa; font-weight: 600; }
a { color: #2266cc; text-decoration: none; }
a:hover { text-decoration: underline; }
.badge { display: inline-block; padding: 0.1rem 0.5rem; border-radius: 0.25rem; font-size: 0.85em; background: #eef; }
.badge.ok { background: #d6f3d6; color: #1a5a1a; }
.badge.fail { background: #f9d6d6; color: #7a1a1a; }
.badge.warn { background: #fcebd0; color: #7a4a1a; }
.error { color: #c33; background: #fff0f0; padding: 1rem; border-left: 4px solid #c33; }
.footer { text-align: center; color: #999; padding: 2rem 1rem; font-size: 0.85rem; }
.kv { display: grid; grid-template-columns: 200px 1fr; gap: 0.5rem 1rem; margin: 1rem 0; }
.kv dt { color: #555; }
.kv dd { margin: 0; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
```

- [ ] **Step 5: Test** that NewRenderer parses without error and emits a non-empty asset filesystem.

- [ ] **Step 6: Build + test + commit** `ui: scaffold templates, layout, embedded CSS`

---

## Task 2: Projects list + detail pages

**Files:**
- Create: `internal/webapi/ui/templates/projects.html`
- Create: `internal/webapi/ui/templates/project_detail.html`
- Edit: `internal/webapi/ui/pages.go` (add `ListProjects`, `ProjectDetail` handlers)
- Edit: `internal/webapi/ui/pages_test.go`

- [ ] **Step 1: Handlers**

```go
// In pages.go
func (r *Renderer) ListProjects(w http.ResponseWriter, req *http.Request) {
    ctx := req.Context()
    projects, err := r.Repo.Projects().List(ctx, r.OrgID)
    if err != nil {
        r.renderError(w, 500, "list projects: "+err.Error())
        return
    }
    r.render(w, "projects.html", map[string]any{"Projects": projects})
}

func (r *Renderer) ProjectDetail(w http.ResponseWriter, req *http.Request) {
    ctx := req.Context()
    id := req.PathValue("id")
    p, err := r.Repo.Projects().Get(ctx, r.OrgID, id)
    if err != nil || p == nil {
        r.renderError(w, 404, "project not found: "+id)
        return
    }
    stacks, _ := r.Repo.Stacks().List(ctx, r.OrgID, id)
    var allComponents []inventory.Component
    for _, s := range stacks {
        comps, _ := r.Repo.Components().ListByStack(ctx, r.OrgID, id, s.ID)
        allComponents = append(allComponents, comps...)
    }
    deployments, _ := r.Repo.Deployments().ListByProject(ctx, r.OrgID, id, 20)
    r.render(w, "project_detail.html", map[string]any{
        "Project":     p,
        "Stacks":      stacks,
        "Components":  allComponents,
        "Deployments": deployments,
    })
}
```

- [ ] **Step 2: projects.html**

```html
{{define "projects.html"}}{{template "layout" .}}{{end}}
{{define "title"}}Projects{{end}}
{{define "content"}}
<h1>Projects</h1>
{{if .Projects}}
<table>
  <thead><tr><th>Name</th><th>ID</th><th>Created</th></tr></thead>
  <tbody>
  {{range .Projects}}
  <tr>
    <td><a href="/ui/projects/{{.ID}}">{{.Name}}</a></td>
    <td><code>{{shortID .ID}}</code></td>
    <td>{{.CreatedAt.Format "2006-01-02"}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
{{else}}
<p>No projects registered yet. Run <code>nimbusfab plan</code> with inventory enabled to register one.</p>
{{end}}
{{end}}
```

- [ ] **Step 3: project_detail.html** with three sections: components (grouped table), stacks, recent deployments (table with link to deployment detail and status badge).

- [ ] **Step 4: Test** using `httptest.NewRecorder` against a seeded SQLite repo. Three cases:
  - Empty repo → projects page shows the "no projects" copy.
  - Seeded project → projects page lists it with a link.
  - GET `/ui/projects/{id}` → renders the project's name + stacks + components in the HTML.
  - GET `/ui/projects/missing-id` → 404 with error.html.

- [ ] **Step 5: Build + test + commit** `ui: projects list + detail pages`

---

## Task 3: Deployment detail + run detail pages

**Files:**
- Create: `internal/webapi/ui/templates/deployment_detail.html`
- Create: `internal/webapi/ui/templates/run_detail.html`
- Edit: `internal/webapi/ui/pages.go` (`DeploymentDetail`, `RunDetail`)
- Edit: `internal/webapi/ui/pages_test.go`

- [ ] **Step 1: Handlers**

```go
func (r *Renderer) DeploymentDetail(w http.ResponseWriter, req *http.Request) {
    ctx := req.Context()
    id := req.PathValue("id")
    d, err := r.Repo.Deployments().Get(ctx, r.OrgID, id)
    if err != nil || d == nil {
        r.renderError(w, 404, "deployment not found: "+id)
        return
    }
    targets, _ := r.Repo.DeploymentTargets().ListByDeployment(ctx, r.OrgID, id)
    // For each target, list its runs (small N expected per Phase 1).
    type targetWithRuns struct {
        T    inventory.DeploymentTarget
        Runs []inventory.Run
    }
    enriched := make([]targetWithRuns, 0, len(targets))
    for _, t := range targets {
        runs, _ := r.Repo.Runs().ListByDeploymentTarget(ctx, r.OrgID, t.ID)
        enriched = append(enriched, targetWithRuns{T: t, Runs: runs})
    }
    r.render(w, "deployment_detail.html", map[string]any{
        "Deployment": d,
        "Targets":    enriched,
    })
}

func (r *Renderer) RunDetail(w http.ResponseWriter, req *http.Request) {
    ctx := req.Context()
    id := req.PathValue("id")
    run, err := r.Repo.Runs().Get(ctx, r.OrgID, id)
    if err != nil || run == nil {
        r.renderError(w, 404, "run not found: "+id)
        return
    }
    r.render(w, "run_detail.html", map[string]any{
        "Run": run,
    })
}
```

- [ ] **Step 2: Templates**

`deployment_detail.html`: kv list (deployment ID, project ID, stack ID, status badge, started/finished), then per-target table with columns (Component, Cloud, Region, Status, Runs link).

`run_detail.html`: kv list (run ID, kind, status, exit code, started/finished, deployment target ID). Phase 1 doesn't show live logs; placeholder note: "Live log streaming lands in UI Phase 2."

- [ ] **Step 3: Test**
  - Seeded deployment with 2 targets and 1 run each → page lists both targets and links to each run.
  - Run detail → renders fields.
  - 404 paths for missing IDs.

- [ ] **Step 4: Build + test + commit** `ui: deployment + run detail pages`

---

## Task 4: Router + server wiring

**Files:**
- Create: `internal/webapi/router.go`
- Create: `internal/webapi/router_test.go`
- Edit: `cmd/server/main.go`

- [ ] **Step 1: Router**

```go
package webapi

import (
    "io/fs"
    "net/http"

    "github.com/klehmer/nimbusfab/internal/webapi/ui"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// Config carries dependencies the router needs.
type Config struct {
    Repo  inventory.Repo
    OrgID string // until Auth Phase 1; "default" in disabled-auth mode
}

// New returns an http.Handler mounting all UI Phase 1 routes plus /healthz.
func New(cfg Config) (http.Handler, error) {
    renderer, err := ui.NewRenderer(cfg.Repo, cfg.OrgID)
    if err != nil {
        return nil, err
    }
    assets, err := ui.AssetsFS()
    if err != nil {
        return nil, err
    }
    mux := http.NewServeMux()
    mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
    mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
        _, _ = w.Write([]byte("ok"))
    })
    mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
        if err := cfg.Repo.Orgs(); err == nil {
            _, _ = w.Write([]byte("ready"))
            return
        }
        http.Error(w, "not ready", http.StatusServiceUnavailable)
    })
    mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, "/ui/projects", http.StatusFound)
    })
    mux.HandleFunc("GET /ui/projects", renderer.ListProjects)
    mux.HandleFunc("GET /ui/projects/{id}", renderer.ProjectDetail)
    mux.HandleFunc("GET /ui/deployments/{id}", renderer.DeploymentDetail)
    mux.HandleFunc("GET /ui/runs/{id}", renderer.RunDetail)
    return mux, nil
}
```

(`/readyz` check is intentionally light for Phase 1: just confirm the inventory repo accessor returns. Real readiness — DB ping, OIDC discovery, etc. — comes with later phases.)

- [ ] **Step 2: Server wiring**

```go
// cmd/server/main.go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
    "github.com/klehmer/nimbusfab/internal/webapi"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := run(ctx); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run(ctx context.Context) error {
    addr := envDefault("NIMBUSFAB_LISTEN_ADDR", ":8080")
    dsn := envDefault("NIMBUSFAB_DB_DSN", "sqlite:./nimbusfab.db")
    orgID := envDefault("NIMBUSFAB_ORG_ID", "default")

    repo, err := openRepo(ctx, dsn)
    if err != nil {
        return fmt.Errorf("open repo (%s): %w", dsn, err)
    }

    handler, err := webapi.New(webapi.Config{Repo: repo, OrgID: orgID})
    if err != nil {
        return err
    }
    srv := &http.Server{
        Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
    }
    errCh := make(chan error, 1)
    go func() {
        fmt.Printf("nimbusfab-server listening on %s (UI Phase 1; auth disabled; org=%s)\n", addr, orgID)
        errCh <- srv.ListenAndServe()
    }()
    select {
    case <-ctx.Done():
        sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        return srv.Shutdown(sCtx)
    case err := <-errCh:
        if err == http.ErrServerClosed {
            return nil
        }
        return err
    }
}

func openRepo(ctx context.Context, dsn string) (inventory.Repo, error) {
    // Phase 1: SQLite only. Postgres branches once Inventory Phase 2 lands.
    r, err := sqlite.Open(dsn)
    if err != nil {
        return nil, err
    }
    if err := r.Migrate(ctx); err != nil {
        _ = r.Close()
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return r, nil
}

func envDefault(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}
```

- [ ] **Step 3: Router tests** using `httptest.NewServer`:
  - `GET /healthz` → 200 "ok"
  - `GET /` → 302 to `/ui/projects`
  - `GET /assets/style.css` → 200 with `text/css` content
  - `GET /ui/projects` → 200 HTML containing "Projects"
  - `GET /ui/projects/{seeded-id}` → 200 HTML containing the project name
  - `GET /ui/projects/missing` → 404 with error.html

- [ ] **Step 4: Build + test + commit** `webapi: mount UI routes + assets; cmd/server constructs SQLite-backed repo`

---

## Task 5: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: README status line** add "UI Phase 1 (read-only pages) merged. Run `nimbusfab-server` against a SQLite inventory to view projects/deployments/runs in a browser."

- [ ] **Step 2: CHANGELOG entry** under "Unreleased — Web App UI Phase 1":
  - `internal/webapi/ui` package with templates + embedded CSS
  - 4 read-only pages: projects list, project detail, deployment detail, run detail
  - `internal/webapi/router.go` mounts UI + `/healthz` + `/readyz` + `/`-redirect
  - `cmd/server` now constructs a real SQLite-backed inventory and serves the router
  - Disabled auth (`org_id="default"` baked in); OIDC + cookies land in Auth Phase 1
  - Out of scope deferrals (mutating endpoints → HTTP Phase 2; SSE → UI Phase 2; etc.)

- [ ] **Step 3: Final test + gofmt**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/webapi/ cmd/server/
```

- [ ] **Step 4: Commit** `docs: UI Phase 1 merged — read-only web pages over inventory`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/webapp-ui-phase1 -m "Merge feat/webapp-ui-phase1: read-only web UI"
git push origin main
git push origin feat/webapp-ui-phase1
```
