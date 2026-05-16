# Inventory Phase 2 (Postgres Backend) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Postgres backend for `inventory.Repo`. After Phase 2, `nimbusfab-server` with `NIMBUSFAB_DB_DSN=postgres://...` runs the same UI + API as the SQLite path. SQLite stays the default for local dev (no Postgres dependency); Postgres unlocks multi-instance web-app deployment and concurrent reads/writes.

**Architecture:**
- New `internal/inventory/postgres` package mirroring `internal/inventory/sqlite/`:
  - `postgres.go` — `Open(dsn)` + `Migrate(ctx)` + accessor methods returning per-table repos.
  - One file per table (orgs / projects / stacks / components / deployments / targets / runs / drift).
  - `notwired.go` for the same `errCostEst` / `errRunLogs` / etc. stubs SQLite has.
- New `pkg/inventory/open.go` dispatcher: `inventory.Open(dsn) (Repo, error)` routes `sqlite:` → sqlite package, `postgres:` / `postgresql:` → postgres package.
- `cmd/server` switches to `inventory.Open` instead of `sqlite.Open` directly.
- `cmd/cli` likewise (currently always SQLite).
- Postgres driver: `github.com/jackc/pgx/v5/stdlib` — modern pgx via the `database/sql` interface so query code stays similar to SQLite's.
- Integration tests gated on `NIMBUSFAB_TEST_PG_DSN` env var; skip cleanly when unset (CI/local without Postgres still passes).

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-inventory-phase2/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- New `sessions` / `pats` / `idempotency_keys` tables — those land with Auth Phase 1 and HTTP Phase 2 polish; they're additive to the Repo interface, easier to add when the consumers are ready.
- Wiring `CostEstimates` / `RunLogs` / `CostActuals` / `AuditLog` for either backend — Phase 2 only mirrors what SQLite ships (those return `ErrNotImplementedYet`).
- Connection pooling tuning, prepared-statement caching, query timeouts beyond the default — Polish Phase 1.
- Schema migrations beyond `0001_init.sql` — that file already exists; we just run it via the existing `RunMigrations` runner.

---

## Task 1: pgx dependency + dispatcher

**Files:**
- Edit: `go.mod`, `go.sum` (via `go get`)
- Create: `pkg/inventory/open.go`
- Create: `pkg/inventory/open_test.go`

- [ ] **Step 1: Add pgx**

```bash
PATH=$HOME/.local/go/bin:$PATH go get github.com/jackc/pgx/v5/stdlib
```

- [ ] **Step 2: Dispatcher**

```go
// pkg/inventory/open.go
package inventory

import (
    "context"
    "fmt"
    "strings"
)

// Opener is the constructor signature each backend exposes. The backend
// packages register themselves via init() so the dispatcher avoids
// importing them directly (would create a cycle).
type Opener func(ctx context.Context, dsn string) (Repo, error)

var openers = map[string]Opener{}

// RegisterBackend wires a scheme prefix ("sqlite", "postgres") to its
// constructor. Backend packages call this from init().
func RegisterBackend(scheme string, fn Opener) {
    openers[scheme] = fn
}

// Open opens a Repo using the backend matching the DSN scheme prefix.
// Recognizes "sqlite:", "postgres:", "postgresql:".
func Open(ctx context.Context, dsn string) (Repo, error) {
    scheme := schemeOf(dsn)
    fn, ok := openers[scheme]
    if !ok {
        return nil, fmt.Errorf("inventory: no backend registered for scheme %q (dsn=%s)", scheme, dsn)
    }
    return fn(ctx, dsn)
}

func schemeOf(dsn string) string {
    if i := strings.Index(dsn, ":"); i > 0 {
        s := dsn[:i]
        if s == "postgresql" {
            return "postgres"
        }
        return s
    }
    return ""
}
```

- [ ] **Step 3: Tests**

- `TestOpen_UnknownScheme`: `Open(ctx, "mysql://foo")` returns error mentioning the scheme.
- `TestOpen_DispatchesByScheme`: stub two backends via `RegisterBackend`; assert correct one is called.
- `TestSchemeOf_PostgreSQLAlias`: `postgresql:` → `"postgres"`.

- [ ] **Step 4: Build + test + commit** `inventory: dispatcher (Open) + pgx dep`

---

## Task 2: Postgres backend scaffold

**Files:**
- Create: `internal/inventory/postgres/postgres.go`
- Create: `internal/inventory/postgres/postgres_test.go`

- [ ] **Step 1: Scaffold**

```go
// Package postgres implements pkg/inventory.Repo against Postgres via
// github.com/jackc/pgx/v5/stdlib.
package postgres

import (
    "context"
    "database/sql"
    "fmt"

    _ "github.com/jackc/pgx/v5/stdlib"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// Repo is the Postgres Repo implementation.
type Repo struct {
    db *sql.DB
}

// Open returns a Postgres Repo from a DSN. Accepts both
// "postgres://user:pass@host:port/db?sslmode=disable" and
// "postgresql://..." forms; pgx parses both.
func Open(dsn string) (*Repo, error) {
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        return nil, fmt.Errorf("postgres open: %w", err)
    }
    return &Repo{db: db}, nil
}

func (r *Repo) Migrate(ctx context.Context) error {
    return inventory.RunMigrations(ctx, r.db, inventory.FlavorPostgres)
}

func (r *Repo) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }
func (r *Repo) Close() error                   { return r.db.Close() }

var _ inventory.Repo = (*Repo)(nil)

func (r *Repo) Orgs() inventory.OrgRepo                           { return &orgRepo{db: r.db} }
func (r *Repo) Users() inventory.UserRepo                         { return errUsers{} }
func (r *Repo) Projects() inventory.ProjectRepo                   { return &projectRepo{db: r.db} }
func (r *Repo) Stacks() inventory.StackRepo                       { return &stackRepo{db: r.db} }
func (r *Repo) Components() inventory.ComponentRepo               { return &componentRepo{db: r.db} }
func (r *Repo) Compositions() inventory.CompositionRepo           { return errCompositions{} }
func (r *Repo) Deployments() inventory.DeploymentRepo             { return &deploymentRepo{db: r.db} }
func (r *Repo) DeploymentTargets() inventory.DeploymentTargetRepo { return &targetRepo{db: r.db} }
func (r *Repo) Runs() inventory.RunRepo                           { return &runRepo{db: r.db} }
func (r *Repo) RunLogs() inventory.RunLogRepo                     { return errRunLogs{} }
func (r *Repo) DriftStatus() inventory.DriftStatusRepo            { return &driftRepo{db: r.db} }
func (r *Repo) CostEstimates() inventory.CostEstimateRepo         { return errCostEst{} }
func (r *Repo) CostActuals() inventory.CostActualRepo             { return errCostAct{} }
func (r *Repo) SecretsRefs() inventory.SecretsRefRepo             { return errSecrets{} }
func (r *Repo) AuditLog() inventory.AuditLogRepo                  { return errAudit{} }

// init registers postgres with the dispatcher.
func init() {
    inventory.RegisterBackend("postgres", func(ctx context.Context, dsn string) (inventory.Repo, error) {
        r, err := Open(dsn)
        if err != nil {
            return nil, err
        }
        if err := r.Migrate(ctx); err != nil {
            _ = r.Close()
            return nil, err
        }
        return r, nil
    })
}
```

- [ ] **Step 2: Stub the per-table repos so the file compiles**

Add `notwired.go` mirroring SQLite's (errUsers, errCompositions, errRunLogs, errCostEst, errCostAct, errSecrets, errAudit). These return ErrNotImplementedYet for every method.

Stub the real repo types (orgRepo, projectRepo, stackRepo, componentRepo, deploymentRepo, targetRepo, runRepo, driftRepo) with minimal struct definitions in their own files (orgs.go, projects.go, etc.) — methods filled in Task 3.

For Task 2 the goal is "package compiles, registers, opens a connection". Real query code is Task 3.

- [ ] **Step 3: Skip-on-no-PG test scaffold**

```go
// postgres_test.go
package postgres_test

import (
    "context"
    "os"
    "testing"

    "github.com/klehmer/nimbusfab/internal/inventory/postgres"
)

// pgDSN returns the test Postgres DSN or skips the test. CI without
// Postgres + local dev without docker run pass cleanly.
func pgDSN(t *testing.T) string {
    t.Helper()
    dsn := os.Getenv("NIMBUSFAB_TEST_PG_DSN")
    if dsn == "" {
        t.Skip("set NIMBUSFAB_TEST_PG_DSN to run Postgres integration tests")
    }
    return dsn
}

func TestPostgres_OpenAndMigrate(t *testing.T) {
    dsn := pgDSN(t)
    r, err := postgres.Open(dsn)
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    defer r.Close()
    if err := r.Ping(context.Background()); err != nil {
        t.Fatalf("Ping: %v (is Postgres running at %s?)", err, dsn)
    }
    if err := r.Migrate(context.Background()); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
}
```

- [ ] **Step 4: Build + test + commit** `inventory/postgres: package scaffold + dispatcher registration`

---

## Task 3: Per-table repo implementations

**Files:**
- Create: `internal/inventory/postgres/{orgs,projects,stacks,components,deployments,targets,runs,drift,time,notwired}.go`
- Edit: `internal/inventory/postgres/postgres_test.go` (add CRUD round-trip test)

- [ ] **Step 1: Port queries**

For each table, port the SQLite version with three changes:
- Replace `?` placeholders with `$1, $2, $3, ...`
- Drop the `mustParseTime(createdAt)` dance — pgx scans `TIMESTAMPTZ` directly into `time.Time`
- Convert SQLite `INSERT OR REPLACE` / `INSERT INTO ... ON CONFLICT DO UPDATE` syntax to Postgres `INSERT ... ON CONFLICT (...) DO UPDATE SET ...`

The Postgres schema uses `UUID` columns; pgx scans them into Go `string` fine (the lib/pq style works).

Skeleton for `orgs.go`:

```go
type orgRepo struct{ db *sql.DB }

func (r *orgRepo) Get(ctx context.Context, id string) (*inventory.Org, error) {
    var o inventory.Org
    err := r.db.QueryRowContext(ctx,
        "SELECT id, name, created_at FROM orgs WHERE id = $1", id).
        Scan(&o.ID, &o.Name, &o.CreatedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("orgs.Get: %w", err)
    }
    return &o, nil
}

func (r *orgRepo) List(ctx context.Context) ([]inventory.Org, error) { ... }
func (r *orgRepo) Create(ctx context.Context, o inventory.Org) error { ... }
```

Repeat for each table. For deployments / targets / runs, the SQLite repos handle `*time.Time` nullable fields with `sql.NullString`; Postgres can use `sql.NullTime` directly which is cleaner.

- [ ] **Step 2: CRUD round-trip test**

```go
// In postgres_test.go:
func TestPostgres_CRUDRoundTrip(t *testing.T) {
    dsn := pgDSN(t)
    r, err := postgres.Open(dsn)
    if err != nil { t.Fatalf("Open: %v", err) }
    defer r.Close()
    if err := r.Migrate(context.Background()); err != nil { t.Fatalf("Migrate: %v", err) }

    // Cleanup namespace via a unique org per test.
    orgID := uuid.NewString()
    ctx := context.Background()
    t.Cleanup(func() {
        // CASCADE delete via the org → everything else.
        _, _ = r.db.ExecContext(ctx, "DELETE FROM orgs WHERE id = $1", orgID)
    })

    // ... create org, project, stack, component, deployment, target, run, drift;
    //     assert reads return the same shapes.
}
```

(Use the same shape as `internal/inventory/sqlite/crud_test.go` for parallel coverage.)

- [ ] **Step 3: Build + test + commit** `inventory/postgres: per-table repo implementations + CRUD round-trip test`

---

## Task 4: Wire dispatcher into cmd/server + cmd/cli

**Files:**
- Edit: `cmd/server/main.go` (use `inventory.Open` instead of `sqlite.Open`)
- Edit: any CLI command that opens a repo
- Edit: maybe `pkg/inventory/open.go` — add SQLite auto-registration

The CLI currently constructs SQLite directly (search `sqlite.Open(`); a richer phase would dispatch through `inventory.Open`. For Phase 2 keep CLI simple: SQLite-only (CLI is a dev tool; a server is the multi-instance case). Just `cmd/server` needs the dispatcher.

- [ ] **Step 1: Register sqlite in its package init**

```go
// internal/inventory/sqlite/sqlite.go init():
func init() {
    inventory.RegisterBackend("sqlite", func(ctx context.Context, dsn string) (inventory.Repo, error) {
        r, err := Open(dsn)
        if err != nil {
            return nil, err
        }
        if err := r.Migrate(ctx); err != nil {
            _ = r.Close()
            return nil, err
        }
        return r, nil
    })
}
```

- [ ] **Step 2: cmd/server switches**

```go
// import "_ github.com/klehmer/nimbusfab/internal/inventory/sqlite" for init side-effect
// import "_ github.com/klehmer/nimbusfab/internal/inventory/postgres"

// openRepo:
func openRepo(ctx context.Context, dsn string) (inventory.Repo, error) {
    return inventory.Open(ctx, dsn)
}
```

- [ ] **Step 3: Smoke + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
# Smoke: nimbusfab-server still works against sqlite default DSN
git add cmd/server/main.go internal/inventory/sqlite/sqlite.go pkg/inventory/open.go
git commit -m "server: inventory.Open dispatcher chooses sqlite/postgres by DSN"
```

---

## Task 5: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: README** brief mention: `NIMBUSFAB_DB_DSN=postgres://...` for production deployments.

- [ ] **Step 2: CHANGELOG** entry:
  - `internal/inventory/postgres` package (pgx/v5 stdlib driver)
  - `inventory.Open(dsn)` dispatcher
  - SQLite remains default for local dev
  - Integration tests gated on `NIMBUSFAB_TEST_PG_DSN`
  - Out of scope deferrals

- [ ] **Step 3: Final test + gofmt**

- [ ] **Step 4: Commit** `docs: Inventory Phase 2 merged — Postgres backend`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/inventory-phase2 -m "Merge feat/inventory-phase2: Postgres backend"
git push origin main
git push origin feat/inventory-phase2
```
