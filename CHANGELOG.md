# Changelog

## Unreleased — Drift Phase 1 (Drift Overview)

### Added

- `DriftStatusRepo.ListByOrg` returns every drift record for an org
  ordered by detected_at DESC. SQLite + Postgres impls + null stub.
- `GET /api/v1/drift` returns `{data: {summary: {total, drifted,
  clean}, records: [{deploymentTargetId, deploymentId,
  componentName, cloud, region, hasDrift, detectedAt}]}}`. Per-record
  target lookup (N+1; fine at v1 scale). Orphaned records skipped.
- `/ui/drift` page with status badges, summary counts, and per-row
  link back to the deployment. Empty-state directs to
  `nimbusfab drift` CLI or the deployment-detail "Detect drift"
  button.
- "Drift" top-nav link added to layout.html.
- 6 new tests: 2 repo (sqlite always, postgres gated), 3 API
  handler, 2 UI page, 2 router-mux integration.

### Out of scope (deferred)

- Background drift cron / scheduler — Drift Phase 2.
- Email / Slack notifications on drift detection.
- Drift-history time series (each Upsert overwrites; no
  per-target history table).

## Earlier — Dashboards Phase 1 (Per-Deployment Cost View)

### Added

- `engine.persistPlan` now also persists cost estimates: after
  writing the per-target plan-run rows, calls the estimator and
  `CostEstimates.BulkInsert`s one row per priced primitive with
  the right run ID. Per-target estimator failures are skipped (rest
  of the deployment's data still lands); repo-level failures log
  via `cfg.Logger` but don't fail the plan — cost-dashboard data is
  non-critical.
- `CostEstimateRepo.ListByDeployment(orgID, deploymentID)` —
  JOINs `cost_estimates → runs → deployment_targets` to return all
  estimates attached to any run of any target of the deployment.
  Both backends. nullRepo gets the matching stub returning
  ErrInventoryRequired.
- `GET /api/v1/deployments/{id}/costs` — JSON handler returning the
  per-target rollup + aggregate total. Shape:
  `{data:{deploymentId, currency, total, targets:[{deploymentTargetId,
  componentName, cloud, region, total, primitives:[...]}]}}`. 404 on
  missing deployment; empty targets array + total:0 when no cost rows.
- Deployment-detail UI page gains a "Cost estimate" section between
  Actions and Live events: per-target table with subtotal and tfoot
  Total. Empty-state copy explains the two no-cost cases (network-only
  components have no priced primitives; pre-Dashboards-Phase-1
  deployments populate after the next plan).
- 8 new tests: ListByDeployment per backend; engine.Plan persistence
  end-to-end; API handler happy/empty/404; UI section with-data and
  empty-state; router-mux integration.

### Out of scope (deferred)

- Cross-deployment / org-wide aggregate cost dashboard — Dashboards
  Phase 2.
- Parity overview page — separate phase.
- Drift overview — Drift Phase 1.
- Cost actuals (estimated-vs-actual) — Cost Collector phase.
- Cost-over-time charts — Dashboards Phase 2 or Polish.

## Earlier — Inventory Persistence (Cost + Audit)

### Added

- SQLite migration `0001_init.sqlite.sql` reaches parity with the
  Postgres migration: adds `api_tokens`, `run_logs`, `cost_estimates`,
  `cost_actuals`, `secrets_refs`, `audit_log` tables plus the
  matching indices (`idx_cost_actuals_period`, `idx_audit_log_ts`).
  Type mapping: UUID→TEXT, JSONB→TEXT, NUMERIC→REAL,
  BIGSERIAL→INTEGER AUTOINCREMENT, BYTEA→BLOB.
- `CostEstimateRepo` wired for both backends:
  - `BulkInsert(items)` writes all rows in one transaction with a
    prepared statement; empty input is a defensive no-op; fresh
    `cest-` UUID per row.
  - `ListByRun(orgID, runID)` returns rows in insertion order, scoped.
- `AuditLogRepo` wired for both backends:
  - `Append(entry)` auto-defaults `Timestamp` to `time.Now().UTC()`
    when caller supplies the zero value; auto-increment id assigned
    by the DB; empty `actor_user_id` / `target` / `payload_json`
    stored as NULL (Postgres UUID column rejects '').
  - `Query(orgID, since, until, limit)` orders newest-first
    `(timestamp DESC, id DESC)` for deterministic pagination;
    `limit ≤ 0` defaults to 100.
- 6 new tests across both backends: round-trips, time-window
  narrowing, limit capping, wrong-org isolation, empty-input
  no-op, timestamp default-to-now. SQLite tests run on every
  invocation; Postgres tests gated on `NIMBUSFAB_TEST_PG_DSN`.
- `TestRunMigrations_FreshDB` extended to assert every table both
  backends expose actually exists post-migration — fresh-migrate
  regression check.

### Out of scope (deferred)

- Consumer wiring — `engine.Plan` calling `CostEstimates.BulkInsert`
  with estimator output lands with Dashboards Phase 1 (which surfaces
  the data). Audit-log writes from the web app's mutating-endpoint
  middleware land with Auth Phase 1.
- `RunLogs`, `CostActuals`, `SecretsRefs`, `ApiTokens` repos remain
  `ErrNotImplementedYet` stubs in both backends. The SQLite migration
  now has the tables, so wiring them is purely repo-layer work when
  their owning consumers ship.

## Earlier — Inventory Phase 2 (Postgres Backend)

### Added

- `internal/inventory/postgres` package — second `inventory.Repo`
  implementation against Postgres via `github.com/jackc/pgx/v5/stdlib`.
  Mirrors `internal/inventory/sqlite` one-for-one: one file per real
  table (orgs / projects / stacks / components / deployments /
  targets / runs / drift) plus the same notwired.go stubs for
  not-yet-wired repos.
- `pkg/inventory.Open(ctx, dsn)` dispatcher routes by DSN scheme:
  `sqlite:` → sqlite, `postgres:` / `postgresql:` → postgres. Both
  backend packages register via init() so the dispatcher avoids
  importing them directly (no import cycle).
- `cmd/server` switches from `sqlite.Open` to `inventory.Open` — one
  import line per backend; adding a new backend (e.g. mysql) is one
  import line at the top of main.go.
- 3 dispatcher unit tests + 3 Postgres integration tests (gated on
  `NIMBUSFAB_TEST_PG_DSN`; skip cleanly when unset so CI without
  Postgres passes). Local Postgres: `docker run --rm -d
  -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16` then
  `NIMBUSFAB_TEST_PG_DSN='postgres://postgres:test@localhost:5432/postgres?sslmode=disable' go test ./...`.

### Design notes

- **Query-syntax delta vs SQLite**: $N placeholders, TIMESTAMPTZ
  scans directly into `time.Time` (no `mustParseTime` helper), JSONB
  reads via `COALESCE(col::text, '')` and writes via `$N::jsonb` cast,
  `ON CONFLICT (cols) DO UPDATE SET col = EXCLUDED.col` syntax,
  `now()` instead of `strftime()`.
- **`jsonOrEmpty(b []byte)`** helper turns nil / empty into `"{}"`
  so Postgres JSONB never receives an empty string (which fails
  to parse).
- **CLI stays SQLite-only** — it's a single-instance dev tool; only
  `cmd/server` needs the multi-backend dispatcher.
- **No new tables yet.** sessions / pats / idempotency_keys (called
  for by the web app spec) land with Auth Phase 1 and HTTP Phase 2
  polish — additive to the Repo interface; easier to introduce when
  the consumers ship.

### Out of scope (deferred)

- Wiring `CostEstimates` / `RunLogs` / `CostActuals` / `AuditLog`
  for either backend (both still return ErrNotImplementedYet).
- Connection pool tuning / prepared-statement caching / per-query
  timeouts — Polish Phase 1.
- Schema migrations beyond `0001_init.sql` — the existing migration
  runs as-is.

## Earlier — Web App UI Phase 2 (Buttons + Live Updates)

### Added

- `internal/webapi/ui/assets/app.js` — vanilla JS (~100 LOC, no
  framework, no build step). `window.nimbusfab.attachDeploymentActions`
  wires the deployment-detail page:
  - Hijacks the 3 buttons (Deploy / Destroy / Drift); confirm() prompt
    on destructive Destroy
  - POSTs to `/api/v1/deployments/{id}/{applies,destroys,drifts}`
  - Opens `EventSource` on `/api/v1/deployments/{id}/events`
  - Listens for the provisioner's RunEvent kinds (start/log/progress/
    success/failure/diagnostic/skip/terminal) plus the handler-emitted
    "complete" event
  - Renders each event as one `<div class="log-line">` with timestamped
    ts/target/kind/msg spans; auto-scrolls
  - Disables buttons during operation; re-enables and full-page-reloads
    after "complete" so target statuses re-read from inventory
  - Reads Bearer token from `<script data-api-token=...>` attribute
    when set (server-rendered when `NIMBUSFAB_API_TOKEN` is configured)
  - `escapeHtml` on all user-data before innerHTML to avoid injection
- CSS additions (~50 lines) for `.actions` button bar + `.log-pane`
  scrollable monospace log with per-kind line styling.
- `ui.Renderer` gains `APIToken` field; `NewRenderer` signature now
  `(repo, orgID, apiToken)`. Plumbed through `webapi.Config.APIToken`.
- `deployment_detail.html` gains the action bar + `event-log` div +
  two `<script>` tags (asset + inline init).
- 4 new tests: app.js embedded with expected exports; style.css has
  the new classes; deployment-detail page renders the buttons +
  script tag; API token wires into the script tag (with vs without).
- 2 router-level smoke tests for the script tag rendering + app.js
  serving from `/assets/app.js` with `javascript` Content-Type.

### Design notes

- **JS-required for interactivity.** Pages still render readable HTML
  without JS (UI Phase 1 read-only views work); buttons just do
  nothing without JS. No plain-form-POST fallback — would require
  server-side redirect after action, which adds surface area for
  marginal gain.
- **APIToken in script tag (not cookie).** Phase 2 stub. Auth Phase 1
  will replace with cookie sessions that browsers send automatically;
  the JS won't need a baked-in token then.
- **Full-page reload after complete.** Simpler than diffing the
  Targets table in JS. The page is small; the reload is imperceptible
  in practice.

### Out of scope (deferred)

- Plan trigger (CLI plans; web app applies/destroys/drifts).
- Auth Phase 1 (OIDC + cookies + real PATs).
- Confirmation modals (browser `confirm()` is sufficient).
- Log filtering / search / persistence (Polish Phase 1).
- Reconnect after SSE disconnect (needs RunLogs replay).

## Earlier — Web App HTTP Phase 2 (Mutating Endpoints + SSE)

### Added

- 3 mutating endpoints under `/api/v1/deployments/{id}/`:
  - `POST .../applies`  → 202 + JSON envelope; engine.Apply runs async
  - `POST .../destroys` → engine.Destroy async
  - `POST .../drifts`   → engine.DetectDrift async
  Each kicks off the engine call in a goroutine with a fresh
  context.Background (request context would cancel when 202 returns,
  killing the operation mid-flight). Response includes `deploymentId`,
  `operation`, `status`, and an `eventsUrl` pointer.
- 1 SSE endpoint `GET /api/v1/deployments/{id}/events` streams
  `RunEvent`s for the deployment. Initial `: connected` hello;
  per-event `id`/`event`/`data` lines with JSON payload; `: ping`
  heartbeat every 15s; `event: complete` on operation finish.
  Subscribers see events posted AFTER they connect (no replay; replay
  needs the stubbed `RunLogs` repo).
- `internal/webapi/runner` package with `RunBroker` — in-process
  pub/sub keyed by deployment ID. One broker per nimbusfab-server
  process; subscribers come and go as SSE clients connect/disconnect.
  Non-blocking dispatch with drop-on-full-subscriber policy.
- `engine.ApplyOpts` / `engine.DestroyOpts` gain `EventSink`. New
  `engine.DriftOpts`. Plumbed through to existing
  `provisioner.ApplyInput.EventSink` / `DestroyInput` / `DriftInput`
  fields. `engine.DetectDrift` signature changes (now takes opts).
  CLI updated to pass `engine.DriftOpts{}`.
- `webapi.Config.Engine` field; mutating + SSE routes mount only when
  configured. cmd/server gains `defaultCloudRegistry()` (mirrors CLI's)
  and constructs the full engine wiring (SQLite repo + DefaultBackend
  secrets + ExecRunner tofu + WorkRoot from `NIMBUSFAB_WORK_ROOT`).
- 14 new unit/integration tests (6 broker + 4 mutation handlers + 4
  SSE handler). End-to-end smoke-tested with a running binary: POST
  returns 202, concurrent SSE subscriber receives connected→complete.

### Design notes

- **Background context for engine goroutine.** The HTTP request
  context cancels when the response is written; the apply runs
  longer. Using `context.Background()` keeps the operation alive.
  Future: thread server-shutdown context so graceful shutdown waits
  for in-flight applies.
- **Deployment-level events (not per-run).** One deployment fans out
  to multiple targets, each with its own run; the spec's per-run SSE
  URL would have required splitting events. Keeping it per-deployment
  matches how the engine already fan-ins events to one channel; SSE
  subscribers see all targets' events interleaved with their
  `deploymentTargetId` field.
- **No replay.** The `RunLogs` inventory repo is stubbed; reconnect
  via `Last-Event-ID` lands when persistence does.

### Out of scope (deferred)

- POST `/api/v1/projects/{id}/plans` — Plan endpoint (CLI plans; web
  app applies/destroys/drifts).
- Idempotency-Key middleware (Auth Phase 1 or later).
- `?wait=true` sync mode.
- Reconnect via `Last-Event-ID` (needs RunLogs repo).
- UI Phase 2 (browser buttons + JS) — separate phase.

## Earlier — Web App HTTP Phase 1 (Read-Only JSON Endpoints)

### Added

- `internal/webapi/api` package — JSON GET handlers for the same
  data UI Phase 1 displays as HTML:
  - `GET /api/v1/projects` → `{"data": {"projects": [...]}}`
  - `GET /api/v1/projects/{id}` → project + stacks + components + recent deployments
  - `GET /api/v1/deployments/{id}` → deployment + targets
  - `GET /api/v1/runs/{id}` → run
- JSON envelope conventions: success → `{"data": ...}`; error →
  `{"error": {"code": "...", "message": "..."}}` matching the web
  app spec. Time fields RFC3339. Inventory Go types converted to
  camelCase via per-type `*JSON` helpers (decouples wire shape from
  Go struct names).
- `internal/webapi/middleware/auth.go` — `BearerToken(token)`
  middleware. Empty token → no-op (dev mode); non-empty → requires
  exact match on `Authorization: Bearer <token>`. Wrong/missing
  headers return 401 with the JSON error envelope.
- `webapi.Config.APIToken` field; `cmd/server` reads
  `NIMBUSFAB_API_TOKEN` env var and logs a clear startup note
  about whether API auth is required.
- 18 new unit/integration tests (4 middleware + 9 API handler + 5
  router). End-to-end smoke-tested with both auth on and off.

### Design notes

- **Per-handler auth wrapping** (not subtree mount). Go's ServeMux
  rejects ambiguous overlaps between path-only and method-specific
  patterns (`/api/v1/` vs `GET /`); registering each API route
  individually with the middleware bypasses the conflict and keeps
  the auth surface exactly scoped to /api/v1/*.
- **UI deliberately ignores APIToken.** UI routes remain
  unauthenticated in Phase 1 even when API auth is configured.
  Auth Phase 1 will add cookie sessions for the UI; until then,
  do not expose the binary publicly.
- **Phase-1 PAT stub.** Single shared token compared via string
  match. Real PATs (per-user, argon2 hashed, expirable) land in
  Auth Phase 1; the middleware shape is forward-compatible.

### Out of scope (deferred)

- Mutating endpoints — POST applies / destroys / drifts (HTTP Phase 2).
- SSE on `/api/v1/runs/{id}/events` (HTTP Phase 2).
- Real PAT data model (Auth Phase 1).
- Idempotency keys (HTTP Phase 2).
- Pagination / filtering (Polish Phase 1).
- Cost / parity / drift endpoints (Dashboards Phase 1, Drift Phase 1).

## Earlier — Web App UI Phase 1 (Read-Only Pages)

### Added

- `internal/webapi/ui` package — `html/template` page renderer with
  `embed.FS`-backed templates and CSS. Each page is parsed into its
  own `*template.Template` instance (avoids `{{define "content"}}`
  collisions across pages in Go's global per-Template namespace).
- 4 read-only pages: `/ui/projects` (table of registered projects),
  `/ui/projects/{id}` (stacks + components + recent deployments),
  `/ui/deployments/{id}` (per-target rows with status badges + run
  links), `/ui/runs/{id}` (kv block: kind, status, exit code,
  timestamps; placeholder for UI Phase 2's live log stream).
- `internal/webapi/router.go` mounts UI routes plus `/assets/*`
  (http.FileServerFS), `/healthz`, `/readyz`, and `/` → `/ui/projects`
  redirect. Std-lib `http.ServeMux` with Go 1.22+ pattern routing — no
  chi.
- `cmd/server/main.go` replaces the hello-world stub: parses
  `NIMBUSFAB_LISTEN_ADDR`, `NIMBUSFAB_DB_DSN`, `NIMBUSFAB_ORG_ID`,
  opens SQLite, runs migrations, mounts the handler.
- 1.5KB hand-authored CSS: system fonts, simple table, status badges
  (ok/fail/warn), kv grid for detail pages. No web fonts, no
  Tailwind, no SPA framework.
- 12 unit tests across `pages_test.go` + `router_test.go`. Smoke-
  tested end-to-end against a real SQLite file: `/healthz` → "ok",
  `/readyz` → "ready", `/ui/projects` → empty-state HTML.

### Design notes

- **Disabled auth.** UI Phase 1 ships with `OrgID: "default"` baked
  in; OIDC + cookie sessions land in Auth Phase 1. Production
  deployments should not expose this binary publicly until then.
- **SQLite only.** Postgres branch lands with Inventory Phase 2.
- **No live updates / no mutations.** SSE log streaming and
  Apply/Destroy/Drift buttons land in HTTP Phase 2 / UI Phase 2.
- **Pages render straight from `inventory.Repo`** — no HTTP API
  round-trip. The REST API is for programmatic clients (CLI talking
  to web-api, scripts using PATs) — server-rendered HTML reads
  directly.

### Out of scope (deferred)

- Mutating endpoints (HTTP Phase 2).
- SSE live log streaming (UI Phase 2).
- OIDC / cookie sessions / PATs (Auth Phase 1).
- Cost dashboard / parity overview / drift overview (Dashboards
  Phase 1, Drift Phase 1).
- Audit log writes (no mutations yet).
- Pagination (load all rows; v1 volume is small).

## Earlier — Secrets Phase 1 (Env + File Backends)

### Added

- `pkg/secrets/env.go` — `EnvBackend` reads
  `NIMBUSFAB_SECRET_<UPPER_REF>` env vars (uppercase + hyphen→underscore).
  Value must be a JSON object. Missing env var returns (nil, nil) so
  the backend can be chained.
- `pkg/secrets/file.go` — `FileBackend` reads `<Dir>/<ref>.json`;
  default `~/.nimbusfab/secrets/`. Missing files return (nil, nil).
- `pkg/secrets/default.go` — `DefaultBackend()` returns
  `Chain(EnvBackend, FileBackend)`. Env-first means dev workflows
  that export `NIMBUSFAB_SECRET_*` take precedence over committed
  files.
- `pkg/provisioner/secrets.go` — `resolveEnvFor` helper translates a
  `credentialRef` + `SecretsBackend` into a `map[string]string` env
  var map. Nil backend or empty ref → empty map (preserves
  pre-Phase-1 behavior of relying on process env). Non-resolvable
  refs → `ErrSecretsRefUnresolved`, failing the target fast before
  any tofu invocation.
- `provisioner.Config.SecretsBackend` field; wired into
  `apply.go`, `destroy.go`, `drift.go` at every `tofu.Workspace`
  construction site (4 callsites total).
- `TargetPlan.CredentialRef` so apply/destroy/drift can resolve
  per-target credentials without re-walking the project.
- `cmd/cli/secrets.go` — `defaultSecretsBackend()` returns
  `secrets.DefaultBackend()`. All 6 CLI command files wire it via
  `engine.Config.SecretsBackend`.
- End-to-end test asserts the resolved env var arrives in
  `runner.ApplyCalls[0].Workspace.Environment`.

### Design notes

- **Payload-is-envvars.** The backend's resolved map keys ARE the
  env var names the cloud provider expects
  (`AWS_ACCESS_KEY_ID`, `ARM_CLIENT_SECRET`,
  `GOOGLE_APPLICATION_CREDENTIALS`, etc.). Keeps engine code
  cloud-agnostic at the cost of pushing env-var-naming knowledge to
  whoever manages the secret material.
- **Operation-time resolution.** Apply / destroy / drift resolve
  credentials immediately before invoking the runner. Plan does NOT
  resolve secrets (out of scope per spec; would block plans in dev
  environments without configured creds).
- **No process-env mutation.** Per-command `cmd.Env` only; concurrent
  target operations stay isolated.

### Out of scope (deferred)

- Vault / cloud-KMS / OIDC / Workload Identity Federation backends.
- Adapter-side env-var translation (cloud-neutral payload shape).
- Secret payload schema validation per cloud.
- Credential rotation / expiry detection.
- Encryption at rest for the file backend (dev-only as documented).
- Audit-log persistence (Inventory Phase 2 owns the AuditLog repo;
  resolutions currently log to the engine's logger only).

## Earlier — Validator Phase 5 (Cross-Component Refs)

### Added

- `internal/dsl/validator/phase5_refs.go` — new pipeline phase that
  validates the cross-component reference graph. Per-ref checks:
  self-reference (`ref.Component == comp.Name`); component existence
  (referenced name is in the project); output existence
  (`ref.Output` is in target `Type.Outputs()`). After the per-ref
  pass, DFS with three-color marking detects cycles in the directed
  ref graph.
- Four new issue codes: `ErrValidatorRefSelf`, `ErrValidatorRefUnknownComponent`,
  `ErrValidatorRefUnknownOutput`, `ErrValidatorRefCycle`. All
  `SeverityError` — every case would fail at provision time anyway.
- Cycle reports include the full path joined by ' → ' (e.g.
  `web-app → orders-db → web-app`) with the issue's Path pointing at
  `components[N].refs` where N is the cycle's first node.
- Suppression rule: if a ref target has an unknown type (Phase 4
  already flagged it), Phase 5 skips the output check rather than
  emit noise. The user fixes the type, re-runs, then any remaining
  ref-output errors surface.
- 10 unit tests in `phase5_refs_test.go` + 4 end-to-end CLI tests in
  `validate_test.go`.

### Performance

O(N + E) where N = components and E = refs. Negligible for realistic
projects.

## Earlier — Validator Phase 4 (Per-Type Spec Schema)

### Added

- `internal/dsl/validator/phase4_typespec.go` — new pipeline phase that
  validates each component's `spec` against the JSON Schema declared
  by its `components.Type.SpecSchema()`. Schemas already shipped in
  `pkg/components/schema/v1alpha1/{network,compute,database,storage}.json`
  but were not previously applied; Phase 4 wires them in.
- Two new issue codes: `ErrValidatorUnknownType` (type name not in
  registry — typo in `type:` field) and `ErrValidatorTypeSpec` (spec
  failed schema validation with field path, e.g.
  `components[2].spec.cidr`).
- Schema-compilation cache scoped to one `Validate()` invocation so
  N components of the same type recompile only once.
- 9 unit tests in `phase4_typespec_test.go` + 3 end-to-end CLI tests
  in `validate_test.go`.

### Changed

- `validator.New()` signature → `validator.New(registry components.Registry)`.
  Production callers in all 8 CLI command files pass
  `components.DefaultRegistry()`. The registry will be the hook for
  user-defined types (plugin loading) in a future phase.
- `internal/dsl/loader/testdata/multi-file/components/web-network.yaml` —
  Phase 4 surfaced a real pre-existing typo (`cidrBlock` instead of
  `cidr`) that prior validation had silently accepted. Fixed the
  fixture to use the schema-required field name.

### Out of scope (deferred)

- Cross-component ref validation (does the referenced component exist;
  does the named output match `Type.Outputs()`?). Future Phase 5 of
  the validator.
- Plugin-loaded user-defined types. The Registry-based design is the
  hook; the loader is later.
- Per-cloud `Type.SupportedClouds()` check — latent until v2 types
  with cloud restrictions.
- Spec interpolation (`${var.foo}` substitution) before validation.
  Future phase.

## Earlier — GCP Adapter Phase 5

### Added

- `internal/cloud/gcp` — full `cloud.Adapter` implementation mirroring
  AWS Phase 3 and Azure Phase 4 structure: per-type emit files
  (network / compute / database / storage), dispatch on
  `target.Spec["__type"]`, `PricingKey()` + `Profile()` real
  implementations, `DefaultStateBackend()` (gcs backend),
  `ProviderBlock()` (google provider with region + optional project).
- Per-type emissions:
  - network = VPC (custom-subnetwork mode) + N regional Subnetworks
    + two Firewalls (allow-internal, deny-external)
  - compute = egress Firewall + N Compute Engine instances distributed
    across zones a/b/c; default image Ubuntu 22.04 LTS
    (ubuntu-os-cloud project)
  - database = Cloud SQL instance (PG/MySQL) + default database;
    MariaDB rejected with explicit error (Cloud SQL doesn't offer it)
  - storage = single GCS bucket (no container sub-resource)
- T-shirt size mappings — compute: e2-small / e2-medium / e2-standard-2
  / n2-standard-4 (E2 burstable + N2 general-purpose families);
  database: db-f1-micro / db-g1-small / db-custom-2-7680 /
  db-custom-4-15360.
- GCP pricing snapshot (`pkg/cost/pricing/snapshot/gcp.json`) covering
  the Phase-5 Compute Engine / Cloud SQL / Cloud Storage SKUs across
  `{us-central1, us-east1, europe-west1}`.
- `pkg/cost/estimator.UnitsFor` extended to recognize GCP Tofu types
  (google_compute_instance, google_sql_database_instance,
  google_storage_bucket).
- `pkg/plugin/contract.RunProvisionerScenarios` passes for GCP adapter.
- `cmd/cli/clouds.go` — `defaultCloudRegistry()` registers GCP
  alongside AWS + Azure (one-line extension; the helper's centralization
  paid off).
- Full-stack fixture (`cmd/cli/testdata/full-stack-project/`) now
  targets all three clouds for every component: 4 components × 3 clouds
  = 12 deployment targets. `nimbusfab parity` reports 3-way weighted
  scores (Azure outlier patterns surface clearly); `nimbusfab cost
  estimate` shows three per-cloud subtotals (AWS / Azure / GCP).
- Region naming: GCP adapter validates against
  `^[a-z]+-[a-z]+[0-9]$` regex, rejecting AWS (`us-east-1`) and Azure
  (`eastus`) formats.
- Bucket naming: GCS buckets share a global namespace; the adapter
  derives `<project>-<component>-<region>-<sha6>` with a deterministic
  hash suffix to reduce collision risk.

### Out of scope (deferred)

- `google-beta` provider resources (Confidential VMs, GKE Autopilot
  features, etc.). v2.
- Service Account / IAM role management (provider-level auth only).
- BigQuery, Spanner, Firestore, Bigtable, GKE, Cloud Run, App Engine.
- VPC peering, Cloud Interconnect, Cloud VPN, Cloud Load Balancing.
- Committed / Sustained Use Discounts, Spot VMs.
- Cloud KMS, Secret Manager (web app + secrets phases).
- Tier-1 `<cloud>: gcp:` escape hatch schemas.

## Earlier — Azure Adapter Phase 4

### Added

- `internal/cloud/azure` — full `cloud.Adapter` implementation mirroring
  AWS Phase 3's structure: per-type emit files (network / compute /
  database / storage), dispatch on `target.Spec["__type"]`,
  `PricingKey()` + `Profile()` real implementations,
  `DefaultStateBackend()` (azurerm backend), `ProviderBlock()`
  (azurerm provider with mandatory features block).
- Per-type emissions:
  - network = ResourceGroup + VirtualNetwork + NSG + N subnets
  - compute = RG + NSG + N (Public IP + NIC + Linux VM); default image
    Ubuntu 22.04 LTS (publisher=Canonical)
  - database = RG + PostgreSQL/MySQL Flexible Server (+ default database)
    OR classic MariaDB server (Azure deprecated MariaDB Flexible)
  - storage = RG + StorageV2 account (LRS replication) + Container
- T-shirt size mappings — compute: Standard_B2s/B2ms/B4ms/D4s_v5;
  database: Standard_B1ms/B2s/D2s_v3/D4s_v3 (Burstable / GeneralPurpose
  tiers).
- Azure pricing snapshot (`pkg/cost/pricing/snapshot/azure.json`) covering
  the Phase-4 VM / Flexible Server / Storage SKUs across
  `{eastus, eastus2, westeurope}`.
- `pkg/cost/estimator.UnitsFor` extended to recognize Azure Tofu types
  (linux_virtual_machine, postgresql/mysql/mariadb servers, storage account).
- `pkg/plugin/contract.RunProvisionerScenarios` passes for Azure adapter.
- `cmd/cli/clouds.go` — `defaultCloudRegistry()` helper registers both
  AWS and Azure for all CLI commands (refactored 6 production files).
- Full-stack fixture (`cmd/cli/testdata/full-stack-project/`) now targets
  both AWS and Azure for every component: 4 components × 2 clouds = 8
  targets. `nimbusfab parity` reports non-trivial scores; `nimbusfab
  cost estimate` shows per-cloud subtotals.
- Region naming: Azure adapter rejects AWS-style names
  (`us-east-1`); use Azure location format (`eastus`, `westeurope`, etc.).

### Out of scope (deferred)

- AzAPI provider for resources not yet covered by AzureRM. v2.
- Managed identities / RBAC role assignments. v2.
- Azure SQL Database / Cosmos DB / Synapse. Future per-service specs.
- Application Gateway / Front Door / Traffic Manager. v2.
- VM Scale Sets (auto-scaling case). v2.
- Storage lifecycle / immutability policies. v2.
- Spot VMs / Reserved Instances / Hybrid Benefit. v2.
- Tier-1 `<cloud>: azure:` escape hatch schemas.
- LocalStack / Azurite integration testing. Credentials-gated CI phase.

## Cost Estimator Phase 1

### Added

- `pkg/cost/pricing.NewCache` — bundled-snapshot pricing cache backed
  by embedded JSON files (`pkg/cost/pricing/snapshot/*.json`).
- AWS price snapshot covering Phase-3 EC2 (t3.* + m6i.* across 3 regions),
  RDS (db.t3.* + db.m6i.* × postgres/mysql/mariadb × Single-AZ/Multi-AZ),
  and S3 Standard. Curated from AWS public pricing pages; refresh process
  documented in `pkg/cost/pricing/snapshot/README.md`.
- `pkg/cost/pricing.CanonicalKey` — deterministic flattening of
  `Adapter.PricingKey()` maps to cache-friendly strings.
- `pkg/cost/pricing.AsPricingProvider` — adapter from `Cache` to
  estimator's `PricingProvider` interface.
- `pkg/cost/pricing.SnapshotAge` — staleness helper for "snapshot is N
  days old" CLI warnings.
- `pkg/cost/pricing.Cache.Refresh` returns `ErrNotImplementedYet`; live
  AWS Pricing API integration is Cost Phase 2.
- `pkg/cost/estimator.New` — runtime Estimator: walks plan targets,
  calls `adapter.PricingKey`, queries pricing provider, multiplies by
  per-primitive usage assumptions, aggregates per-target / overall.
- `pkg/cost/estimator.UnitsFor` — usage assumptions table: 730 hr/month
  for compute / db; 100 GB-Mo default for storage. User overrides via
  `spec.usage.hoursPerMonth` and `spec.usage.storageGB`.
- `engine.EstimateCost(plan)` wires the cost path through the registry.
- `pkg/provisioner.TargetPlan.RawPrimitives` — keeps the adapter's emit
  output verbatim so the cost path can call `PricingKey` without
  re-emitting.
- `nimbusfab plan` now prints a Cost summary alongside the Parity
  summary (and also fixes the Parity-summary gap from the prior phase's
  commit message that wasn't actually applied).
- `nimbusfab cost estimate --stack <stack>` — detailed per-primitive
  monthly breakdown with target subtotals + warnings.

### Out of scope (deferred)

- Live AWS Pricing API integration. Cost Phase 2.
- Azure / GCP pricing snapshots. Ship with those adapters.
- Cost actuals collection from billing APIs. Separate Cost Collector spec.
- Inventory writes of estimates to `cost_estimates` table. Inventory Phase 2.
- Reserved instances, savings plans, spot pricing. v2.
- Data transfer / egress costs. v2.
- Multi-currency. v2.
- Cost optimization recommendations. v2.

## Parity Engine Phase 1

### Added

- `pkg/parity.NewEngine` — public parity surface: `Compare()` builds
  per-component reports; `EvaluateRules()` applies parity.yaml policies.
- Embedded contract-floor catalog (`pkg/parity/contracts/*.yaml`) for
  the four v1 types (database / compute / storage / network).
- Score function: per-attribute numeric / exact / boolean comparisons
  with weighted mean and feature-group averaging. Weights documented
  in `pkg/parity/score.go`; not user-tunable in v1.
- Rule evaluator: per-component `minScore` + per-attribute `exact` /
  `maxRatio` / `requireAll` policies; per-component `warn` / `block` /
  `off` modes.
- `parity.LoadRulesFromFile` for `parity.yaml`; missing file = the
  parity-default "informative-only" mode.
- `parity.RenderText` + `RenderViolations` terminal reporters.
- Provisioner integration: every `Plan()` collects `Profile()` per
  primitive into `TargetPlan.PrimitiveProfiles` and aggregates
  `ParityReport`s per component into `PlanResult.ParityReports`.
- CLI: `nimbusfab plan` prints a per-component parity summary; new
  `nimbusfab parity --stack <stack>` command surfaces detailed reports
  with optional `--component <name>` filter.

### Out of scope (deferred)

- REST API endpoints — web app phase.
- Inventory persistence of parity reports — inventory Phase 2 / web app.
- Auto-balancing (adapter actively upgrading SKUs to maximize parity) — v2.
- Per-attribute weight tuning by users — v2.
- Cross-region equivalence mapping — v2.
- Historical parity tracking — v2.

## AWS Adapter Expansion Phase 3

### Added

- Four concrete `components.Type` implementations: `network`, `compute`,
  `database`, `storage`. Each ships an embedded JSON Schema (under
  `pkg/components/schema/v1alpha1/`) and declares its output names + types.
- `components.DefaultRegistry()` returns all four registered;
  `engine.New` defaults `Config.ComponentTypes` to it.
- `Type.Outputs()` added to the `components.Type` interface, plus
  `components.OutputType` struct.
- AWS adapter dispatches `Emit()` on `target.Spec["__type"]` (a new
  field the provisioner stuffs alongside `__component`).
- `internal/cloud/aws/network.go` — VPC + IGW + RT + N subnets +
  RT associations with deterministic /24 slicing and per-region AZ trios.
- `internal/cloud/aws/compute.go` — security group + N EC2 instances
  with T-shirt size → instance type resolution and per-region default
  Amazon Linux 2023 AMIs.
- `internal/cloud/aws/database.go` — DB subnet group + RDS instance
  with T-shirt size → instance class + storage resolution for
  postgres / mysql / mariadb.
- `internal/cloud/aws/storage.go` — S3 bucket + versioning +
  public-access-block + server-side encryption with secure defaults
  and deterministic bucket-name derivation.
- `internal/cloud/aws/pricing.go` — `PricingKey()` real implementation
  with structured maps for AmazonEC2, AmazonRDS, AmazonS3 (free
  primitives return `nil, nil`).
- `internal/cloud/aws/profile.go` — `Profile()` real implementation
  populating `parity.ResourceProfile` per resource class.
- AWS adapter `SupportedComponentTypes()` returns all four type names.
- Full-stack CLI fixture under `cmd/cli/testdata/full-stack-project/`
  exercising all four types in one project.

### Out of scope (deferred)

- Validator Phase 4 (per-type `SpecSchema` validation in the validator
  pipeline). Type schemas ship; wiring is its own phase.
- Cost estimator / parity engine consumption of the new data.
- Tier-1 `<cloud>:` escape-hatch schemas for AWS per-type fields.
- Tier-2 `raw:` block merging.
- Azure / GCP adapters.
- LocalStack integration tests.
- Auto-scaling groups, NAT gateways, RDS read replicas, S3 lifecycle.

## Inventory Persistence Phase 1

### Added

- SQLite inventory backend (`internal/inventory/sqlite`) built on
  modernc.org/sqlite (CGo-free). Implements Org / Project / Stack /
  Component / Deployment / DeploymentTarget / Run / DriftStatus repos;
  the remaining sub-repos return `ErrNotImplementedYet` until their
  owning phases.
- Embedded migration runner (`pkg/inventory/migrations.go`) that picks
  flavor-specific SQL files via `//go:embed` and tracks applied versions
  in `schema_migrations`. Postgres flavor of the migration ships
  unchanged; SQLite flavor adapts types (UUID/JSONB/TIMESTAMPTZ → TEXT).
- `pkg/inventory.NewNullRepo()` for `--no-inventory` mode: writes no-op,
  reads return `inventory.ErrInventoryRequired`.
  `inventory.IsNullRepo()` lets callers branch on inventory presence.
- `nimbusfab plan` now returns a Deployment ID when an inventory is
  configured. Project / stack / components / deployment / per-target
  rows + a per-target `kind=plan` run are persisted in one go.
- `nimbusfab apply <deployment-id>` / `destroy <deployment-id>` /
  `drift <deployment-id>` reconstitute the plan from inventory rows,
  delegate to the provisioner, and update terminal status / drift_status
  / per-target apply (or destroy) run rows.
- CLI flags: `--inventory-dsn` (default `sqlite://~/.config/nimbusfab/inventory.db`)
  and `--no-inventory`.
- `plan_file` column added to `deployment_targets` so Apply-by-ID can
  locate the saved tofu plan binary.

### Out of scope (deferred)

- Postgres backend (future phase; same contract).
- Web auth / api_tokens / OIDC / users (web app phase).
- Run log persistence (server phase; `RunLogs` repo returns
  `ErrNotImplementedYet`).
- Cost write paths (cost specs; `CostEstimates` / `CostActuals` repos
  return `ErrNotImplementedYet`).
- `nimbusfab runs status` / `deployments list` CLI commands.
- Idempotent plan IDs derived from `(project, stack, plan-content-hash)`
  — Phase 1 always creates a fresh deployment.

## Provisioner Phase 2

### Added

- `nimbusfab apply --stack <stack>` — validates, plans, then applies with
  `--partial-failure {leave|rollback|retry-failed}` policy.
- `nimbusfab destroy --stack <stack>` — reverse-order tear-down.
- `nimbusfab drift --stack <stack>` — `tofu plan -refresh-only` per target.
- `pkg/provisioner` — `Apply`, `Destroy`, `DetectDrift` implementations
  feeding the new CLI surface.
- `pkg/provisioner/orchestrator.go` — component DAG topo sort with parallel
  target fan-out and three-semaphore caps (global / per-cloud / per-credential).
- Partial-failure policies: `leave` (default), `rollback` (destroys succeeded
  targets when any failed), `retry-failed` (re-runs failed targets up to
  `MaxRetries` times).
- `internal/state/bridge` — parses `tofu show -json` into a typed Snapshot
  with deterministic per-resource attribute hash; Apply embeds the snapshot
  in `TargetApplyResult`.
- `pkg/provisioner.RunEvent` — typed per-target event stream (consumed
  by CLI; web SSE wires in a later phase).
- Cross-component refs: `data.terraform_remote_state` block auto-injected
  into dependent workspaces when a component declares `refs:`.

### Changed

- `tofu.Runner.Plan` accepts `PlanOpts.RefreshOnly` for drift detection.
- `tofu.FakeRunner` gains a `DriftPlan` field that scripts the response to
  refresh-only plan calls.
- `pkg/engine` adds `ApplyWithPlan`, `DestroyWithPlan`,
  `DetectDriftWithPlan` to the `Engine` interface (Phase-2 surface; pass the
  PlanResult directly since inventory persistence is pending).
  `Engine.DetectDrift(deploymentID)` and `Engine.Apply(planID)` still return
  `ErrNotImplementedYet`.
- `pkg/engine` aliases `DriftReport`/`TargetDriftReport`/`DriftedResource`
  to the provisioner shapes; the Phase-0 placeholder types are removed.

## DSL/IR + Provisioner Phase 1 (merged 2026-05-15)

### Added — Provisioner Phase 1

- `nimbusfab plan --stack <stack>` for AWS `network` components: validates,
  materializes a per-target Tofu workspace, runs `tofu init && tofu plan`,
  prints the summary.
- `pkg/provisioner` — workspace materialization, framework-tag injection
  (`infra:component`, `infra:deployment_id`, `infra:org_id`), canonical JSON
  serialization (sorted keys, deterministic across runs).
- `pkg/cloud.Registry` — cloud adapter registry with `Register`/`Get`/`List`.
- `internal/cloud/aws` — minimal AWS adapter (`network` → `aws_vpc`).
- `internal/tofu` — subprocess `Runner` (`Init`, `Plan`, `Show`, `Output`,
  `Validate`, `Version`, `StateRm`, `StateMv`) plus `FakeRunner` for tests
  and structured Tofu diagnostic parsing.
- `pkg/parity` — `ResourceProfile` types referenced by the new
  `cloud.Adapter.Profile()` contract slot.
- `pkg/cloud.Adapter` extended with `TierOneSchema`, `Validate`, `Profile`,
  `ProviderBlock`, and `SupportedComponentTypes`.
- `pkg/plugin/contract.RunProvisionerScenarios` — adapter contract test
  suite covering name stability, schema validity, secrets safety, default
  state-backend kind, and Emit purity.

### Documentation

- New subsystem spec: `docs/superpowers/specs/2026-05-15-provisioner-design.md`
  (provisioner orchestration, full Adapter contract, workspace layout,
  state-bridge / drift detection, multi-target orchestration with
  partial-failure policies, run / inventory persistence).
- New phase plan: `docs/superpowers/plans/2026-05-15-provisioner-phase1-runner-and-network.md`.

### Out of scope (Phase 1 — defers to later phases)

- `Apply`, `Destroy`, state bridge, drift detection (Phase 2).
- Multi-target parallel orchestration with partial-failure policies (Phase 2).
- Cross-component refs and `data.terraform_remote_state` resolution (Phase 2).
- Inventory persistence (Phase 2).
- Additional AWS resources (subnets, route tables, database/compute/storage) — Phase 3.
- Azure / GCP adapters (Phases 4–5).
- `Profile` / `PricingKey` / `BillingQuery` / `FetchBilling` real
  implementations — return `ErrNotImplementedYet` in Phase 1.

## DSL/IR Phase 1 (merged 2026-05-15)

### Added

- `nimbusfab validate` CLI: loader + validator phases 1–3 (YAML
  well-formedness, APIVersion check, JSON Schema validation).
- `pkg/ir` IR types with provenance + validation report model.
- `tools/schemagen`: generates JSON Schemas from IR Go types.
- `internal/dsl/loader`: project / components / compositions / stack-values
  loader with file:line provenance via `internal/dsl/yamlnode`.
- `internal/dsl/validator`: 3-phase pipeline (YAML / APIVersion / JSON Schema).
