# Inventory Phase 1 — SQLite Impl + Engine Wiring

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land a working SQLite-backed inventory that persists `Plan` output and lets `Apply` / `Destroy` / `DetectDrift` operate by deployment ID across process boundaries. After Phase 1, `nimbusfab plan --stack dev` returns a deployment ID, and `nimbusfab apply <deployment-id>` (run later, possibly from another shell) deploys it — with full audit rows for project / stack / deployment / deployment_target / run. No-inventory mode (`--no-inventory`) continues to work for CI flows.

**Architecture:** Three new layers under `pkg/inventory` and `internal/inventory/sqlite`. (1) **Migration runner** (`pkg/inventory/migrations.go`) embeds `*.sql` files via `//go:embed`, applies them in order, records applied versions in `schema_migrations`. (2) **SQLite repo** (`internal/inventory/sqlite`) implements every sub-repo in `pkg/inventory.Repo` against `modernc.org/sqlite`. (3) **Null repo** (`pkg/inventory/nullrepo.go`) implements `Repo` with no-op writes and `ErrInventoryRequired` reads, used in `--no-inventory` mode. The engine surface (`pkg/engine`) gains inventory-aware paths: `Plan` persists, `Apply(planID)` / `Destroy(deploymentID)` / `DetectDrift(deploymentID)` look up.

**Tech Stack:**
- Go 1.22 (existing)
- `modernc.org/sqlite@latest` (NEW; CGo-free SQLite driver)
- `github.com/google/uuid` (existing; v5 deterministic UUIDs for idempotency)
- Standard library `database/sql`, `embed`, `encoding/json`

**Conventions used throughout this plan:**
- All file paths are relative to the repo root.
- Run all `go` commands with `PATH=$HOME/.local/go/bin:$PATH` prefix.
- The Bash tool does NOT persist `cd` between calls — use absolute paths or `cd /home/kurt/git/nimbusfab-inventory-phase1 && <cmd>` chains.
- Each Task ends with a commit.
- Phase 1 implements SQLite ONLY; Postgres is a future phase. The contract is the same so Postgres slots in cleanly.
- Phase 1 implements the SUBSET of repos that `Plan`/`Apply`/`Destroy`/`DetectDrift` actually need: Org, Project, Stack, Component, Deployment, DeploymentTarget, Run, DriftStatus. The other sub-repos (User, Composition, RunLog, CostEstimate, CostActual, SecretsRef, AuditLog) get null implementations that return `ErrNotImplementedYet` for now; they get wired by their owning phases.

**Out of scope for Phase 1 (deferred):**
- Postgres impl. Future phase.
- Web auth / api_tokens / OIDC. Web app spec.
- Run log persistence. Server phase (the table exists in the schema; nothing writes to it in Phase 1).
- Cost actuals / estimates write paths. Cost specs.
- Background drift schedules. GitOps daemon spec.
- Postgres `cost_actuals.tags_json` indexing. v2.
- `nimbusfab runs status` / `deployments list` CLI commands. Phase 1 surfaces inventory through existing commands only; the introspection commands land alongside the web app.

---

## Task 1: Add modernc.org/sqlite dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dep**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Verify build**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go build ./...
```

- [ ] **Step 3: Commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add go.mod go.sum && git commit -m "deps: add modernc.org/sqlite (CGo-free)"
```

---

## Task 2: Define inventory errors + nullRepo

**Files:**
- Create: `pkg/inventory/errors.go`
- Create: `pkg/inventory/nullrepo.go`
- Create: `pkg/inventory/nullrepo_test.go`

- [ ] **Step 1: Define stable errors**

Create `pkg/inventory/errors.go`:

```go
package inventory

import "errors"

// ErrInventoryRequired is returned by read paths when no inventory is
// configured. Engine surfaces wrap this into UserFacing errors.
var ErrInventoryRequired = errors.New("inventory: required for this operation but not configured")

// ErrInventoryUnavailable is returned when inventory IS configured but the
// underlying DB is unreachable. Distinguish from ErrInventoryRequired so
// the CLI can suggest different remediations.
var ErrInventoryUnavailable = errors.New("inventory: unavailable (check DSN / DB liveness)")

// ErrDeploymentNotFound is returned by Apply/Destroy/Drift when the given
// deployment ID doesn't exist for the requested org.
var ErrDeploymentNotFound = errors.New("inventory: deployment not found")

// ErrDeploymentWrongStatus is returned by Apply when the deployment is not
// in the expected lifecycle state (e.g., trying to Apply an already-applied
// deployment).
var ErrDeploymentWrongStatus = errors.New("inventory: deployment is not in the expected status")

// ErrMigrationConflict is returned by Migrate when applied versions don't
// match expectations (typically: a migration was deleted on disk).
var ErrMigrationConflict = errors.New("inventory: migration conflict")

// ErrNotImplementedYet is returned by sub-repo methods that exist in the
// interface but aren't wired in Phase 1.
var ErrNotImplementedYet = errors.New("inventory: not implemented yet")
```

- [ ] **Step 2: Write failing test for nullRepo**

Create `pkg/inventory/nullrepo_test.go`:

```go
package inventory_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestNullRepo_WritesAreNoOp(t *testing.T) {
    r := inventory.NewNullRepo()
    ctx := context.Background()

    if err := r.Migrate(ctx); err != nil {
        t.Errorf("Migrate: %v", err)
    }
    if err := r.Ping(ctx); err != nil {
        t.Errorf("Ping: %v", err)
    }
    if err := r.Orgs().Create(ctx, inventory.Org{ID: "x"}); err != nil {
        t.Errorf("Orgs.Create: %v", err)
    }
    if err := r.Projects().Create(ctx, inventory.Project{ID: "x"}); err != nil {
        t.Errorf("Projects.Create: %v", err)
    }
    if err := r.Deployments().Create(ctx, inventory.Deployment{ID: "x", StartedAt: time.Now()}); err != nil {
        t.Errorf("Deployments.Create: %v", err)
    }
    if err := r.Close(); err != nil {
        t.Errorf("Close: %v", err)
    }
}

func TestNullRepo_ReadsReturnRequired(t *testing.T) {
    r := inventory.NewNullRepo()
    ctx := context.Background()

    if _, err := r.Orgs().Get(ctx, "x"); !errors.Is(err, inventory.ErrInventoryRequired) {
        t.Errorf("Orgs.Get: want ErrInventoryRequired, got %v", err)
    }
    if _, err := r.Projects().Get(ctx, "x", "y"); !errors.Is(err, inventory.ErrInventoryRequired) {
        t.Errorf("Projects.Get: want ErrInventoryRequired, got %v", err)
    }
    if _, err := r.Deployments().Get(ctx, "x", "y"); !errors.Is(err, inventory.ErrInventoryRequired) {
        t.Errorf("Deployments.Get: want ErrInventoryRequired, got %v", err)
    }
}
```

- [ ] **Step 3: Implement nullRepo**

Create `pkg/inventory/nullrepo.go`:

```go
package inventory

import (
    "context"
    "time"
)

// NewNullRepo returns a Repo whose writes are no-ops and whose reads return
// ErrInventoryRequired. Used by the engine in --no-inventory mode.
func NewNullRepo() Repo { return nullRepo{} }

type nullRepo struct{}

func (nullRepo) Orgs() OrgRepo                            { return nullOrgs{} }
func (nullRepo) Users() UserRepo                          { return nullUsers{} }
func (nullRepo) Projects() ProjectRepo                    { return nullProjects{} }
func (nullRepo) Stacks() StackRepo                        { return nullStacks{} }
func (nullRepo) Components() ComponentRepo                { return nullComponents{} }
func (nullRepo) Compositions() CompositionRepo            { return nullCompositions{} }
func (nullRepo) Deployments() DeploymentRepo              { return nullDeployments{} }
func (nullRepo) DeploymentTargets() DeploymentTargetRepo  { return nullTargets{} }
func (nullRepo) Runs() RunRepo                            { return nullRuns{} }
func (nullRepo) RunLogs() RunLogRepo                      { return nullRunLogs{} }
func (nullRepo) DriftStatus() DriftStatusRepo             { return nullDrift{} }
func (nullRepo) CostEstimates() CostEstimateRepo          { return nullCostEst{} }
func (nullRepo) CostActuals() CostActualRepo              { return nullCostAct{} }
func (nullRepo) SecretsRefs() SecretsRefRepo              { return nullSecrets{} }
func (nullRepo) AuditLog() AuditLogRepo                   { return nullAudit{} }
func (nullRepo) Migrate(ctx context.Context) error        { return nil }
func (nullRepo) Ping(ctx context.Context) error           { return nil }
func (nullRepo) Close() error                             { return nil }

type nullOrgs struct{}
func (nullOrgs) Get(ctx context.Context, id string) (*Org, error)    { return nil, ErrInventoryRequired }
func (nullOrgs) List(ctx context.Context) ([]Org, error)             { return nil, ErrInventoryRequired }
func (nullOrgs) Create(ctx context.Context, o Org) error             { return nil }

type nullUsers struct{}
func (nullUsers) Get(ctx context.Context, orgID, id string) (*User, error)       { return nil, ErrInventoryRequired }
func (nullUsers) GetByEmail(ctx context.Context, orgID, email string) (*User, error) { return nil, ErrInventoryRequired }
func (nullUsers) Create(ctx context.Context, u User) error                        { return nil }

type nullProjects struct{}
func (nullProjects) Get(ctx context.Context, orgID, id string) (*Project, error) { return nil, ErrInventoryRequired }
func (nullProjects) List(ctx context.Context, orgID string) ([]Project, error)   { return nil, ErrInventoryRequired }
func (nullProjects) Create(ctx context.Context, p Project) error                  { return nil }

type nullStacks struct{}
func (nullStacks) Get(ctx context.Context, orgID, id string) (*Stack, error) { return nil, ErrInventoryRequired }
func (nullStacks) GetByName(ctx context.Context, orgID, projectID, name string) (*Stack, error) { return nil, ErrInventoryRequired }
func (nullStacks) List(ctx context.Context, orgID, projectID string) ([]Stack, error) { return nil, ErrInventoryRequired }
func (nullStacks) Upsert(ctx context.Context, s Stack) error { return nil }

type nullComponents struct{}
func (nullComponents) Get(ctx context.Context, orgID, id string) (*Component, error) { return nil, ErrInventoryRequired }
func (nullComponents) ListByStack(ctx context.Context, orgID, projectID, stackID string) ([]Component, error) { return nil, ErrInventoryRequired }
func (nullComponents) Upsert(ctx context.Context, c Component) error { return nil }

type nullCompositions struct{}
func (nullCompositions) ListByProject(ctx context.Context, orgID, projectID string) ([]CompositionRecord, error) { return nil, ErrInventoryRequired }
func (nullCompositions) Upsert(ctx context.Context, c CompositionRecord) error { return nil }

type nullDeployments struct{}
func (nullDeployments) Get(ctx context.Context, orgID, id string) (*Deployment, error) { return nil, ErrInventoryRequired }
func (nullDeployments) Create(ctx context.Context, d Deployment) error { return nil }
func (nullDeployments) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error { return nil }
func (nullDeployments) ListByProject(ctx context.Context, orgID, projectID string, limit int) ([]Deployment, error) { return nil, ErrInventoryRequired }

type nullTargets struct{}
func (nullTargets) Get(ctx context.Context, orgID, id string) (*DeploymentTarget, error) { return nil, ErrInventoryRequired }
func (nullTargets) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]DeploymentTarget, error) { return nil, ErrInventoryRequired }
func (nullTargets) Create(ctx context.Context, t DeploymentTarget) error { return nil }
func (nullTargets) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error { return nil }

type nullRuns struct{}
func (nullRuns) Get(ctx context.Context, orgID, id string) (*Run, error) { return nil, ErrInventoryRequired }
func (nullRuns) ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]Run, error) { return nil, ErrInventoryRequired }
func (nullRuns) Create(ctx context.Context, r Run) error { return nil }
func (nullRuns) UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error { return nil }

type nullRunLogs struct{}
func (nullRunLogs) Append(ctx context.Context, lines []RunLogLine) error { return nil }
func (nullRunLogs) Read(ctx context.Context, orgID, runID string, sinceSeq int64) ([]RunLogLine, error) { return nil, ErrInventoryRequired }

type nullDrift struct{}
func (nullDrift) Get(ctx context.Context, orgID, dtID string) (*DriftRecord, error) { return nil, ErrInventoryRequired }
func (nullDrift) Upsert(ctx context.Context, d DriftRecord) error { return nil }

type nullCostEst struct{}
func (nullCostEst) BulkInsert(ctx context.Context, items []CostEstimate) error { return nil }
func (nullCostEst) ListByRun(ctx context.Context, orgID, runID string) ([]CostEstimate, error) { return nil, ErrInventoryRequired }

type nullCostAct struct{}
func (nullCostAct) Upsert(ctx context.Context, rows []CostActual) error { return nil }
func (nullCostAct) Query(ctx context.Context, q CostActualQuery) ([]CostActual, error) { return nil, ErrInventoryRequired }

type nullSecrets struct{}
func (nullSecrets) Get(ctx context.Context, orgID, name string) (*SecretsRef, error) { return nil, ErrInventoryRequired }
func (nullSecrets) List(ctx context.Context, orgID string) ([]SecretsRef, error) { return nil, ErrInventoryRequired }
func (nullSecrets) Upsert(ctx context.Context, r SecretsRef) error { return nil }
func (nullSecrets) Delete(ctx context.Context, orgID, name string) error { return nil }

type nullAudit struct{}
func (nullAudit) Append(ctx context.Context, e AuditEntry) error { return nil }
func (nullAudit) Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]AuditEntry, error) { return nil, ErrInventoryRequired }
```

- [ ] **Step 4: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./pkg/inventory/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add pkg/inventory/ && git commit -m "inventory: errors + nullRepo (writes no-op, reads ErrInventoryRequired)"
```

---

## Task 3: Migration runner

**Files:**
- Create: `pkg/inventory/migrations.go`
- Create: `pkg/inventory/migrations_test.go`
- Create: `pkg/inventory/migrations/0001_init.sqlite.sql` (SQLite flavor of existing 0001_init.sql)

- [ ] **Step 1: Write SQLite migration**

Create `pkg/inventory/migrations/0001_init.sqlite.sql`:

```sql
-- SQLite flavor of 0001_init. JSONB → TEXT, UUID → TEXT, TIMESTAMPTZ → TEXT
-- (ISO-8601), NUMERIC → REAL. Phase 1 implements only the subset of tables
-- used by Plan/Apply/Destroy/Drift; the rest are created for forward-compat.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS orgs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    display_name    TEXT,
    is_local        INTEGER NOT NULL DEFAULT 0,
    oidc_provider   TEXT,
    oidc_subject    TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (org_id, email)
);

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    source_uri  TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (org_id, name)
);

CREATE TABLE IF NOT EXISTS stacks (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id         TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    state_backend_kind TEXT,
    state_backend_cfg  TEXT,
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS components (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id    TEXT NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    ir_json     TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, stack_id, name)
);

CREATE TABLE IF NOT EXISTS compositions (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    ir_json     TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, kind)
);

CREATE TABLE IF NOT EXISTS deployments (
    id                     TEXT PRIMARY KEY,
    org_id                 TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id             TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id               TEXT NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    requested_by_user_id   TEXT REFERENCES users(id),
    status                 TEXT NOT NULL,
    partial_failure_policy TEXT,
    started_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at            TEXT
);

CREATE TABLE IF NOT EXISTS deployment_targets (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_id   TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    component_name  TEXT NOT NULL,
    cloud           TEXT NOT NULL,
    region          TEXT NOT NULL,
    credential_ref  TEXT NOT NULL,
    workspace_path  TEXT,
    state_backend   TEXT,
    status          TEXT NOT NULL,
    plan_file       TEXT,
    started_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at     TEXT
);

CREATE TABLE IF NOT EXISTS runs (
    id                    TEXT PRIMARY KEY,
    org_id                TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_target_id  TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    kind                  TEXT NOT NULL,
    status                TEXT NOT NULL,
    exit_code             INTEGER,
    started_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at           TEXT,
    user_id               TEXT REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS drift_status (
    deployment_target_id TEXT PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    org_id               TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    detected_at          TEXT NOT NULL,
    has_drift            INTEGER NOT NULL,
    summary_json         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployments_proj    ON deployments (org_id, project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_targets_deployment  ON deployment_targets (org_id, deployment_id);
CREATE INDEX IF NOT EXISTS idx_runs_target         ON runs (org_id, deployment_target_id, started_at DESC);
```

Note: this migration ADDS a `plan_file` column to `deployment_targets` that's not in the Postgres-flavor 0001_init.sql. The Postgres migration will be updated when Postgres lands; the column is needed so Apply by deployment ID can find the saved plan binary.

- [ ] **Step 2: Write failing test**

Create `pkg/inventory/migrations_test.go`:

```go
package inventory_test

import (
    "context"
    "database/sql"
    "testing"

    _ "modernc.org/sqlite"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestRunMigrations_FreshDB(t *testing.T) {
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil { t.Fatalf("open: %v", err) }
    defer db.Close()
    if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil { t.Fatalf("pragma: %v", err) }

    if err := inventory.RunMigrations(context.Background(), db, inventory.FlavorSQLite); err != nil {
        t.Fatalf("RunMigrations: %v", err)
    }
    // Verify schema_migrations table exists and has the 0001 row.
    var n int
    if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
        t.Fatalf("count: %v", err)
    }
    if n < 1 {
        t.Errorf("schema_migrations count = %d, want >= 1", n)
    }
    // Verify a sample table exists.
    if _, err := db.Exec("SELECT * FROM deployments LIMIT 0"); err != nil {
        t.Errorf("deployments table missing: %v", err)
    }
}

func TestRunMigrations_Idempotent(t *testing.T) {
    db, _ := sql.Open("sqlite", ":memory:")
    defer db.Close()
    _, _ = db.Exec("PRAGMA foreign_keys = ON")
    ctx := context.Background()
    if err := inventory.RunMigrations(ctx, db, inventory.FlavorSQLite); err != nil {
        t.Fatalf("first: %v", err)
    }
    if err := inventory.RunMigrations(ctx, db, inventory.FlavorSQLite); err != nil {
        t.Fatalf("second (should be no-op): %v", err)
    }
}
```

- [ ] **Step 3: Implement migrations.go**

Create `pkg/inventory/migrations.go`:

```go
package inventory

import (
    "context"
    "database/sql"
    "embed"
    "fmt"
    "sort"
    "strings"
)

// Flavor selects which migration files apply. SQLite picks `.sqlite.sql`
// where present; Postgres picks the bare `.sql`.
type Flavor string

const (
    FlavorSQLite   Flavor = "sqlite"
    FlavorPostgres Flavor = "postgres"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations applies all pending migrations to db. Records applied
// versions in schema_migrations. Idempotent.
func RunMigrations(ctx context.Context, db *sql.DB, flavor Flavor) error {
    if _, err := db.ExecContext(ctx, schemaMigrationsTable(flavor)); err != nil {
        return fmt.Errorf("create schema_migrations: %w", err)
    }
    applied, err := loadApplied(ctx, db)
    if err != nil {
        return fmt.Errorf("load applied: %w", err)
    }
    migrations, err := discoverMigrations(flavor)
    if err != nil {
        return fmt.Errorf("discover migrations: %w", err)
    }
    for _, m := range migrations {
        if applied[m.Version] {
            continue
        }
        if err := applyOne(ctx, db, m); err != nil {
            return fmt.Errorf("apply %s: %w", m.Version, err)
        }
    }
    return nil
}

type migrationFile struct {
    Version string
    Body    string
}

func discoverMigrations(flavor Flavor) ([]migrationFile, error) {
    entries, err := migrationFS.ReadDir("migrations")
    if err != nil {
        return nil, err
    }
    // Group by version; prefer flavor-specific file.
    byVersion := map[string]string{}
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        if !strings.HasSuffix(name, ".sql") {
            continue
        }
        version, isFlavor := parseMigrationName(name, flavor)
        if version == "" {
            continue
        }
        existing, exists := byVersion[version]
        if !exists || isFlavor {
            body, err := migrationFS.ReadFile("migrations/" + name)
            if err != nil {
                return nil, err
            }
            byVersion[version] = string(body)
            _ = existing
        }
    }
    var out []migrationFile
    for v, b := range byVersion {
        out = append(out, migrationFile{Version: v, Body: b})
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
    return out, nil
}

// parseMigrationName returns the version slug ("0001_init") and whether the
// file is a flavor-specific override. Returns "" for non-matching files.
func parseMigrationName(name string, flavor Flavor) (version string, isFlavor bool) {
    if strings.HasSuffix(name, ".sqlite.sql") {
        if flavor != FlavorSQLite {
            return "", false
        }
        return strings.TrimSuffix(name, ".sqlite.sql"), true
    }
    if strings.HasSuffix(name, ".postgres.sql") {
        if flavor != FlavorPostgres {
            return "", false
        }
        return strings.TrimSuffix(name, ".postgres.sql"), true
    }
    // Bare .sql: applies to either flavor when no override present.
    return strings.TrimSuffix(name, ".sql"), false
}

func loadApplied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
    rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := map[string]bool{}
    for rows.Next() {
        var v string
        if err := rows.Scan(&v); err != nil {
            return nil, err
        }
        out[v] = true
    }
    return out, rows.Err()
}

func applyOne(ctx context.Context, db *sql.DB, m migrationFile) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    if _, err := tx.ExecContext(ctx, m.Body); err != nil {
        return err
    }
    if _, err := tx.ExecContext(ctx,
        "INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))",
        m.Version); err != nil {
        return err
    }
    return tx.Commit()
}

func schemaMigrationsTable(flavor Flavor) string {
    if flavor == FlavorPostgres {
        return `CREATE TABLE IF NOT EXISTS schema_migrations (
            version    TEXT PRIMARY KEY,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )`
    }
    return `CREATE TABLE IF NOT EXISTS schema_migrations (
        version    TEXT PRIMARY KEY,
        applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    )`
}
```

- [ ] **Step 4: Run, commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./pkg/inventory/ -v -run TestRunMigrations
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add pkg/inventory/migrations.go pkg/inventory/migrations_test.go pkg/inventory/migrations/0001_init.sqlite.sql && git commit -m "inventory: embedded migration runner + 0001_init.sqlite.sql"
```

---

## Task 4: SQLite Repo skeleton (Open, Migrate, Ping, Close, Orgs)

**Files:**
- Create: `internal/inventory/sqlite/sqlite.go`
- Create: `internal/inventory/sqlite/sqlite_test.go`
- Create: `internal/inventory/sqlite/orgs.go`

- [ ] **Step 1: Implement Open / Repo struct / Migrate / Ping / Close**

Create `internal/inventory/sqlite/sqlite.go`:

```go
// Package sqlite implements pkg/inventory.Repo against modernc.org/sqlite.
// All cross-cutting concerns (connection setup, foreign-key pragma, helpers)
// live here; each sub-repo lives in its own file.
package sqlite

import (
    "context"
    "database/sql"
    "fmt"
    "net/url"
    "strings"

    _ "modernc.org/sqlite"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// Repo is the SQLite Repo implementation.
type Repo struct {
    db *sql.DB
}

// Open returns a SQLite Repo from a DSN like "sqlite:///path/to/file.db" or
// "sqlite::memory:". Foreign keys are enabled.
func Open(dsn string) (*Repo, error) {
    path, err := parseDSN(dsn)
    if err != nil {
        return nil, err
    }
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, fmt.Errorf("sqlite open: %w", err)
    }
    if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
        db.Close()
        return nil, fmt.Errorf("foreign_keys pragma: %w", err)
    }
    return &Repo{db: db}, nil
}

// parseDSN turns "sqlite:///path" or "sqlite::memory:" into the path
// modernc.org/sqlite expects. Plain paths are passed through.
func parseDSN(dsn string) (string, error) {
    if strings.HasPrefix(dsn, "sqlite::memory:") {
        return ":memory:", nil
    }
    if strings.HasPrefix(dsn, "sqlite://") {
        rest := strings.TrimPrefix(dsn, "sqlite://")
        // Allow file: prefix in DSN; otherwise pass through.
        return rest, nil
    }
    if strings.HasPrefix(dsn, "sqlite:") {
        return strings.TrimPrefix(dsn, "sqlite:"), nil
    }
    _, err := url.Parse(dsn) // sanity
    if err != nil {
        return "", fmt.Errorf("invalid DSN: %w", err)
    }
    return dsn, nil
}

func (r *Repo) Migrate(ctx context.Context) error {
    return inventory.RunMigrations(ctx, r.db, inventory.FlavorSQLite)
}

func (r *Repo) Ping(ctx context.Context) error {
    return r.db.PingContext(ctx)
}

func (r *Repo) Close() error { return r.db.Close() }

// Verify the Repo satisfies the inventory contract at compile time.
var _ inventory.Repo = (*Repo)(nil)

// Sub-repo accessors. Each lives in its own file.
func (r *Repo) Orgs() inventory.OrgRepo                           { return &orgRepo{db: r.db} }
func (r *Repo) Users() inventory.UserRepo                         { return notWired{}.users() }
func (r *Repo) Projects() inventory.ProjectRepo                   { return &projectRepo{db: r.db} }
func (r *Repo) Stacks() inventory.StackRepo                       { return &stackRepo{db: r.db} }
func (r *Repo) Components() inventory.ComponentRepo               { return &componentRepo{db: r.db} }
func (r *Repo) Compositions() inventory.CompositionRepo           { return notWired{}.compositions() }
func (r *Repo) Deployments() inventory.DeploymentRepo             { return &deploymentRepo{db: r.db} }
func (r *Repo) DeploymentTargets() inventory.DeploymentTargetRepo { return &targetRepo{db: r.db} }
func (r *Repo) Runs() inventory.RunRepo                           { return &runRepo{db: r.db} }
func (r *Repo) RunLogs() inventory.RunLogRepo                     { return notWired{}.runLogs() }
func (r *Repo) DriftStatus() inventory.DriftStatusRepo            { return &driftRepo{db: r.db} }
func (r *Repo) CostEstimates() inventory.CostEstimateRepo         { return notWired{}.costEst() }
func (r *Repo) CostActuals() inventory.CostActualRepo             { return notWired{}.costAct() }
func (r *Repo) SecretsRefs() inventory.SecretsRefRepo             { return notWired{}.secrets() }
func (r *Repo) AuditLog() inventory.AuditLogRepo                  { return notWired{}.audit() }
```

Create `internal/inventory/sqlite/notwired.go`:

```go
package sqlite

import (
    "context"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// notWired returns repos that fail loudly when called. Phase 1 only wires
// the subset of repos that Plan/Apply/Destroy/Drift need; others land in
// their respective phases.
type notWired struct{}

func (notWired) users() inventory.UserRepo               { return errUsers{} }
func (notWired) compositions() inventory.CompositionRepo { return errCompositions{} }
func (notWired) runLogs() inventory.RunLogRepo           { return errRunLogs{} }
func (notWired) costEst() inventory.CostEstimateRepo     { return errCostEst{} }
func (notWired) costAct() inventory.CostActualRepo       { return errCostAct{} }
func (notWired) secrets() inventory.SecretsRefRepo       { return errSecrets{} }
func (notWired) audit() inventory.AuditLogRepo           { return errAudit{} }

type errUsers struct{}
func (errUsers) Get(ctx context.Context, orgID, id string) (*inventory.User, error) { return nil, inventory.ErrNotImplementedYet }
func (errUsers) GetByEmail(ctx context.Context, orgID, email string) (*inventory.User, error) { return nil, inventory.ErrNotImplementedYet }
func (errUsers) Create(ctx context.Context, u inventory.User) error { return inventory.ErrNotImplementedYet }

type errCompositions struct{}
func (errCompositions) ListByProject(ctx context.Context, orgID, projectID string) ([]inventory.CompositionRecord, error) { return nil, inventory.ErrNotImplementedYet }
func (errCompositions) Upsert(ctx context.Context, c inventory.CompositionRecord) error { return inventory.ErrNotImplementedYet }

type errRunLogs struct{}
func (errRunLogs) Append(ctx context.Context, lines []inventory.RunLogLine) error { return inventory.ErrNotImplementedYet }
func (errRunLogs) Read(ctx context.Context, orgID, runID string, sinceSeq int64) ([]inventory.RunLogLine, error) { return nil, inventory.ErrNotImplementedYet }

type errCostEst struct{}
func (errCostEst) BulkInsert(ctx context.Context, items []inventory.CostEstimate) error { return inventory.ErrNotImplementedYet }
func (errCostEst) ListByRun(ctx context.Context, orgID, runID string) ([]inventory.CostEstimate, error) { return nil, inventory.ErrNotImplementedYet }

type errCostAct struct{}
func (errCostAct) Upsert(ctx context.Context, rows []inventory.CostActual) error { return inventory.ErrNotImplementedYet }
func (errCostAct) Query(ctx context.Context, q inventory.CostActualQuery) ([]inventory.CostActual, error) { return nil, inventory.ErrNotImplementedYet }

type errSecrets struct{}
func (errSecrets) Get(ctx context.Context, orgID, name string) (*inventory.SecretsRef, error) { return nil, inventory.ErrNotImplementedYet }
func (errSecrets) List(ctx context.Context, orgID string) ([]inventory.SecretsRef, error) { return nil, inventory.ErrNotImplementedYet }
func (errSecrets) Upsert(ctx context.Context, r inventory.SecretsRef) error { return inventory.ErrNotImplementedYet }
func (errSecrets) Delete(ctx context.Context, orgID, name string) error { return inventory.ErrNotImplementedYet }

type errAudit struct{}
func (errAudit) Append(ctx context.Context, e inventory.AuditEntry) error { return inventory.ErrNotImplementedYet }
func (errAudit) Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]inventory.AuditEntry, error) { return nil, inventory.ErrNotImplementedYet }
```

Create `internal/inventory/sqlite/orgs.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type orgRepo struct{ db *sql.DB }

func (r *orgRepo) Get(ctx context.Context, id string) (*inventory.Org, error) {
    var o inventory.Org
    var createdAt string
    err := r.db.QueryRowContext(ctx, "SELECT id, name, created_at FROM orgs WHERE id = ?", id).
        Scan(&o.ID, &o.Name, &createdAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("orgs.Get: %w", err)
    }
    o.CreatedAt = mustParseTime(createdAt)
    return &o, nil
}

func (r *orgRepo) List(ctx context.Context) ([]inventory.Org, error) {
    rows, err := r.db.QueryContext(ctx, "SELECT id, name, created_at FROM orgs ORDER BY name")
    if err != nil {
        return nil, fmt.Errorf("orgs.List: %w", err)
    }
    defer rows.Close()
    var out []inventory.Org
    for rows.Next() {
        var o inventory.Org
        var createdAt string
        if err := rows.Scan(&o.ID, &o.Name, &createdAt); err != nil {
            return nil, err
        }
        o.CreatedAt = mustParseTime(createdAt)
        out = append(out, o)
    }
    return out, rows.Err()
}

func (r *orgRepo) Create(ctx context.Context, o inventory.Org) error {
    _, err := r.db.ExecContext(ctx, "INSERT INTO orgs (id, name) VALUES (?, ?)", o.ID, o.Name)
    if err != nil {
        return fmt.Errorf("orgs.Create: %w", err)
    }
    return nil
}
```

Create `internal/inventory/sqlite/time.go`:

```go
package sqlite

import "time"

const sqliteTimeLayout = "2006-01-02T15:04:05.000Z"

func mustParseTime(s string) time.Time {
    if s == "" {
        return time.Time{}
    }
    t, err := time.Parse(sqliteTimeLayout, s)
    if err == nil {
        return t
    }
    // Try without milliseconds.
    if t, err := time.Parse(time.RFC3339, s); err == nil {
        return t
    }
    return time.Time{}
}

func formatTime(t time.Time) string {
    if t.IsZero() {
        return ""
    }
    return t.UTC().Format(sqliteTimeLayout)
}

func nullableTime(t *time.Time) any {
    if t == nil || t.IsZero() {
        return nil
    }
    return formatTime(*t)
}
```

- [ ] **Step 2: Write test**

Create `internal/inventory/sqlite/sqlite_test.go`:

```go
package sqlite_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func openMemory(t *testing.T) *sqlite.Repo {
    t.Helper()
    r, err := sqlite.Open("sqlite::memory:")
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    t.Cleanup(func() { r.Close() })
    if err := r.Migrate(context.Background()); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    return r
}

func TestRepo_Ping(t *testing.T) {
    r := openMemory(t)
    if err := r.Ping(context.Background()); err != nil {
        t.Errorf("Ping: %v", err)
    }
}

func TestRepo_OrgsRoundTrip(t *testing.T) {
    r := openMemory(t)
    ctx := context.Background()
    if err := r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "test"}); err != nil {
        t.Fatalf("Create: %v", err)
    }
    o, err := r.Orgs().Get(ctx, "org-1")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if o == nil || o.Name != "test" {
        t.Errorf("got %+v", o)
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./internal/inventory/sqlite/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add internal/inventory/sqlite/ && git commit -m "sqlite: Repo skeleton + Orgs CRUD + notWired stubs for deferred sub-repos"
```

---

## Task 5: Projects, Stacks, Components repos

**Files:**
- Create: `internal/inventory/sqlite/projects.go`
- Create: `internal/inventory/sqlite/stacks.go`
- Create: `internal/inventory/sqlite/components.go`
- Create: `internal/inventory/sqlite/crud_test.go`

- [ ] **Step 1: Implement each repo**

Create `internal/inventory/sqlite/projects.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type projectRepo struct{ db *sql.DB }

func (r *projectRepo) Get(ctx context.Context, orgID, id string) (*inventory.Project, error) {
    var p inventory.Project
    var createdAt string
    err := r.db.QueryRowContext(ctx,
        "SELECT id, org_id, name, COALESCE(source_uri, ''), created_at FROM projects WHERE org_id = ? AND id = ?",
        orgID, id).Scan(&p.ID, &p.OrgID, &p.Name, &p.SourceURI, &createdAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("projects.Get: %w", err)
    }
    p.CreatedAt = mustParseTime(createdAt)
    return &p, nil
}

func (r *projectRepo) List(ctx context.Context, orgID string) ([]inventory.Project, error) {
    rows, err := r.db.QueryContext(ctx,
        "SELECT id, org_id, name, COALESCE(source_uri, ''), created_at FROM projects WHERE org_id = ? ORDER BY name",
        orgID)
    if err != nil {
        return nil, fmt.Errorf("projects.List: %w", err)
    }
    defer rows.Close()
    var out []inventory.Project
    for rows.Next() {
        var p inventory.Project
        var createdAt string
        if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.SourceURI, &createdAt); err != nil {
            return nil, err
        }
        p.CreatedAt = mustParseTime(createdAt)
        out = append(out, p)
    }
    return out, rows.Err()
}

func (r *projectRepo) Create(ctx context.Context, p inventory.Project) error {
    _, err := r.db.ExecContext(ctx,
        "INSERT INTO projects (id, org_id, name, source_uri) VALUES (?, ?, ?, ?)",
        p.ID, p.OrgID, p.Name, p.SourceURI)
    if err != nil {
        return fmt.Errorf("projects.Create: %w", err)
    }
    return nil
}
```

Create `internal/inventory/sqlite/stacks.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type stackRepo struct{ db *sql.DB }

func (r *stackRepo) Get(ctx context.Context, orgID, id string) (*inventory.Stack, error) {
    return r.scanOne(ctx,
        "SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND id = ?",
        orgID, id)
}

func (r *stackRepo) GetByName(ctx context.Context, orgID, projectID, name string) (*inventory.Stack, error) {
    return r.scanOne(ctx,
        "SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND project_id = ? AND name = ?",
        orgID, projectID, name)
}

func (r *stackRepo) List(ctx context.Context, orgID, projectID string) ([]inventory.Stack, error) {
    rows, err := r.db.QueryContext(ctx,
        "SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND project_id = ? ORDER BY name",
        orgID, projectID)
    if err != nil {
        return nil, fmt.Errorf("stacks.List: %w", err)
    }
    defer rows.Close()
    var out []inventory.Stack
    for rows.Next() {
        var s inventory.Stack
        var cfg string
        if err := rows.Scan(&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.StateBackendKind, &cfg); err != nil {
            return nil, err
        }
        s.StateBackendCfg = []byte(cfg)
        out = append(out, s)
    }
    return out, rows.Err()
}

func (r *stackRepo) Upsert(ctx context.Context, s inventory.Stack) error {
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO stacks (id, org_id, project_id, name, state_backend_kind, state_backend_cfg)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, name) DO UPDATE SET
            state_backend_kind = excluded.state_backend_kind,
            state_backend_cfg  = excluded.state_backend_cfg
    `, s.ID, s.OrgID, s.ProjectID, s.Name, s.StateBackendKind, string(s.StateBackendCfg))
    if err != nil {
        return fmt.Errorf("stacks.Upsert: %w", err)
    }
    return nil
}

func (r *stackRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.Stack, error) {
    var s inventory.Stack
    var cfg string
    err := r.db.QueryRowContext(ctx, query, args...).
        Scan(&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.StateBackendKind, &cfg)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("stacks: %w", err)
    }
    s.StateBackendCfg = []byte(cfg)
    return &s, nil
}
```

Create `internal/inventory/sqlite/components.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type componentRepo struct{ db *sql.DB }

func (r *componentRepo) Get(ctx context.Context, orgID, id string) (*inventory.Component, error) {
    var c inventory.Component
    var irJSON, updatedAt string
    err := r.db.QueryRowContext(ctx,
        "SELECT id, org_id, project_id, stack_id, name, type, ir_json, updated_at FROM components WHERE org_id = ? AND id = ?",
        orgID, id).Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.StackID, &c.Name, &c.Type, &irJSON, &updatedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("components.Get: %w", err)
    }
    c.IRJSON = []byte(irJSON)
    c.UpdatedAt = mustParseTime(updatedAt)
    return &c, nil
}

func (r *componentRepo) ListByStack(ctx context.Context, orgID, projectID, stackID string) ([]inventory.Component, error) {
    rows, err := r.db.QueryContext(ctx,
        "SELECT id, org_id, project_id, stack_id, name, type, ir_json, updated_at FROM components WHERE org_id = ? AND project_id = ? AND stack_id = ? ORDER BY name",
        orgID, projectID, stackID)
    if err != nil {
        return nil, fmt.Errorf("components.ListByStack: %w", err)
    }
    defer rows.Close()
    var out []inventory.Component
    for rows.Next() {
        var c inventory.Component
        var irJSON, updatedAt string
        if err := rows.Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.StackID, &c.Name, &c.Type, &irJSON, &updatedAt); err != nil {
            return nil, err
        }
        c.IRJSON = []byte(irJSON)
        c.UpdatedAt = mustParseTime(updatedAt)
        out = append(out, c)
    }
    return out, rows.Err()
}

func (r *componentRepo) Upsert(ctx context.Context, c inventory.Component) error {
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO components (id, org_id, project_id, stack_id, name, type, ir_json, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
        ON CONFLICT(project_id, stack_id, name) DO UPDATE SET
            type = excluded.type,
            ir_json = excluded.ir_json,
            updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
    `, c.ID, c.OrgID, c.ProjectID, c.StackID, c.Name, c.Type, string(c.IRJSON))
    if err != nil {
        return fmt.Errorf("components.Upsert: %w", err)
    }
    return nil
}
```

- [ ] **Step 2: Write test**

Create `internal/inventory/sqlite/crud_test.go`:

```go
package sqlite_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestProjectStackComponent_RoundTrip(t *testing.T) {
    r := openMemory(t)
    ctx := context.Background()

    if err := r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "local"}); err != nil { t.Fatal(err) }
    if err := r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "org-1", Name: "demo"}); err != nil { t.Fatal(err) }
    if err := r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "org-1", ProjectID: "p-1", Name: "dev", StateBackendKind: "local", StateBackendCfg: []byte(`{}`)}); err != nil { t.Fatal(err) }
    if err := r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1", Name: "web", Type: "network", IRJSON: []byte(`{"name":"web"}`)}); err != nil { t.Fatal(err) }

    p, _ := r.Projects().Get(ctx, "org-1", "p-1")
    if p == nil || p.Name != "demo" { t.Fatalf("project: %+v", p) }
    s, _ := r.Stacks().GetByName(ctx, "org-1", "p-1", "dev")
    if s == nil || s.StateBackendKind != "local" { t.Fatalf("stack: %+v", s) }
    cs, _ := r.Components().ListByStack(ctx, "org-1", "p-1", "s-1")
    if len(cs) != 1 || cs[0].Name != "web" { t.Fatalf("components: %+v", cs) }

    // Upsert idempotency: updating the IR.
    if err := r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1", Name: "web", Type: "network", IRJSON: []byte(`{"name":"web","updated":true}`)}); err != nil { t.Fatal(err) }
    cs2, _ := r.Components().ListByStack(ctx, "org-1", "p-1", "s-1")
    if string(cs2[0].IRJSON) != `{"name":"web","updated":true}` {
        t.Errorf("upsert update: %s", cs2[0].IRJSON)
    }
}

func TestOrgScoping_IsolatedReads(t *testing.T) {
    r := openMemory(t)
    ctx := context.Background()
    _ = r.Orgs().Create(ctx, inventory.Org{ID: "org-A", Name: "a"})
    _ = r.Orgs().Create(ctx, inventory.Org{ID: "org-B", Name: "b"})
    _ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "org-A", Name: "shared-name"})
    _ = r.Projects().Create(ctx, inventory.Project{ID: "p2", OrgID: "org-B", Name: "shared-name"})

    listA, _ := r.Projects().List(ctx, "org-A")
    if len(listA) != 1 || listA[0].ID != "p" {
        t.Errorf("org A leak: %+v", listA)
    }
    listB, _ := r.Projects().List(ctx, "org-B")
    if len(listB) != 1 || listB[0].ID != "p2" {
        t.Errorf("org B leak: %+v", listB)
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./internal/inventory/sqlite/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add internal/inventory/sqlite/ && git commit -m "sqlite: Projects / Stacks / Components repos with org-scoped CRUD"
```

---

## Task 6: Deployments, DeploymentTargets, Runs, DriftStatus repos

**Files:**
- Create: `internal/inventory/sqlite/deployments.go`
- Create: `internal/inventory/sqlite/targets.go`
- Create: `internal/inventory/sqlite/runs.go`
- Create: `internal/inventory/sqlite/drift.go`
- Create: `internal/inventory/sqlite/runlifecycle_test.go`

Phase-1 caveat: the migration adds a `plan_file TEXT` column to `deployment_targets` that's not in the existing repo struct. Extend `pkg/inventory.DeploymentTarget` to include it:

- [ ] **Step 1: Extend the public type**

In `pkg/inventory/repo.go`, modify `DeploymentTarget`:

```go
type DeploymentTarget struct {
    ID            string
    OrgID         string
    DeploymentID  string
    ComponentName string
    Cloud         string
    Region        string
    CredentialRef string
    WorkspacePath string
    PlanFile      string  // NEW: path to the tofu plan binary for this target
    StateBackend  []byte
    Status        string
    StartedAt     time.Time
    FinishedAt    *time.Time
}
```

- [ ] **Step 2: Implement deployment / target / run / drift repos**

Create `internal/inventory/sqlite/deployments.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type deploymentRepo struct{ db *sql.DB }

func (r *deploymentRepo) Get(ctx context.Context, orgID, id string) (*inventory.Deployment, error) {
    var d inventory.Deployment
    var requestedBy sql.NullString
    var startedAt string
    var finishedAt sql.NullString
    err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, project_id, stack_id, requested_by_user_id, status,
               COALESCE(partial_failure_policy,''), started_at, finished_at
        FROM deployments WHERE org_id = ? AND id = ?
    `, orgID, id).Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.StackID, &requestedBy,
        &d.Status, &d.PartialFailurePolicy, &startedAt, &finishedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("deployments.Get: %w", err)
    }
    d.RequestedByUserID = requestedBy.String
    d.StartedAt = mustParseTime(startedAt)
    if finishedAt.Valid {
        t := mustParseTime(finishedAt.String)
        d.FinishedAt = &t
    }
    return &d, nil
}

func (r *deploymentRepo) Create(ctx context.Context, d inventory.Deployment) error {
    var requestedBy any
    if d.RequestedByUserID != "" {
        requestedBy = d.RequestedByUserID
    }
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO deployments (id, org_id, project_id, stack_id, requested_by_user_id, status, partial_failure_policy, started_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, d.ID, d.OrgID, d.ProjectID, d.StackID, requestedBy, d.Status, d.PartialFailurePolicy, formatTime(d.StartedAt))
    if err != nil {
        return fmt.Errorf("deployments.Create: %w", err)
    }
    return nil
}

func (r *deploymentRepo) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
    _, err := r.db.ExecContext(ctx,
        "UPDATE deployments SET status = ?, finished_at = ? WHERE org_id = ? AND id = ?",
        status, nullableTime(finishedAt), orgID, id)
    if err != nil {
        return fmt.Errorf("deployments.UpdateStatus: %w", err)
    }
    return nil
}

func (r *deploymentRepo) ListByProject(ctx context.Context, orgID, projectID string, limit int) ([]inventory.Deployment, error) {
    if limit <= 0 {
        limit = 50
    }
    rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, project_id, stack_id, COALESCE(requested_by_user_id, ''), status,
               COALESCE(partial_failure_policy,''), started_at, finished_at
        FROM deployments WHERE org_id = ? AND project_id = ?
        ORDER BY started_at DESC LIMIT ?
    `, orgID, projectID, limit)
    if err != nil {
        return nil, fmt.Errorf("deployments.ListByProject: %w", err)
    }
    defer rows.Close()
    var out []inventory.Deployment
    for rows.Next() {
        var d inventory.Deployment
        var startedAt string
        var finishedAt sql.NullString
        if err := rows.Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.StackID, &d.RequestedByUserID,
            &d.Status, &d.PartialFailurePolicy, &startedAt, &finishedAt); err != nil {
            return nil, err
        }
        d.StartedAt = mustParseTime(startedAt)
        if finishedAt.Valid {
            t := mustParseTime(finishedAt.String)
            d.FinishedAt = &t
        }
        out = append(out, d)
    }
    return out, rows.Err()
}
```

Create `internal/inventory/sqlite/targets.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type targetRepo struct{ db *sql.DB }

func (r *targetRepo) Get(ctx context.Context, orgID, id string) (*inventory.DeploymentTarget, error) {
    return r.scanOne(ctx, `
        SELECT id, org_id, deployment_id, component_name, cloud, region, credential_ref,
               COALESCE(workspace_path,''), COALESCE(plan_file,''), COALESCE(state_backend,''),
               status, started_at, finished_at
        FROM deployment_targets WHERE org_id = ? AND id = ?
    `, orgID, id)
}

func (r *targetRepo) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]inventory.DeploymentTarget, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, deployment_id, component_name, cloud, region, credential_ref,
               COALESCE(workspace_path,''), COALESCE(plan_file,''), COALESCE(state_backend,''),
               status, started_at, finished_at
        FROM deployment_targets WHERE org_id = ? AND deployment_id = ?
        ORDER BY component_name, cloud, region
    `, orgID, deploymentID)
    if err != nil {
        return nil, fmt.Errorf("targets.ListByDeployment: %w", err)
    }
    defer rows.Close()
    var out []inventory.DeploymentTarget
    for rows.Next() {
        t, err := scanTargetRow(rows)
        if err != nil {
            return nil, err
        }
        out = append(out, *t)
    }
    return out, rows.Err()
}

func (r *targetRepo) Create(ctx context.Context, t inventory.DeploymentTarget) error {
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO deployment_targets
            (id, org_id, deployment_id, component_name, cloud, region, credential_ref,
             workspace_path, plan_file, state_backend, status, started_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, t.ID, t.OrgID, t.DeploymentID, t.ComponentName, t.Cloud, t.Region, t.CredentialRef,
        t.WorkspacePath, t.PlanFile, string(t.StateBackend), t.Status, formatTime(t.StartedAt))
    if err != nil {
        return fmt.Errorf("targets.Create: %w", err)
    }
    return nil
}

func (r *targetRepo) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
    _, err := r.db.ExecContext(ctx,
        "UPDATE deployment_targets SET status = ?, finished_at = ? WHERE org_id = ? AND id = ?",
        status, nullableTime(finishedAt), orgID, id)
    if err != nil {
        return fmt.Errorf("targets.UpdateStatus: %w", err)
    }
    return nil
}

func (r *targetRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.DeploymentTarget, error) {
    rows, err := r.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    if !rows.Next() {
        if err := rows.Err(); err != nil {
            return nil, err
        }
        return nil, nil
    }
    return scanTargetRow(rows)
}

func scanTargetRow(rows *sql.Rows) (*inventory.DeploymentTarget, error) {
    var t inventory.DeploymentTarget
    var sb string
    var startedAt string
    var finishedAt sql.NullString
    if err := rows.Scan(&t.ID, &t.OrgID, &t.DeploymentID, &t.ComponentName, &t.Cloud, &t.Region,
        &t.CredentialRef, &t.WorkspacePath, &t.PlanFile, &sb, &t.Status, &startedAt, &finishedAt); err != nil {
        return nil, err
    }
    t.StateBackend = []byte(sb)
    t.StartedAt = mustParseTime(startedAt)
    if finishedAt.Valid {
        tt := mustParseTime(finishedAt.String)
        t.FinishedAt = &tt
    }
    return &t, nil
}

// Keep an unused-import guard out of the way.
var _ = errors.New
```

Create `internal/inventory/sqlite/runs.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type runRepo struct{ db *sql.DB }

func (r *runRepo) Get(ctx context.Context, orgID, id string) (*inventory.Run, error) {
    var run inventory.Run
    var exit sql.NullInt64
    var startedAt string
    var finishedAt sql.NullString
    var userID sql.NullString
    err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, exit_code, started_at, finished_at, user_id
        FROM runs WHERE org_id = ? AND id = ?
    `, orgID, id).Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
        &exit, &startedAt, &finishedAt, &userID)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("runs.Get: %w", err)
    }
    run.ExitCode = int(exit.Int64)
    run.StartedAt = mustParseTime(startedAt)
    if finishedAt.Valid {
        t := mustParseTime(finishedAt.String)
        run.FinishedAt = &t
    }
    run.UserID = userID.String
    return &run, nil
}

func (r *runRepo) ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]inventory.Run, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, COALESCE(exit_code,0), started_at, finished_at, COALESCE(user_id,'')
        FROM runs WHERE org_id = ? AND deployment_target_id = ?
        ORDER BY started_at DESC
    `, orgID, dtID)
    if err != nil {
        return nil, fmt.Errorf("runs.ListByDeploymentTarget: %w", err)
    }
    defer rows.Close()
    var out []inventory.Run
    for rows.Next() {
        var run inventory.Run
        var startedAt string
        var finishedAt sql.NullString
        if err := rows.Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
            &run.ExitCode, &startedAt, &finishedAt, &run.UserID); err != nil {
            return nil, err
        }
        run.StartedAt = mustParseTime(startedAt)
        if finishedAt.Valid {
            t := mustParseTime(finishedAt.String)
            run.FinishedAt = &t
        }
        out = append(out, run)
    }
    return out, rows.Err()
}

func (r *runRepo) Create(ctx context.Context, run inventory.Run) error {
    var userID any
    if run.UserID != "" {
        userID = run.UserID
    }
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO runs (id, org_id, deployment_target_id, kind, status, exit_code, started_at, user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, run.ID, run.OrgID, run.DeploymentTargetID, run.Kind, run.Status, run.ExitCode,
        formatTime(run.StartedAt), userID)
    if err != nil {
        return fmt.Errorf("runs.Create: %w", err)
    }
    return nil
}

func (r *runRepo) UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error {
    _, err := r.db.ExecContext(ctx, `
        UPDATE runs SET status = ?, exit_code = ?, finished_at = ?
        WHERE org_id = ? AND id = ?
    `, status, exitCode, nullableTime(finishedAt), orgID, id)
    if err != nil {
        return fmt.Errorf("runs.UpdateStatus: %w", err)
    }
    return nil
}
```

Create `internal/inventory/sqlite/drift.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

type driftRepo struct{ db *sql.DB }

func (r *driftRepo) Get(ctx context.Context, orgID, dtID string) (*inventory.DriftRecord, error) {
    var d inventory.DriftRecord
    var summary string
    var detectedAt string
    var hasDrift int
    err := r.db.QueryRowContext(ctx, `
        SELECT deployment_target_id, org_id, detected_at, has_drift, summary_json
        FROM drift_status WHERE org_id = ? AND deployment_target_id = ?
    `, orgID, dtID).Scan(&d.DeploymentTargetID, &d.OrgID, &detectedAt, &hasDrift, &summary)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("drift.Get: %w", err)
    }
    d.DetectedAt = mustParseTime(detectedAt)
    d.HasDrift = hasDrift != 0
    d.SummaryJSON = []byte(summary)
    return &d, nil
}

func (r *driftRepo) Upsert(ctx context.Context, d inventory.DriftRecord) error {
    hasDrift := 0
    if d.HasDrift {
        hasDrift = 1
    }
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO drift_status (deployment_target_id, org_id, detected_at, has_drift, summary_json)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(deployment_target_id) DO UPDATE SET
            detected_at = excluded.detected_at,
            has_drift   = excluded.has_drift,
            summary_json = excluded.summary_json
    `, d.DeploymentTargetID, d.OrgID, formatTime(d.DetectedAt), hasDrift, string(d.SummaryJSON))
    if err != nil {
        return fmt.Errorf("drift.Upsert: %w", err)
    }
    return nil
}
```

- [ ] **Step 3: Write the run-lifecycle test**

Create `internal/inventory/sqlite/runlifecycle_test.go`:

```go
package sqlite_test

import (
    "context"
    "testing"
    "time"

    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestDeploymentRunLifecycle(t *testing.T) {
    r := openMemory(t)
    ctx := context.Background()
    _ = r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "local"})
    _ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "org-1", Name: "demo"})
    _ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "org-1", ProjectID: "p-1", Name: "dev", StateBackendKind: "local"})

    dep := inventory.Deployment{
        ID: "d-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1",
        Status: "planned", PartialFailurePolicy: "leave", StartedAt: time.Now().UTC(),
    }
    if err := r.Deployments().Create(ctx, dep); err != nil { t.Fatalf("dep create: %v", err) }

    tgt := inventory.DeploymentTarget{
        ID: "t-1", OrgID: "org-1", DeploymentID: "d-1",
        ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "aws-dev",
        WorkspacePath: "/tmp/ws", PlanFile: "/tmp/plan.bin",
        StateBackend: []byte(`{"kind":"local"}`),
        Status:       "planned", StartedAt: time.Now().UTC(),
    }
    if err := r.DeploymentTargets().Create(ctx, tgt); err != nil { t.Fatalf("tgt create: %v", err) }

    run := inventory.Run{
        ID: "r-1", OrgID: "org-1", DeploymentTargetID: "t-1",
        Kind: "apply", Status: "running", StartedAt: time.Now().UTC(),
    }
    if err := r.Runs().Create(ctx, run); err != nil { t.Fatalf("run create: %v", err) }

    finished := time.Now().UTC().Add(time.Minute)
    if err := r.Runs().UpdateStatus(ctx, "org-1", "r-1", "succeeded", 0, &finished); err != nil { t.Fatal(err) }
    if err := r.DeploymentTargets().UpdateStatus(ctx, "org-1", "t-1", "succeeded", &finished); err != nil { t.Fatal(err) }
    if err := r.Deployments().UpdateStatus(ctx, "org-1", "d-1", "succeeded", &finished); err != nil { t.Fatal(err) }

    d, _ := r.Deployments().Get(ctx, "org-1", "d-1")
    if d == nil || d.Status != "succeeded" || d.FinishedAt == nil {
        t.Errorf("deployment terminal: %+v", d)
    }
    runs, _ := r.Runs().ListByDeploymentTarget(ctx, "org-1", "t-1")
    if len(runs) != 1 || runs[0].Status != "succeeded" {
        t.Errorf("runs: %+v", runs)
    }
    targets, _ := r.DeploymentTargets().ListByDeployment(ctx, "org-1", "d-1")
    if len(targets) != 1 || targets[0].PlanFile != "/tmp/plan.bin" {
        t.Errorf("targets: %+v", targets)
    }
}

func TestDrift_UpsertReplaces(t *testing.T) {
    r := openMemory(t)
    ctx := context.Background()
    _ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})
    _ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "o", Name: "x"})
    _ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "o", ProjectID: "p", Name: "dev"})
    _ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "o", ProjectID: "p", StackID: "s", Status: "planned", StartedAt: time.Now()})
    _ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t", OrgID: "o", DeploymentID: "d", ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "r", Status: "planned", StartedAt: time.Now()})

    _ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
        DeploymentTargetID: "t", OrgID: "o", DetectedAt: time.Now().UTC(), HasDrift: true, SummaryJSON: []byte(`{"v":1}`),
    })
    _ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
        DeploymentTargetID: "t", OrgID: "o", DetectedAt: time.Now().UTC(), HasDrift: false, SummaryJSON: []byte(`{"v":2}`),
    })
    d, _ := r.DriftStatus().Get(ctx, "o", "t")
    if d == nil || d.HasDrift {
        t.Errorf("upsert should replace: %+v", d)
    }
    if string(d.SummaryJSON) != `{"v":2}` {
        t.Errorf("summary not replaced: %s", d.SummaryJSON)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./internal/inventory/sqlite/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add internal/inventory/sqlite/ pkg/inventory/repo.go && git commit -m "sqlite: Deployments/Targets/Runs/Drift repos; add plan_file column"
```

---

## Task 7: Wire engine Plan to persist + Apply/Destroy/Drift to look up

**Files:**
- Modify: `pkg/engine/config.go` (default to nullRepo when InventoryRepo is nil)
- Modify: `pkg/engine/plan.go` (persist on Plan; reconstitute on Apply(planID))
- Create: `pkg/engine/inventory.go` (helpers)
- Create: `pkg/engine/inventory_test.go`

- [ ] **Step 1: Make nullRepo the default**

In `pkg/engine/config.go`, replace `New()`:

```go
func New(ctx context.Context, cfg Config) (Engine, error) {
    if cfg.CloudAdapters == nil {
        return nil, errors.New("engine.New: CloudAdapters registry is required")
    }
    if cfg.InventoryRepo == nil {
        cfg.InventoryRepo = inventory.NewNullRepo()
    }
    return &runtimeEngine{cfg: cfg}, nil
}
```

(Add `"github.com/klehmer/nimbusfab/pkg/inventory"` to imports.)

- [ ] **Step 2: Implement helpers**

Create `pkg/engine/inventory.go`:

```go
package engine

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/klehmer/nimbusfab/pkg/inventory"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// ensureOrgProjectStack upserts the org/project/stack rows for an inventory-
// backed engine call. Returns the IDs. In no-inventory mode this is a no-op
// that returns synthetic IDs; the engine's downstream code doesn't need them.
func (e *runtimeEngine) ensureOrgProjectStack(ctx context.Context, project *ir.Project, stackName string) (orgID, projectID, stackID string, err error) {
    orgID = e.orgID()
    if _, ok := e.cfg.InventoryRepo.(nullRepoDetector); ok {
        return orgID, "local-" + project.Name, "local-" + project.Name + "-" + stackName, nil
    }
    // Try to find existing org.
    _ = e.cfg.InventoryRepo.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: orgID})

    existingProjects, _ := e.cfg.InventoryRepo.Projects().List(ctx, orgID)
    for _, p := range existingProjects {
        if p.Name == project.Name {
            projectID = p.ID
            break
        }
    }
    if projectID == "" {
        projectID = "proj-" + uuid.NewString()
        if err = e.cfg.InventoryRepo.Projects().Create(ctx, inventory.Project{
            ID: projectID, OrgID: orgID, Name: project.Name,
        }); err != nil {
            return "", "", "", fmt.Errorf("project upsert: %w", err)
        }
    }

    stack := project.Stacks[stackName]
    cfgJSON, _ := json.Marshal(stack.StateBackend.Config)
    s, _ := e.cfg.InventoryRepo.Stacks().GetByName(ctx, orgID, projectID, stackName)
    if s == nil {
        stackID = "stk-" + uuid.NewString()
    } else {
        stackID = s.ID
    }
    if err = e.cfg.InventoryRepo.Stacks().Upsert(ctx, inventory.Stack{
        ID: stackID, OrgID: orgID, ProjectID: projectID, Name: stackName,
        StateBackendKind: stack.StateBackend.Kind,
        StateBackendCfg:  cfgJSON,
    }); err != nil {
        return "", "", "", fmt.Errorf("stack upsert: %w", err)
    }
    return orgID, projectID, stackID, nil
}

// persistPlan writes the deployment + targets + plan runs for a freshly
// computed PlanResult. Mutates plan.DeploymentID to the persisted UUID.
func (e *runtimeEngine) persistPlan(ctx context.Context, project *ir.Project, stackName string, opts PlanOpts, plan *provisioner.PlanResult) error {
    if _, ok := e.cfg.InventoryRepo.(nullRepoDetector); ok {
        return nil
    }
    orgID, projectID, stackID, err := e.ensureOrgProjectStack(ctx, project, stackName)
    if err != nil {
        return err
    }
    // Upsert components.
    for _, comp := range project.Components {
        irJSON, _ := json.Marshal(comp)
        if err := e.cfg.InventoryRepo.Components().Upsert(ctx, inventory.Component{
            ID: "cmp-" + uuid.NewString(), OrgID: orgID, ProjectID: projectID, StackID: stackID,
            Name: comp.Name, Type: comp.Type, IRJSON: irJSON,
        }); err != nil {
            return fmt.Errorf("component upsert: %w", err)
        }
    }
    // Create deployment.
    deploymentID := "dep-" + uuid.NewString()
    if err := e.cfg.InventoryRepo.Deployments().Create(ctx, inventory.Deployment{
        ID: deploymentID, OrgID: orgID, ProjectID: projectID, StackID: stackID,
        Status: "planned", PartialFailurePolicy: string(opts.PartialFailure),
        StartedAt: time.Now().UTC(),
    }); err != nil {
        return fmt.Errorf("deployment create: %w", err)
    }
    plan.DeploymentID = deploymentID
    // Create targets + plan runs.
    now := time.Now().UTC()
    for i := range plan.Targets {
        tp := &plan.Targets[i]
        targetID := "tgt-" + uuid.NewString()
        tp.DeploymentTargetID = targetID
        if err := e.cfg.InventoryRepo.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
            ID: targetID, OrgID: orgID, DeploymentID: deploymentID,
            ComponentName: tp.Component, Cloud: tp.Cloud, Region: tp.Region,
            CredentialRef: "",
            WorkspacePath: tp.WorkspaceDir, PlanFile: tp.PlanFile,
            Status:    "planned",
            StartedAt: now,
        }); err != nil {
            return fmt.Errorf("target create: %w", err)
        }
        runFinished := now
        if err := e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
            ID: "run-" + uuid.NewString(), OrgID: orgID, DeploymentTargetID: targetID,
            Kind: "plan", Status: "succeeded", StartedAt: now, FinishedAt: &runFinished,
        }); err != nil {
            return fmt.Errorf("plan run create: %w", err)
        }
    }
    return nil
}

// reconstitutePlan rebuilds a PlanResult from inventory rows for Apply/Destroy/Drift by ID.
func (e *runtimeEngine) reconstitutePlan(ctx context.Context, deploymentID string) (*provisioner.PlanResult, *inventory.Deployment, error) {
    orgID := e.orgID()
    d, err := e.cfg.InventoryRepo.Deployments().Get(ctx, orgID, deploymentID)
    if err != nil {
        return nil, nil, err
    }
    if d == nil {
        return nil, nil, inventory.ErrDeploymentNotFound
    }
    targets, err := e.cfg.InventoryRepo.DeploymentTargets().ListByDeployment(ctx, orgID, deploymentID)
    if err != nil {
        return nil, nil, fmt.Errorf("list targets: %w", err)
    }
    plan := &provisioner.PlanResult{
        DeploymentID:   deploymentID,
        PartialFailure: provisioner.PartialFailurePolicy(d.PartialFailurePolicy),
        GeneratedAt:    d.StartedAt,
    }
    for _, t := range targets {
        plan.Targets = append(plan.Targets, provisioner.TargetPlan{
            DeploymentTargetID: t.ID,
            Component:          t.ComponentName,
            Cloud:              t.Cloud,
            Region:             t.Region,
            WorkspaceDir:       t.WorkspacePath,
            PlanFile:           t.PlanFile,
        })
    }
    return plan, d, nil
}

// nullRepoDetector lets ensureOrgProjectStack detect the no-op repo cheaply.
// Implemented by inventory.NewNullRepo() via a marker method.
type nullRepoDetector interface {
    isNullRepo() bool
}

// errDeploymentNotApplyable wraps a friendlier error for status mismatch.
func errDeploymentNotApplyable(d *inventory.Deployment) error {
    return fmt.Errorf("%w: deployment %s is in status %q (expected 'planned')",
        inventory.ErrDeploymentWrongStatus, d.ID, d.Status)
}

// ensure errors package usage; harmless when only used through fmt.Errorf above.
var _ = errors.New
```

For the `nullRepoDetector` to work, add a marker method to the null repo. In `pkg/inventory/nullrepo.go`, add:

```go
func (nullRepo) isNullRepo() bool { return true }
```

- [ ] **Step 3: Wire Plan to call persistPlan**

In `pkg/engine/plan.go`, replace the Plan method body:

```go
func (e *runtimeEngine) Plan(ctx context.Context, project *ir.Project, stack string, opts PlanOpts) (*PlanResult, error) {
    p, err := e.newProvisioner()
    if err != nil {
        return nil, fmt.Errorf("engine.Plan: %w", err)
    }
    res, err := p.Plan(ctx, provisioner.PlanInput{
        Project:        project,
        Stack:          stack,
        OrgID:          e.orgID(),
        DeploymentID:   "dep-" + uuid.NewString(), // overwritten in persistPlan
        PartialFailure: opts.PartialFailure,
        Refresh:        opts.RefreshState,
        Targets:        opts.Targets,
    })
    if err != nil {
        return nil, err
    }
    if err := e.persistPlan(ctx, project, stack, opts, res); err != nil {
        return nil, fmt.Errorf("engine.Plan: persist: %w", err)
    }
    return res, nil
}
```

- [ ] **Step 4: Wire Apply / Destroy / DetectDrift to look up**

In `pkg/engine/plan.go`, replace the stubs:

```go
func (e *runtimeEngine) Apply(ctx context.Context, planID string, opts ApplyOpts) (string, error) {
    if _, ok := e.cfg.InventoryRepo.(nullRepoDetector); ok {
        return "", inventory.ErrInventoryRequired
    }
    plan, d, err := e.reconstitutePlan(ctx, planID)
    if err != nil {
        return "", err
    }
    if d.Status != "planned" {
        return "", errDeploymentNotApplyable(d)
    }
    res, err := e.ApplyWithPlan(ctx, plan, opts)
    if err != nil {
        return "", err
    }
    // Update inventory: deployment + per-target status.
    finished := time.Now().UTC()
    _ = e.cfg.InventoryRepo.Deployments().UpdateStatus(ctx, e.orgID(), planID, string(res.Status), &finished)
    for _, tr := range res.TargetResults {
        _ = e.cfg.InventoryRepo.DeploymentTargets().UpdateStatus(ctx, e.orgID(), tr.DeploymentTargetID, string(tr.Status), &tr.FinishedAt)
        // Create an apply run row for each target.
        _ = e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
            ID: "run-" + uuid.NewString(), OrgID: e.orgID(), DeploymentTargetID: tr.DeploymentTargetID,
            Kind: "apply", Status: string(tr.Status), StartedAt: tr.StartedAt, FinishedAt: &tr.FinishedAt,
        })
    }
    return planID, nil
}

func (e *runtimeEngine) Destroy(ctx context.Context, deploymentID string, opts DestroyOpts) (string, error) {
    if _, ok := e.cfg.InventoryRepo.(nullRepoDetector); ok {
        return "", inventory.ErrInventoryRequired
    }
    plan, _, err := e.reconstitutePlan(ctx, deploymentID)
    if err != nil {
        return "", err
    }
    res, err := e.DestroyWithPlan(ctx, plan, opts)
    if err != nil {
        return "", err
    }
    finished := time.Now().UTC()
    _ = e.cfg.InventoryRepo.Deployments().UpdateStatus(ctx, e.orgID(), deploymentID, "destroyed", &finished)
    for _, tr := range res.TargetResults {
        finalStatus := "destroyed"
        if tr.Status == provisioner.RunStatusFailed {
            finalStatus = "failed"
        }
        _ = e.cfg.InventoryRepo.DeploymentTargets().UpdateStatus(ctx, e.orgID(), tr.DeploymentTargetID, finalStatus, &tr.FinishedAt)
        _ = e.cfg.InventoryRepo.Runs().Create(ctx, inventory.Run{
            ID: "run-" + uuid.NewString(), OrgID: e.orgID(), DeploymentTargetID: tr.DeploymentTargetID,
            Kind: "destroy", Status: string(tr.Status), StartedAt: tr.StartedAt, FinishedAt: &tr.FinishedAt,
        })
    }
    return deploymentID, nil
}

func (e *runtimeEngine) DetectDrift(ctx context.Context, deploymentID string) (*DriftReport, error) {
    if _, ok := e.cfg.InventoryRepo.(nullRepoDetector); ok {
        return nil, inventory.ErrInventoryRequired
    }
    plan, _, err := e.reconstitutePlan(ctx, deploymentID)
    if err != nil {
        return nil, err
    }
    rep, err := e.DetectDriftWithPlan(ctx, plan)
    if err != nil {
        return nil, err
    }
    // Upsert drift_status per target.
    now := time.Now().UTC()
    for _, tr := range rep.TargetReports {
        summary, _ := json.Marshal(tr)
        _ = e.cfg.InventoryRepo.DriftStatus().Upsert(ctx, inventory.DriftRecord{
            DeploymentTargetID: tr.DeploymentTargetID, OrgID: e.orgID(),
            DetectedAt: now, HasDrift: tr.HasDrift, SummaryJSON: summary,
        })
    }
    return rep, nil
}
```

(Add `encoding/json`, `time`, and `inventory` imports as needed.)

- [ ] **Step 5: Write engine test**

Create `pkg/engine/inventory_test.go`:

```go
package engine_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/engine"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func mkProject() *ir.Project {
    return &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1, Name: "demo",
        Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "web", Type: "network",
            Spec:    map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }
}

func TestEngine_PlanThenApplyByID(t *testing.T) {
    repo, _ := sqlite.Open("sqlite::memory:")
    defer repo.Close()
    if err := repo.Migrate(context.Background()); err != nil { t.Fatalf("migrate: %v", err) }

    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

    eng, err := engine.New(context.Background(), engine.Config{
        CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
        InventoryRepo: repo,
    })
    if err != nil { t.Fatalf("New: %v", err) }

    plan, err := eng.Plan(context.Background(), mkProject(), "dev", engine.PlanOpts{})
    if err != nil { t.Fatalf("Plan: %v", err) }
    if plan.DeploymentID == "" { t.Fatal("DeploymentID empty after persistPlan") }

    runID, err := eng.Apply(context.Background(), plan.DeploymentID, engine.ApplyOpts{})
    if err != nil { t.Fatalf("Apply: %v", err) }
    if runID != plan.DeploymentID {
        t.Errorf("Apply returned %q, want %q", runID, plan.DeploymentID)
    }
}

func TestEngine_ApplyMissingDeployment(t *testing.T) {
    repo, _ := sqlite.Open("sqlite::memory:")
    defer repo.Close()
    _ = repo.Migrate(context.Background())
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    eng, _ := engine.New(context.Background(), engine.Config{
        CloudAdapters: reg, TofuRunner: tofu.NewFakeRunner(), WorkRoot: t.TempDir(),
        InventoryRepo: repo,
    })
    _, err := eng.Apply(context.Background(), "nonexistent", engine.ApplyOpts{})
    if err == nil { t.Fatal("expected error for missing deployment") }
}

func TestEngine_NoInventory_ApplyByIDRejected(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    eng, _ := engine.New(context.Background(), engine.Config{
        CloudAdapters: reg, TofuRunner: tofu.NewFakeRunner(), WorkRoot: t.TempDir(),
    })
    _, err := eng.Apply(context.Background(), "anything", engine.ApplyOpts{})
    if err == nil { t.Fatal("expected ErrInventoryRequired") }
}
```

- [ ] **Step 6: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./pkg/engine/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add pkg/engine/ pkg/inventory/nullrepo.go && git commit -m "engine: wire Plan to persist + Apply/Destroy/DetectDrift to look up by deployment ID"
```

---

## Task 8: CLI inventory plumbing (--inventory-dsn, --no-inventory)

**Files:**
- Modify: `cmd/cli/main.go` (root flags + default DSN)
- Modify: `cmd/cli/plan.go` (open inventory, pass to engine, print deployment ID)
- Modify: `cmd/cli/apply.go` (accept positional deployment ID OR --stack form)
- Modify: `cmd/cli/destroy.go` (accept positional deployment ID OR --stack form)
- Modify: `cmd/cli/drift.go` (accept positional deployment ID OR --stack form)
- Create: `cmd/cli/inventory.go` (shared helpers)
- Create: `cmd/cli/inventory_test.go`

- [ ] **Step 1: Shared helpers**

Create `cmd/cli/inventory.go`:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

// openInventory returns a Repo per the flags: NullRepo if --no-inventory or
// dsn is empty; otherwise a SQLite Repo. Caller is responsible for Close().
func openInventory(ctx context.Context, dsn string, noInventory bool) (inventory.Repo, error) {
    if noInventory {
        return inventory.NewNullRepo(), nil
    }
    if dsn == "" {
        dsn = os.Getenv("NIMBUSFAB_INVENTORY_DSN")
    }
    if dsn == "" {
        home, _ := os.UserHomeDir()
        if home == "" {
            return inventory.NewNullRepo(), nil
        }
        dir := filepath.Join(home, ".config", "nimbusfab")
        if err := os.MkdirAll(dir, 0o700); err != nil {
            return nil, fmt.Errorf("inventory dir: %w", err)
        }
        dsn = "sqlite://" + filepath.Join(dir, "inventory.db")
    }
    repo, err := sqlite.Open(dsn)
    if err != nil {
        return nil, fmt.Errorf("inventory open: %w", err)
    }
    if err := repo.Migrate(ctx); err != nil {
        repo.Close()
        return nil, fmt.Errorf("inventory migrate: %w", err)
    }
    return repo, nil
}
```

- [ ] **Step 2: Add root flags**

In `cmd/cli/main.go`, replace `main()`:

```go
var (
    flagInventoryDSN string
    flagNoInventory  bool
)

func main() {
    root := &cobra.Command{
        Use:           "nimbusfab",
        Short:         "Multi-cloud Infrastructure-as-Code framework over OpenTofu",
        SilenceUsage:  true,
        SilenceErrors: true,
    }
    root.PersistentFlags().StringVar(&flagInventoryDSN, "inventory-dsn", "",
        "inventory DB DSN (default: sqlite://~/.config/nimbusfab/inventory.db)")
    root.PersistentFlags().BoolVar(&flagNoInventory, "no-inventory", false,
        "disable inventory persistence; all operations are in-process only")
    root.AddCommand(newValidateCommand())
    root.AddCommand(newPlanCommand())
    root.AddCommand(newApplyCommand())
    root.AddCommand(newDestroyCommand())
    root.AddCommand(newDriftCommand())

    if err := root.Execute(); err != nil {
        os.Exit(1)
    }
}
```

- [ ] **Step 3: Wire `plan` to inventory + print deployment ID**

In `cmd/cli/plan.go`, in the `RunE` body of `newPlanCommand`, replace the engine construction:

```go
repo, err := openInventory(cmd.Context(), flagInventoryDSN, flagNoInventory)
if err != nil { fmt.Fprintf(cmd.ErrOrStderr(), "inventory: %v\n", err); os.Exit(1) }
defer repo.Close()
```

And in `runPlan`, after the engine construction, replace:

```go
eng, err := engine.New(ctx, engine.Config{
    CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot,
})
```

with:

```go
eng, err := engine.New(ctx, engine.Config{
    CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot,
    InventoryRepo: in.Inventory,
})
```

Add `Inventory inventory.Repo` to `planArgs`. Print the deployment ID at the end of the summary:

```go
if result.DeploymentID != "" {
    fmt.Fprintf(in.Stdout, "Deployment ID: %s\n", result.DeploymentID)
    fmt.Fprintf(in.Stdout, "  (run `nimbusfab apply %s` to deploy)\n", result.DeploymentID)
}
```

Pass `repo` from the cobra RunE into `runPlan`'s args.

- [ ] **Step 4: Wire `apply` to accept either form**

In `cmd/cli/apply.go`, restructure the command:

```go
func newApplyCommand() *cobra.Command {
    var stack, partialFailure string
    var autoApprove bool
    cmd := &cobra.Command{
        Use:   "apply [deployment-id | path]",
        Short: "Apply by deployment ID (preferred), or validate-plan-apply against a stack",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            repo, err := openInventory(cmd.Context(), flagInventoryDSN, flagNoInventory)
            if err != nil { return err }
            defer repo.Close()

            arg := ""
            if len(args) == 1 { arg = args[0] }

            reg := cloud.NewRegistry()
            if err := reg.Register(aws.New()); err != nil { return err }

            code := runApply(cmd.Context(), applyArgs{
                PositionalArg:  arg,
                Stack:          stack,
                AutoApprove:    autoApprove,
                PartialFailure: partialFailure,
                Adapters:       reg,
                Runner:         tofu.NewExecRunner(),
                Inventory:      repo,
                Stdout:         cmd.OutOrStdout(),
                Stderr:         cmd.ErrOrStderr(),
            })
            if code != 0 { os.Exit(code) }
            return nil
        },
        SilenceUsage: true, SilenceErrors: true,
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack to plan + apply (only when no deployment ID given)")
    cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
    cmd.Flags().StringVar(&partialFailure, "partial-failure", "leave", "leave | rollback | retry-failed")
    return cmd
}
```

And update `applyArgs` and `runApply`:

```go
type applyArgs struct {
    PositionalArg  string  // deployment ID OR path
    Stack          string
    AutoApprove    bool
    PartialFailure string
    Adapters       cloud.Registry
    Runner         tofu.Runner
    Inventory      inventory.Repo
    WorkRoot       string
    Stdout, Stderr io.Writer
}

func runApply(ctx context.Context, in applyArgs) int {
    if ctx == nil { ctx = context.Background() }

    // Heuristic: a deployment ID starts with "dep-" and contains no "/".
    isDeploymentID := strings.HasPrefix(in.PositionalArg, "dep-") && !strings.Contains(in.PositionalArg, "/")

    eng, err := engine.New(ctx, engine.Config{
        CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot, InventoryRepo: in.Inventory,
    })
    if err != nil { fmt.Fprintf(in.Stderr, "engine: %v\n", err); return 1 }

    if isDeploymentID {
        _, err := eng.Apply(ctx, in.PositionalArg, engine.ApplyOpts{
            AutoApprove: true,
            PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
        })
        if err != nil { fmt.Fprintf(in.Stderr, "apply: %v\n", err); return 1 }
        fmt.Fprintf(in.Stdout, "Applied deployment %s\n", in.PositionalArg)
        return 0
    }

    // Plan + apply form.
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack required when no deployment ID given")
        return 2
    }
    projectPath := in.PositionalArg
    if projectPath == "" { projectPath = "." }
    project, err := loader.New().Load(ctx, projectPath)
    if err != nil { fmt.Fprintf(in.Stderr, "load: %v\n", err); return 1 }
    report, err := validator.New().Validate(ctx, project)
    if err != nil { fmt.Fprintf(in.Stderr, "validator: %v\n", err); return 2 }
    if report != nil && !report.OK() {
        for _, issue := range report.Issues { fmt.Fprintln(in.Stderr, issue.String()) }
        return 1
    }
    plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{
        PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
    })
    if err != nil { fmt.Fprintf(in.Stderr, "plan: %v\n", err); return 1 }
    fmt.Fprintf(in.Stdout, "Planning %d targets... done\n", len(plan.Targets))
    res, err := eng.ApplyWithPlan(ctx, plan, engine.ApplyOpts{
        AutoApprove: true,
        PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
    })
    if err != nil { fmt.Fprintf(in.Stderr, "apply: %v\n", err); return 1 }
    printApplyResult(in.Stdout, res)
    if res.Status != provisioner.ApplySucceeded { return 1 }
    return 0
}

// (Extract the result-printing into a helper for reuse — see plan file)
```

Apply analogous changes to `destroy.go` and `drift.go`. They both accept a positional deployment ID first; fall back to `--stack` planning form.

- [ ] **Step 5: Write CLI test**

Create `cmd/cli/inventory_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestPlanThenApplyByID(t *testing.T) {
    repo, _ := sqlite.Open("sqlite::memory:")
    defer repo.Close()
    if err := repo.Migrate(context.Background()); err != nil { t.Fatalf("migrate: %v", err) }

    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

    var planOut, planErr bytes.Buffer
    code := runPlan(context.Background(), planArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        Adapters:    reg, Runner: runner, Inventory: repo, WorkRoot: t.TempDir(),
        Stdout: &planOut, Stderr: &planErr,
    })
    if code != 0 { t.Fatalf("plan: exit %d stderr=%s", code, planErr.String()) }
    if !strings.Contains(planOut.String(), "Deployment ID:") {
        t.Fatalf("plan output missing deployment ID:\n%s", planOut.String())
    }
    var deploymentID string
    for _, line := range strings.Split(planOut.String(), "\n") {
        if strings.HasPrefix(line, "Deployment ID:") {
            deploymentID = strings.TrimSpace(strings.TrimPrefix(line, "Deployment ID:"))
            break
        }
    }
    if !strings.HasPrefix(deploymentID, "dep-") {
        t.Fatalf("deployment ID malformed: %q", deploymentID)
    }

    var applyOut, applyErr bytes.Buffer
    code = runApply(context.Background(), applyArgs{
        PositionalArg: deploymentID,
        AutoApprove:   true,
        Adapters:      reg, Runner: runner, Inventory: repo, WorkRoot: t.TempDir(),
        Stdout: &applyOut, Stderr: &applyErr,
    })
    if code != 0 { t.Fatalf("apply: exit %d stderr=%s", code, applyErr.String()) }
    if !strings.Contains(applyOut.String(), "Applied deployment") {
        t.Errorf("apply output: %s", applyOut.String())
    }
}
```

- [ ] **Step 6: Run + commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./cmd/cli/ -v
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go build -o bin/nimbusfab ./cmd/cli
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add cmd/cli/ && git commit -m "cli: --inventory-dsn / --no-inventory flags; apply/destroy/drift accept deployment ID"
```

---

## Task 9: README + CHANGELOG

**Files:**
- Modify: `README.md` (add inventory section, update statuses)
- Modify: `CHANGELOG.md` (Inventory Phase 1 section)

- [ ] **Step 1: README updates**

Update the Status line and add a section under Commands:

```markdown
## Inventory

Phase 1 ships a SQLite-backed inventory that persists every Plan, Apply,
Destroy, and Drift across processes. By default `~/.config/nimbusfab/inventory.db`
is used; override with `--inventory-dsn sqlite:///path/to/inventory.db` or
disable entirely with `--no-inventory` (useful in CI).

`nimbusfab plan` now returns a Deployment ID. `nimbusfab apply <deployment-id>`
applies that plan later, possibly from a different shell. `nimbusfab destroy
<deployment-id>` tears it down. `nimbusfab drift <deployment-id>` runs
refresh-only against it.
```

- [ ] **Step 2: CHANGELOG update**

```markdown
## Unreleased — Inventory Persistence Phase 1

### Added

- SQLite inventory backend (`internal/inventory/sqlite`) built on
  modernc.org/sqlite (CGo-free).
- Embedded migration runner (`pkg/inventory/migrations.go`) that picks
  flavor-specific SQL files via `//go:embed` and tracks applied versions in
  `schema_migrations`.
- `pkg/inventory.NewNullRepo()` for `--no-inventory` mode: writes no-op,
  reads return `inventory.ErrInventoryRequired`.
- `nimbusfab plan` now returns a Deployment ID and persists project,
  stack, components, deployment, deployment_targets, and plan runs to
  the inventory.
- `nimbusfab apply <deployment-id>` / `destroy <deployment-id>` /
  `drift <deployment-id>` operate against persisted deployments and update
  status rows / drift_status / per-target apply runs.
- CLI flags: `--inventory-dsn`, `--no-inventory`.
- `plan_file` column added to `deployment_targets` (SQLite migration) so
  Apply by ID can locate the saved plan binary.

### Out of scope (deferred)

- Postgres backend (future phase; contract is shared so it slots in cleanly).
- Web auth / api_tokens / OIDC / users (web app phase).
- Run log persistence (server phase).
- Cost write paths (cost specs).
- `nimbusfab runs status` / `deployments list` CLI commands.
```

- [ ] **Step 3: Final verification**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./...
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go vet ./...
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH gofmt -l .
```

- [ ] **Step 4: Commit**

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && git add README.md CHANGELOG.md && git commit -m "docs: README + CHANGELOG for Inventory Persistence Phase 1"
```

---

## Final verification + smoke

```bash
cd /home/kurt/git/nimbusfab-inventory-phase1 && PATH=$HOME/.local/go/bin:$PATH go test ./...
cd /home/kurt/git/nimbusfab-inventory-phase1 && ./bin/nimbusfab --help | grep -E "inventory|apply|destroy|drift|plan"
cd /home/kurt/git/nimbusfab-inventory-phase1 && ./bin/nimbusfab plan --stack dev --inventory-dsn sqlite:///tmp/test-inventory.db cmd/cli/testdata/network-only-project 2>&1 | tail -10
# Should print a Deployment ID. Then:
cd /home/kurt/git/nimbusfab-inventory-phase1 && ./bin/nimbusfab apply --inventory-dsn sqlite:///tmp/test-inventory.db <deployment-id> 2>&1 | tail -5
# Will fail at `tofu init` since tofu isn't installed; that's expected.
```

End-state: a user with `tofu` installed and AWS creds can:
1. Run `nimbusfab plan --stack dev` → get a Deployment ID, see it persisted in `~/.config/nimbusfab/inventory.db`.
2. Days later, from another shell, `nimbusfab apply <deployment-id>` → applies that exact plan.
3. `nimbusfab drift <deployment-id>` → reports current drift, persists `drift_status` row.
4. `nimbusfab destroy <deployment-id>` → tears it down.

The substrate for the web app (which queries the same rows), cost ingestion (which writes `cost_estimates` / `cost_actuals`), and the GitOps daemon (which polls `drift_status`) is now in place.
