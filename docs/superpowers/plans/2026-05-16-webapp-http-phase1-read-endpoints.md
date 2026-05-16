# Web App HTTP Phase 1 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** JSON `/api/v1/*` GET endpoints serving the same data UI Phase 1 displays as HTML. After Phase 1, programmatic clients (scripts, future GitOps daemon, future CLI-talking-to-web-api flow) can curl the API with optional bearer-token auth. Tight, focused phase; sets the JSON conventions that HTTP Phase 2 (mutations + SSE) layers on.

**Architecture:** New `internal/webapi/api` package — JSON handlers that read from `inventory.Repo` and emit `application/json`. New `internal/webapi/middleware/auth.go` — optional bearer-token middleware activated when `NIMBUSFAB_API_TOKEN` is set (Phase-1 stub for the real PAT data model coming in Auth Phase 1). Router gains `/api/v1/*` routes under the existing handler.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-webapp-http-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.
- JSON envelope: success → `{"data": ...}`; error → `{"error": {"code": "...", "message": "..."}}` (matches the web app spec).
- No new deps; std-lib `encoding/json` + `net/http`.

**Out of scope:**
- Mutating endpoints (HTTP Phase 2).
- SSE on `/api/v1/runs/{id}/events` (HTTP Phase 2).
- Real PAT data model — argon2 hashed, per-user, expirable (Auth Phase 1). Phase-1 token is a single string compared against `NIMBUSFAB_API_TOKEN`.
- Idempotency keys (HTTP Phase 2).
- Pagination / query filtering (Polish Phase 1; v1 row counts are small).
- Cost / parity / drift endpoints (Dashboards Phase 1 / Drift Phase 1).

---

## Task 1: Bearer-token middleware (optional)

**Files:**
- Create: `internal/webapi/middleware/auth.go`
- Create: `internal/webapi/middleware/auth_test.go`

- [ ] **Step 1: Middleware**

```go
package middleware

import (
    "net/http"
    "strings"
)

// BearerToken returns a middleware that rejects requests without
// "Authorization: Bearer <token>" matching the configured token. If the
// configured token is empty, the middleware is a no-op — useful for local
// dev / when running with NIMBUSFAB_AUTH_MODE=disabled.
//
// Phase-1 stub: one shared token, no per-user identity. Real PATs land
// in Auth Phase 1.
func BearerToken(token string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        if token == "" {
            return next
        }
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            header := r.Header.Get("Authorization")
            const prefix = "Bearer "
            if !strings.HasPrefix(header, prefix) {
                writeAPIError(w, http.StatusUnauthorized, "ErrUnauthorized", "Authorization header missing or malformed")
                return
            }
            if header[len(prefix):] != token {
                writeAPIError(w, http.StatusUnauthorized, "ErrUnauthorized", "invalid bearer token")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

// writeAPIError writes the standard JSON error envelope.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    _, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
```

- [ ] **Step 2: Tests**:

- `TestBearerToken_EmptyTokenIsNoop`: middleware constructed with `""` returns `next` directly; request without Authorization passes through.
- `TestBearerToken_MissingHeaderReturns401`: token set, no header → 401 + JSON error envelope.
- `TestBearerToken_WrongTokenReturns401`: header `Bearer wrong` → 401.
- `TestBearerToken_CorrectTokenPasses`: header `Bearer secret` → 200.

- [ ] **Step 3: Build + test + commit** `webapi: bearer-token middleware (Phase-1 stub)`

---

## Task 2: API handlers — read endpoints

**Files:**
- Create: `internal/webapi/api/api.go`
- Create: `internal/webapi/api/api_test.go`

- [ ] **Step 1: Handlers**

```go
package api

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// Handlers groups the JSON read handlers and their dependencies.
type Handlers struct {
    Repo  inventory.Repo
    OrgID string
}

// ListProjects → GET /api/v1/projects
func (h *Handlers) ListProjects(w http.ResponseWriter, r *http.Request) {
    projects, err := h.Repo.Projects().List(r.Context(), h.OrgID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "ErrInventory", err.Error())
        return
    }
    writeData(w, map[string]any{"projects": projectsJSON(projects)})
}

// GetProject → GET /api/v1/projects/{id}
func (h *Handlers) GetProject(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    p, err := h.Repo.Projects().Get(r.Context(), h.OrgID, id)
    if err != nil || p == nil {
        writeError(w, http.StatusNotFound, "ErrNotFound", "project not found: "+id)
        return
    }
    stacks, _ := h.Repo.Stacks().List(r.Context(), h.OrgID, id)
    var components []inventory.Component
    for _, s := range stacks {
        cs, _ := h.Repo.Components().ListByStack(r.Context(), h.OrgID, id, s.ID)
        components = append(components, cs...)
    }
    deployments, _ := h.Repo.Deployments().ListByProject(r.Context(), h.OrgID, id, 20)
    writeData(w, map[string]any{
        "project":     projectJSON(*p),
        "stacks":      stacksJSON(stacks),
        "components":  componentsJSON(components),
        "deployments": deploymentsJSON(deployments),
    })
}

// GetDeployment → GET /api/v1/deployments/{id}
func (h *Handlers) GetDeployment(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    d, err := h.Repo.Deployments().Get(r.Context(), h.OrgID, id)
    if err != nil || d == nil {
        writeError(w, http.StatusNotFound, "ErrNotFound", "deployment not found: "+id)
        return
    }
    targets, _ := h.Repo.DeploymentTargets().ListByDeployment(r.Context(), h.OrgID, id)
    writeData(w, map[string]any{
        "deployment": deploymentJSON(*d),
        "targets":    targetsJSON(targets),
    })
}

// GetRun → GET /api/v1/runs/{id}
func (h *Handlers) GetRun(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    run, err := h.Repo.Runs().Get(r.Context(), h.OrgID, id)
    if err != nil || run == nil {
        writeError(w, http.StatusNotFound, "ErrNotFound", "run not found: "+id)
        return
    }
    writeData(w, map[string]any{"run": runJSON(*run)})
}
```

The `*JSON` helpers convert inventory types to camelCase JSON-shaped maps so the API surface doesn't expose Go struct names directly. Helps with future schema evolution.

```go
func projectJSON(p inventory.Project) map[string]any {
    return map[string]any{
        "id": p.ID, "orgId": p.OrgID, "name": p.Name,
        "createdAt": p.CreatedAt.Format(time.RFC3339),
    }
}
// ... similar for stacks/components/deployments/targets/runs.
```

- [ ] **Step 2: writeData / writeError helpers**

```go
func writeData(w http.ResponseWriter, data any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    _ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]any{"code": code, "message": msg},
    })
}
```

- [ ] **Step 3: Tests** in `api_test.go` using seeded SQLite repo + `httptest.NewRecorder`:

- `TestListProjects_Empty`: empty repo → `{"data":{"projects":[]}}` (note: nil slice should serialize as `[]` not `null`; use a non-nil empty slice in `projectsJSON`).
- `TestListProjects_OneProject`: seeded project → JSON contains `"name":"demo"`.
- `TestGetProject_Found`: returns project + stacks + components + deployments.
- `TestGetProject_NotFound`: returns 404 + error envelope with code `ErrNotFound`.
- `TestGetDeployment_Found`: returns deployment + targets.
- `TestGetDeployment_NotFound`: 404.
- `TestGetRun_Found`: returns run.
- `TestGetRun_NotFound`: 404.
- `TestEnvelope_DataKey`: every success response has top-level `data` key (defensive against forgetting the envelope).

- [ ] **Step 4: Build + test + commit** `webapi: JSON GET handlers for projects/deployments/runs`

---

## Task 3: Mount /api/v1/* + thread bearer token through Config

**Files:**
- Edit: `internal/webapi/router.go`
- Edit: `internal/webapi/router_test.go`
- Edit: `cmd/server/main.go`

- [ ] **Step 1: Router**

```go
package webapi

import (
    "fmt"
    "net/http"

    "github.com/klehmer/nimbusfab/internal/webapi/api"
    "github.com/klehmer/nimbusfab/internal/webapi/middleware"
    "github.com/klehmer/nimbusfab/internal/webapi/ui"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type Config struct {
    Repo     inventory.Repo
    OrgID    string
    APIToken string // optional; empty = no auth on /api/v1/* (dev mode)
}

func New(cfg Config) (http.Handler, error) {
    // ... existing UI + assets + healthz setup ...

    apiHandlers := &api.Handlers{Repo: cfg.Repo, OrgID: cfg.OrgID}
    apiMux := http.NewServeMux()
    apiMux.HandleFunc("GET /api/v1/projects", apiHandlers.ListProjects)
    apiMux.HandleFunc("GET /api/v1/projects/{id}", apiHandlers.GetProject)
    apiMux.HandleFunc("GET /api/v1/deployments/{id}", apiHandlers.GetDeployment)
    apiMux.HandleFunc("GET /api/v1/runs/{id}", apiHandlers.GetRun)
    apiAuth := middleware.BearerToken(cfg.APIToken)
    mux.Handle("/api/v1/", apiAuth(apiMux))

    // ... existing routes ...

    return mux, nil
}
```

- [ ] **Step 2: Server wiring**

```go
// cmd/server/main.go: parse NIMBUSFAB_API_TOKEN.
apiToken := os.Getenv("NIMBUSFAB_API_TOKEN")
handler, err := webapi.New(webapi.Config{
    Repo: repo, OrgID: orgID, APIToken: apiToken,
})
// In the startup log: if apiToken == "", log a warning that the API is unauthenticated.
```

- [ ] **Step 3: Router tests** add end-to-end through-the-mux checks:

- `TestRouter_APIProjectsEmpty`: GET /api/v1/projects → 200 with JSON envelope.
- `TestRouter_APIProjectMissing`: GET /api/v1/projects/missing → 404 with error envelope.
- `TestRouter_APIWithToken_NoHeader_401`: server constructed with `APIToken: "secret"`, GET without header → 401.
- `TestRouter_APIWithToken_GoodHeader_200`: same, GET with `Authorization: Bearer secret` → 200.
- `TestRouter_UIUnaffectedByAPIToken`: UI routes (`/ui/projects`) work without bearer header even when APIToken is set (auth applies only to /api/v1/*).

- [ ] **Step 4: Build + test + commit** `webapi: mount /api/v1/* with optional bearer-token auth`

---

## Task 4: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: README** brief mention of new endpoints (`/api/v1/projects` etc.) and how to set `NIMBUSFAB_API_TOKEN`.

- [ ] **Step 2: CHANGELOG** entry under "Unreleased — Web App HTTP Phase 1":
  - JSON GET endpoints + JSON envelope conventions
  - Bearer-token middleware (Phase-1 stub; real PATs in Auth Phase 1)
  - Disabled-auth mode preserved (empty token = no auth)
  - Out-of-scope deferrals

- [ ] **Step 3: Final test + gofmt**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/webapi/ cmd/server/
```

- [ ] **Step 4: Commit** `docs: HTTP Phase 1 merged — JSON GET endpoints + bearer-token stub`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/webapp-http-phase1 -m "Merge feat/webapp-http-phase1: JSON GET endpoints"
git push origin main
git push origin feat/webapp-http-phase1
```
