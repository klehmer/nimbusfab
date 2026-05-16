# Inventory Persistence Subsystem Spec

**Status:** Subsystem spec. Defines the lifecycle, lookup contracts, and runtime wiring of the inventory DB layer. The repo interfaces are already locked in `pkg/inventory/repo.go` (architecture-spec scaffold) and the initial Postgres schema in `pkg/inventory/migrations/0001_init.sql`. This spec fills in the operational picture: when rows are written, how no-inventory mode behaves, how engine surfaces resolve IDs back into runtime state, what SQLite vs. Postgres flavor differences look like, and what transactional boundaries the engine relies on.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (locked module boundaries, run model, `org_id` tenancy invariant)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (Plan/Apply/Destroy/Drift orchestration; defines what rows the inventory captures)

**Depended on by:**
- Web app spec (consumes deployments / runs / run_logs streams)
- Cost dashboard spec (consumes cost_actuals + cost_estimates)
- GitOps daemon spec (consumes deployments + drift_status to decide reconciliation)
- The eventual multi-tenant SaaS deployment (turns on the `org_id` scoping that's already in the schema)

---

## Context

Through DSL/IR Phase 1 and Provisioner Phases 1–2, every engine surface has worked in-memory: a caller hands the engine a `*ir.Project`, gets a `PlanResult`, and passes it directly to `ApplyWithPlan`/`DestroyWithPlan`/`DetectDriftWithPlan`. There is no notion of "the plan from yesterday" or "the deployment we created last week." Every run is self-contained.

This spec turns the engine into a stateful service. Plans land in `deployments` + `deployment_targets` + `runs`. Applies update those rows in place. Drift queries the latest deployment for a `(project, stack)`. Destroy operates on a `deployment_id`, not a `PlanResult`. The CLI's `apply`/`destroy`/`drift` commands continue to work plan→apply in one shot for solo developers, but the same engine surface now powers the web app's "click Deploy on the latest plan" flow.

**Design principles:**
1. **No-inventory mode stays a first-class citizen.** A user with `--no-inventory` (or no DSN configured) still gets working `validate` / `plan` / `apply` / `destroy` / `drift`. Inventory is an *augmentation*, not a precondition. Internally, no-inventory mode plugs in a `nullRepo` that no-ops every write and errors on every lookup that would require persistence.
2. **One transaction per orchestration step.** Plan writes its rows in one tx; per-target Apply writes its rows in another tx; destroy mirrors. The engine never holds a transaction across a `tofu` subprocess — too long, too lock-prone. Idempotency keys handle retries.
3. **Append-only by default; UPDATE only for status.** Inventory rows for deployments / runs / targets are created at start and only updated to record their final status / `finished_at`. Mutability is constrained so audit reconstruction is straightforward.
4. **Schema is the contract.** The Go types in `pkg/inventory/repo.go` and the SQL migrations under `pkg/inventory/migrations/` are the source of truth; both implementations (SQLite, Postgres) MUST be byte-for-byte equivalent on row shape, only differing on storage-engine concerns (types, indexes, JSONB vs. JSON1).
5. **`org_id` is a load-bearing column even in single-tenant mode.** SQLite installations hardcode `org_id = "local"`; every query still filters on it. Turning multi-tenancy on means setting `org_id` from session resolution — no schema change.

---

## Scope

**In scope (this spec):**
- Lifecycle of every inventory entity: when it's created, when it's updated, when it's deleted, what invariants hold at each transition.
- Engine surface contracts in inventory mode: how `Apply(planID)` / `Destroy(deploymentID)` / `DetectDrift(deploymentID)` resolve their inputs.
- No-inventory mode boundary: which methods work, which return `ErrInventoryRequired`, how the CLI degrades cleanly.
- SQLite vs. Postgres flavor decisions: types, defaults, JSON storage, migration tooling.
- Migration tooling: `golang-migrate/migrate`-style versioned migrations, embedded via `//go:embed`, automatic apply on `Repo.Migrate(ctx)`.
- Transactional boundaries: what gets one transaction, what does not, how partial-failure interacts with row updates.
- Idempotency: how clients reissuing the same Apply don't create duplicate runs.
- `nullRepo` semantics: what no-op + error returns look like.
- Multi-tenancy posture: `org_id` resolution path in CLI (`local`) vs. web app (session).
- Repository test contract: a shared test suite that both SQLite and Postgres implementations must pass.

**Out of scope (deferred):**
- Postgres impl. Phase 1 ships SQLite only; Postgres is its own phase (adds container-based CI, connection pooling, statement_timeout tuning). The contract is the same.
- Web auth (`api_tokens`, OIDC users beyond `is_local`). The columns exist; the surface is the web app spec.
- Cost data write paths. `cost_estimates` / `cost_actuals` tables exist; the estimator and collector specs wire them.
- Run log archival to object storage. Phase 1 keeps `run_logs` in the DB; the off-load policy is a server-mode optimization.
- Background jobs (drift detection on a schedule, deployment GC). The GitOps daemon spec covers these.
- Migration rollbacks. v1 only adds; if a migration is wrong, the next migration fixes it forward. Down-migrations land if a real user-visible need emerges.

---

## Entity lifecycles

Each lifecycle below describes the SQL row state machine. The narrative is concrete: which method on which repo, what's the row content at each step, what's mutable.

### Org

- `Create(o)` at first-startup of a fresh DB (migration's seed) OR explicit admin action. SQLite installations create a single `(id="local", name="local")` row at `Repo.Migrate(ctx)` time.
- Never updated.
- Never deleted in v1 (would cascade everything).

### Project

- `Create(p)` on first inventory-aware engine call that references a project name that doesn't exist. The engine calls `Projects().Create()` lazily.
- `(org_id, name)` is the unique key. `id` is a UUID generated at create time.
- `source_uri` is a free-text pointer (git URL, local path) — never queried; recorded for audit.
- Updated only by an explicit "edit project" API in the web app phase. CLI doesn't expose updates.

### Stack

- `Upsert(s)` on every `Plan` / `Apply` / `Destroy` / `Drift` call: the stack named in the input is upserted into the inventory before the operation proceeds. `state_backend_kind` and `state_backend_cfg` reflect the IR's `Stack.StateBackend`.
- `(org_id, project_id, name)` unique.
- Mutable: `state_backend_*` change when the user changes their YAML.

### Component

- `Upsert(c)` per component per stack at the end of a successful `Plan`. The full IR JSON is stored, snapshot-per-plan.
- `(org_id, project_id, stack_id, name)` unique. Each new plan REPLACES the row.
- Used for: audit ("what was X's spec at the time we last planned Y?") and the web app's stack inspector.

### Composition

- `Upsert(c)` per Composition kind per project, same cadence as Components.
- `(org_id, project_id, kind)` unique.

### Deployment

- `Create(d)` at the start of `Apply(planID)`. Initial `status = "running"`, `started_at = now()`.
- `UpdateStatus(id, status, finished_at)` once orchestration completes, with terminal status `succeeded` / `partial_failure` / `failed` / `rollback_failed`.
- Never updated again after terminal status.
- Phase 1 caveat: since Plan now persists its own targets to support Apply-by-ID, a deployment row is also created at Plan time with status `planned`. Apply transitions it to `running` → terminal. Destroy starts a *separate* deployment row with `status="destroying"` → terminal.

### DeploymentTarget

- `Create(t)` per target as the orchestrator dispatches it. Initial `status = "queued"`, `started_at` set immediately. `workspace_path` populated from the provisioner's path computation.
- `UpdateStatus(id, status, finished_at)` to `succeeded` / `failed` / `skipped` / `reverted`.
- Phase 1 caveat: same as Deployment — Plan creates targets with `status="planned"` and a `workspace_path`. Apply mutates the same rows.

### Run

- `Create(r)` per `tofu` invocation. Initial `status = "running"`, `kind = "plan" | "apply" | "destroy"`, `started_at = now()`.
- `UpdateStatus(id, status, exit_code, finished_at)` on subprocess completion.
- One Run per (DeploymentTarget, kind) per orchestration step. A failed `tofu apply` followed by a `retry-failed` policy retry creates a *new* Run row; the original is preserved.

### RunLog

- `Append([]RunLogLine)` in batches as `RunEvent`s stream off the runner.
- Read-only after run terminal status.
- Phase 1: stored in-DB; server mode optionally archives older logs (deferred).

### DriftStatus

- `Upsert(d)` per `DetectDrift` call, one row per `(org_id, deployment_target_id)`. Each new drift run replaces the row.
- The full `DriftReport` JSON is denormalized into `summary_json`. Historical drift requires querying old `runs` of kind `drift` (Phase 2 adds `kind = "drift"` to the runs enum).

### CostEstimate / CostActual / SecretsRef / AuditLog

Reserved for cost / secrets / web phases. Inventory Phase 1 leaves these tables in the schema but unwired — Phase 1's `nullRepo` implementations of these specific sub-repos return `ErrNotImplementedYet` so accidental usage fails loudly.

---

## Engine surface in inventory mode

The Engine interface gets one new contract: when `cfg.InventoryRepo != nil`, the inventory-aware paths activate. Same method signatures, different behavior.

### Plan

```go
Plan(ctx, *ir.Project, stack string, opts PlanOpts) (*PlanResult, error)
```

- In-memory mode: unchanged — returns a `PlanResult` populated from in-memory work.
- Inventory mode:
  1. Upsert `Project` row (lookup by name; create if missing).
  2. Upsert `Stack` row.
  3. Upsert `Component` rows (snapshot the IR per component).
  4. Create a `Deployment` row with `status = "planned"`, `partial_failure_policy = opts.PartialFailure` (or default `leave`).
  5. For each `TargetPlan` the provisioner produces, create a `DeploymentTarget` row with `status = "planned"` and `workspace_path`.
  6. Create a `Run` row per target with `kind = "plan"`, `status = "succeeded"`, `exit_code = 0` (since plan already succeeded by the time we're persisting).
  7. Stamp the returned `PlanResult.DeploymentID` with the real deployment row's UUID.

The deployment ID is now stable across plan→apply: `nimbusfab plan` returns it, `nimbusfab apply <deployment-id>` looks it up.

### Apply

```go
Apply(ctx, planID string, opts ApplyOpts) (runID string, err error)
```

- In-memory mode: returns `ErrInventoryRequired` (a stable error code clients can check). The CLI's `apply` command uses `ApplyWithPlan` when running with `--no-inventory`.
- Inventory mode:
  1. Fetch `Deployment` by `planID`; reject if status isn't `planned`.
  2. Fetch all `DeploymentTarget`s for the deployment.
  3. Reconstruct an in-memory `PlanResult` from the rows (workspace paths + plan file paths + component / cloud / region tuples).
  4. Mutate deployment status to `running`.
  5. Per target: create a new `Run` with `kind = "apply"`, `status = "running"`. Hand to the provisioner's apply worker. On terminal: update `Run` + `DeploymentTarget` status.
  6. Mutate deployment to terminal status (`succeeded` / `partial_failure` / `failed` / `rollback_failed`).
  7. Return the deployment ID as `runID` (Phase-1 simplification: caller uses `nimbusfab runs status <id>` later — but Phase 1 ships only a synchronous CLI, so `runID` is mostly informational).

### Destroy

```go
Destroy(ctx, deploymentID string, opts DestroyOpts) (runID string, err error)
```

- In-memory mode: `ErrInventoryRequired`.
- Inventory mode:
  1. Fetch `Deployment` and its targets.
  2. Create a NEW `Deployment` row with `status = "destroying"`, `partial_failure_policy = opts.PartialFailure`. The new deployment is a sibling, not a replacement.
  3. For each existing `DeploymentTarget`, create a new `DeploymentTarget` row in the destroy deployment, plus a new `Run` of `kind = "destroy"`.
  4. The provisioner's destroy worker uses the *original* workspace_path (the destroy operates against the existing state).
  5. On success per target: mark the new `Run`/`DeploymentTarget` terminal AND mark the original `DeploymentTarget` as `destroyed` (final terminal).
  6. The original `Deployment` rolls forward to status `destroyed` once all its targets are destroyed.

### DetectDrift

```go
DetectDrift(ctx, deploymentID string) (*DriftReport, error)
```

- In-memory mode: `ErrInventoryRequired`.
- Inventory mode:
  1. Fetch `Deployment` and its targets.
  2. Reconstruct a thin `PlanResult` from the rows (drift needs workspace_path + component / cloud / region — no plan file required since drift uses `-refresh-only`).
  3. Per target: create a new `Run` with `kind = "drift"`, run the drift worker.
  4. Upsert `DriftStatus` rows.
  5. Aggregate into `DriftReport` and return.

### Other entry points

- `LoadProject(ctx, path)`, `Validate(ctx, *Project)` are pure functions; no inventory interaction.
- `GetRun(ctx, runID)`, `StreamRun(ctx, runID)`: inventory-mode only. In-memory mode returns `ErrInventoryRequired`. (CLI doesn't surface these in Phase 1; they're for the web app spec.)
- `Import(ctx, ...)`: inventory-mode-recommended but not required; Phase 1 doesn't fully wire imports (it's its own future spec).
- `EstimateCost(ctx, *PlanResult)`, `GetCostActuals(...)`: orthogonal to inventory persistence; estimator/collector specs cover.

---

## No-inventory mode contract

The `nullRepo` provides a `Repo` implementation that:

- `Migrate(ctx)`, `Ping(ctx)`, `Close()`: all no-op success.
- Every sub-repo method returns:
  - For write methods (`Create`, `Upsert`, `Append`, `UpdateStatus`, `BulkInsert`): no-op success. The engine in no-inventory mode calls these unconditionally but they discard.
  - For read methods (`Get`, `GetByName`, `List`, `Query`, `Read`): `ErrInventoryRequired`. The engine in no-inventory mode NEVER calls read methods because its no-inventory code paths go through `ApplyWithPlan`/`DestroyWithPlan`/`DetectDriftWithPlan` which take the data directly.

This division is asserted: a unit test in `pkg/inventory/nullrepo_test.go` enumerates every interface method and confirms writes succeed silently while reads error.

`ErrInventoryRequired` is exported as `inventory.ErrInventoryRequired`. Engine surfaces wrap it with a `UserFacing()` message so the CLI can render "this operation needs --inventory-dsn or a working inventory."

---

## Migrations

### Tool

Phase 1 uses **embedded migrations applied at startup by a thin in-house runner** rather than pulling in `golang-migrate/migrate` as a dependency. Rationale: golang-migrate has a heavyweight CLI surface we don't need, drags in driver plugins, and complicates the SQLite vs. Postgres flavor split. Our needs are minimal: pull `*.sql` from `//go:embed`, apply in lexicographic order, record applied versions in a `schema_migrations` table, skip already-applied.

```go
type Migration struct {
    Version  string  // "0001_init"
    SQL      string
}

func (r *sqliteRepo) Migrate(ctx context.Context) error {
    // 1. CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMP);
    // 2. SELECT version FROM schema_migrations
    // 3. For each //go:embed-loaded migration not in the set: BEGIN; exec; INSERT INTO schema_migrations; COMMIT
}
```

### Layout

```
pkg/inventory/migrations/
  0001_init.sql          # existing — Postgres flavor
  0001_init.sqlite.sql   # NEW — SQLite flavor (Phase 1 writes this)
  README.md              # convention: <NNNN>_<slug>.[sqlite.]sql
```

The runner picks `.sqlite.sql` if present when running on SQLite, otherwise the bare `.sql`. Future migrations that don't need flavor differences ship only the bare `.sql`.

### Naming

`<4-digit version>_<snake_case_slug>.[sqlite.]sql`. Version is sortable; slug is human-readable.

### Idempotency

Every CREATE uses `IF NOT EXISTS`. Every migration is wrapped in a transaction. A migration that's been partially applied gets rolled back and retried.

### No down-migrations

If a migration is wrong, write the next migration that corrects forward. v1 has no users, so we still have the option to delete and re-apply migrations; once we have users, forward-only is the contract.

---

## SQLite flavor

### Engine choice

`mattn/go-sqlite3` is the historical default; we use `modernc.org/sqlite` (CGo-free) so Phase 1 stays buildable without a C toolchain. Performance is acceptable for the per-deployment write rate (single-digit writes/sec at peak).

### Type translations

| Postgres | SQLite |
|---|---|
| `UUID` | `TEXT` (string-formatted UUID) |
| `TIMESTAMPTZ NOT NULL DEFAULT now()` | `TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))` |
| `JSONB` | `TEXT` (with `json()` checks via the json1 extension, which modernc.org/sqlite includes) |
| `BYTEA` | `BLOB` |
| `NUMERIC(20,8)` | `REAL` (single-tenant; precision loss acceptable for v1 cost data — Postgres mode keeps `NUMERIC`) |
| `BIGSERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT` |
| `now()` | `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')` |

### Foreign keys

SQLite supports `FOREIGN KEY ... REFERENCES ...` and enforces them when `PRAGMA foreign_keys = ON` is set at connection time. The repo's connection initialization MUST set this.

### Concurrency

SQLite uses a single writer at a time. Phase 1's apply orchestrator can have multiple targets writing concurrently — we serialize repo writes through a single connection (`db.Exec` is threadsafe but blocks on the global write lock). For the CLI's expected workload this is fine.

### Connection string

```
sqlite:///path/to/nimbusfab.db
sqlite::memory:    (for tests only)
```

The engine accepts a `InventoryDSN` config; CLI default is `~/.config/nimbusfab/inventory.db`.

---

## Postgres flavor (not Phase 1)

Reserved for the Postgres phase:

- Driver: `jackc/pgx/v5` (modern, performant, fewer footguns than `lib/pq`).
- Connection pooling via `pgxpool`.
- `org_id`-scoped queries assume an index on `(org_id, ...)`; the existing migration adds these.
- Transaction isolation: `READ COMMITTED` for run updates; `SERIALIZABLE` would be safer for the migration runner but degrades throughput — only the migration tx uses it.
- Row-level security: not v1 (relies on every query filtering by `org_id`); reserved for future.

---

## Transactional boundaries

| Operation | Transaction scope | Notes |
|---|---|---|
| `Migrate()` | One tx per migration file | Whole file rolls back on error |
| `Plan` persistence | One tx wrapping Project / Stack upserts + Component upserts + Deployment Create + per-target DeploymentTarget Creates + per-target plan Run Creates | The provisioner has already produced everything; this is one big bulk write |
| `Apply` per target | One tx per target | Create Run (`running`) + update Run terminal + update DeploymentTarget terminal in a single tx. Deployment-level status update happens at the end in its own tx. |
| `Destroy` per target | Same pattern as Apply | Plus an additional update to the *original* DeploymentTarget marking it `destroyed` |
| `Drift` per target | One tx per target | Create Run + update Run terminal + upsert DriftStatus |
| `RunLogs.Append` | No outer tx; the call is atomic per batch | Logs CAN be lost if the process crashes between flushes — acceptable for v1 (apply re-issued via idempotency key) |

The engine NEVER holds a transaction across a `tofu` subprocess invocation. Transactions are opened only when DB writes happen.

---

## Idempotency

Re-applying the same logical plan to the engine MUST NOT create duplicate Deployments. v1 implements this with a deterministic deployment ID derived from `(project_id, stack_id, plan_content_hash)`:

```go
deploymentID := uuid.NewSHA1(namespaceProjectStack, []byte(planContentHash))
```

`planContentHash` is the sha256 of the canonical JSON serialization of the IR + stack vars at plan time. Repeating the same plan produces the same UUID; the inventory's `(deployment_id)` PRIMARY KEY rejects duplicates. The engine catches the duplicate, fetches the existing row, and returns it as-if-newly-created.

In Phase 1 this surfaces as: `nimbusfab plan` produces the same `deployment-id` for the same project state, even across re-runs. `nimbusfab apply <deployment-id>` is naturally idempotent because the apply path checks `status = "planned"` and rejects re-applies of already-running/terminal deployments. (Re-attempting after a failure uses a different path: `nimbusfab plan` produces a new `deployment-id` if the IR changed; `nimbusfab apply --retry <deployment-id>` is reserved.)

The web app's HTTP layer additionally honors an `Idempotency-Key` header (web app spec); inventory persistence is the substrate.

---

## Repository test contract

A shared test suite in `pkg/inventory/repotest/` runs against any `Repo` implementation. Phase 1 lands the suite plus the SQLite hookup. When Postgres lands, it adds a hookup test that runs the same suite against a containerized Postgres.

```go
func TestSQLite_Repo(t *testing.T) {
    repo := mustOpen(t, "sqlite::memory:")
    repotest.RunSuite(t, repo)
}
```

Suite coverage:

1. `Migrate` is idempotent (run twice, no error).
2. `Ping` works after `Migrate`.
3. Each repo's CRUD round-trip: insert, fetch, list, update if applicable.
4. `org_id` scoping: row in org A is not visible from a query scoped to org B.
5. Transactional behavior: a failing exec inside a tx leaves no rows.
6. Concurrent inserts to disjoint primary keys succeed.
7. Schema migration replay: starting from a fresh DB and running all migrations matches starting from a partial DB with old migrations already applied.

---

## CLI integration

### New flags

- `--inventory-dsn <dsn>`: override default. Empty string activates no-inventory mode.
- `--no-inventory`: shorthand for `--inventory-dsn ""`. Useful in CI.

### Default behavior

If `--inventory-dsn` is unset:
- CLI looks for `$NIMBUSFAB_INVENTORY_DSN` env var.
- If unset, defaults to `sqlite://$HOME/.config/nimbusfab/inventory.db`.
- Creates the parent directory if missing; creates the DB file if missing; runs `Migrate`.

### Per-command changes

| Command | Inventory mode | No-inventory mode |
|---|---|---|
| `validate` | unchanged | unchanged |
| `plan` | persists Project/Stack/Components/Deployment/Targets; prints the deployment ID | prints "no-inventory: deployment ID not persisted" |
| `apply` | `apply <deployment-id>` looks up the plan; `apply --stack <s>` does a plan-then-apply as one step | `apply --stack <s>` is the only form; uses `ApplyWithPlan` internally |
| `destroy` | `destroy <deployment-id>` tears down; `destroy --stack <s>` queries inventory for the latest deployment | `destroy --stack <s>` does a plan-then-destroy as one step |
| `drift` | `drift <deployment-id>` or `drift --stack <s>` (queries latest) | `drift --stack <s>` plans, then runs drift |
| `runs status <run-id>` | NEW: queries run history | `ErrInventoryRequired` |
| `deployments list` | NEW: lists recent deployments for a project | `ErrInventoryRequired` |

The CLI surface evolution: Phase 1 keeps the existing flag set working AND adds the inventory-aware forms. `nimbusfab apply --stack dev` continues to work in inventory mode by chaining plan→apply internally; the deployment row gets created during the plan step.

---

## Error model

| Code | Meaning |
|---|---|
| `ErrInventoryRequired` | A read path was called but no inventory is configured. CLI prints "this command requires --inventory-dsn or a working inventory DB". |
| `ErrInventoryUnavailable` | Inventory is configured but the underlying DB is unreachable. Different from above: this is a runtime issue, not a config one. |
| `ErrDeploymentNotFound` | `Apply/Destroy/Drift <deployment-id>` and the ID doesn't exist. |
| `ErrDeploymentWrongStatus` | `Apply <deployment-id>` but the deployment is not `planned` (e.g., already applied or destroyed). |
| `ErrMigrationConflict` | Schema migration failed mid-application; recovery is "fix the bug, re-run". |

All errors implement `UserFacing() (code, message, remediation)`.

---

## Verification (design-level)

This spec is design, not implementation. Verify by walking through scenarios:

1. **Plan-then-Apply across process boundaries.** `nimbusfab plan --stack dev` (process A) returns `deployment-id=X`. `nimbusfab apply X` (process B, days later) succeeds. Walk through every row read/written.
2. **No-inventory CI flow.** A CI pipeline runs `nimbusfab plan --no-inventory && nimbusfab apply --no-inventory` against a stack. Walk through how `apply` falls back to `ApplyWithPlan`. Confirm the engine's `nullRepo` doesn't accidentally call any read methods.
3. **Drift after deploy.** Apply → wait → drift. Walk through how drift reconstructs the plan-shaped data it needs from `deployment_targets` rows.
4. **Destroy a multi-target deployment.** A `(web-network, aws)` + `(web-network, gcp)` deployment. `destroy <id>`. Confirm two new Runs of kind=destroy are created, and that the original DeploymentTargets transition to `destroyed`.
5. **Partial failure on Apply.** Two targets, one fails. Walk through: Deployment is `partial_failure`, one DeploymentTarget is `succeeded`, the other is `failed`, both have terminal Runs.
6. **Migrations on a fresh and a half-applied DB.** Fresh: all migrations run, schema_migrations has every version. Half-applied: simulate by manually inserting `0001_init` then running Migrate; confirm only `0002+` apply.
7. **Idempotent plan.** Two identical `plan` calls produce the same `deployment-id`. The second one is a no-op (or reports "already planned, use apply"). Walk through the duplicate-key handling.

---

## Future hooks (not Phase 1)

- **Cost write paths.** `cost_estimates` Insert called by the estimator; `cost_actuals` Upsert called by the collector. Schema already in place.
- **Run log archival.** `RunLogs.Append` keeps appending to DB; a background job moves logs older than N days to S3/GCS, and the Read method falls through to object storage transparently.
- **Soft-delete + GC.** Today, `ON DELETE CASCADE` cascades from Org. v2 may want soft-delete for audit; reserved.
- **Multi-org SaaS.** Phase 1's hardcoded `org_id="local"` becomes a session-resolved value. No schema change.
- **Backup / restore.** `nimbusfab backup` / `restore` commands wrap `sqlite3 .dump` and the Postgres equivalent. Deferred until production users ask.
- **Audit log surfacing.** Phase 1 doesn't wire `audit_log` writes from the engine path; future security review may. Schema is ready.
