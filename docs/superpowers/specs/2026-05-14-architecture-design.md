# Architecture-Level Spec: Multi-Cloud IaC Framework (working name: `cloud-infra-manager`)

**Status:** Architecture-level spec. Defines module boundaries, the IR data model, and public interface contracts between subsystems. Each of the four major subsystems (DSL/IR, Provisioner, Cost Estimator, Cost Dashboard) will get its own detailed spec in follow-up cycles.

**Date:** 2026-05-14
**Target directory:** `/home/kurt/git/cloud-infra-manager/` (currently empty)

---

## Context

The user wants to build an open-source framework that lets a user declare an infrastructure component (network, database, compute, storage, etc.) in YAML, target one or more clouds (AWS / Azure / GCP) — including multi-cloud parity deployments — and have the framework generate and run OpenTofu under the hood. The framework must also provide pre-deploy cost estimation and an ongoing actual-cost dashboard pulling from cloud billing APIs.

The four named subsystems (DSL/IR, Provisioner, Cost Estimator, Cost Dashboard) are interlocking but independent enough to design and ship separately. Getting the contracts between them right matters more than any one subsystem's internals, and getting them wrong is expensive to undo. This spec exists to lock those contracts before the per-subsystem specs are written.

This is a brand-new project — no code exists yet, and the target directory is empty. The architecture decisions below are the outcome of a brainstorming session with the user.

---

## Locked-in decisions (do not relitigate in implementation)

1. **Motivation:** open-source product, broad external adoption.
2. **Frontends v1:** CLI + self-hosted web app. GitOps daemon planned later. Engine must be a Go library that all three consume.
3. **Language:** Go.
4. **State model:** inventory DB (Postgres for server, SQLite for solo CLI) layered over OpenTofu state. Standard Tofu backends (S3, GCS, Azure Blob, Postgres) for state itself.
5. **DSL:** YAML files validated against JSON Schema. Programmatic Go SDK exists for power users but YAML is primary.
6. **Plugin model:** all adapters in-tree for v1; interfaces designed to be gRPC-plugin-ready in v2 (HashiCorp go-plugin style). Plus YAML Compositions for user-defined high-level components.
7. **Multi-cloud parity:** both replication (v1, default) and cross-cloud composition (v2). IR supports both; v1 engine rejects composed mode with a clear error.
8. **Cost data:** cloud-native APIs (AWS Pricing API, Azure Retail Prices API, GCP Cloud Billing Catalog for estimation; Cost Explorer / Cost Management / Cloud Billing API for actuals, plus cost-export integrations for high volume). Bundled per-release pricing snapshot as fallback. No Infracost or third-party runtime deps.
9. **Tenancy:** single-tenant self-host default. Every persisted entity has `org_id` from day one so multi-tenant SaaS lands without schema migration. Auth: OIDC + local users.
10. **OpenTofu invocation:** subprocess `tofu`. No embedding of Tofu internals.
11. **Module generation:** emit OpenTofu JSON configuration syntax (not HCL).
12. **CLI inventory mode:** SQLite by default; `--no-inventory` mode supported (drift detection and cost actuals are disabled in that mode, clearly documented).
13. **Composition expansion timing:** at validation time, snapshot per deployment (Helm-chart semantics). Editing a Composition does not live-update existing deployments; users must re-plan.
14. **Credential scoping:** per-`DeploymentTarget` credential refs. One project can deploy into multiple AWS accounts / Azure subscriptions / GCP projects by giving targets different `credentialRef`s.
15. **Web API:** REST + Server-Sent Events under `/api/v1/...`. Cookie session for browser; PATs for programmatic.
16. **CLI apply default:** synchronous (streams the run inline); `--detach` returns a run ID for async use (matches what the future GitOps daemon needs).
17. **Tofu workspaces:** per-`(component, cloud, region)`. No shared state across targets.
18. **IR versioning:** `v1alpha1` pre-1.0, semver after.

---

## 1. System overview

```
+--------------------------------------------------------------+
|  Frontends                                                   |
|  +-------------+   +----------------+   +-----------------+  |
|  |  CLI (Go)   |   |  Web Backend   |   |  GitOps Daemon  |  |
|  |             |   |  (Go HTTP)     |   |  (future)       |  |
|  +------+------+   +-------+--------+   +--------+--------+  |
+---------|------------------|---------------------|-----------+
          |                  |                     |
          v                  v                     v
+--------------------------------------------------------------+
|  Engine Library (Go) -- pkg/engine                           |
|  +-------------+ +-------------+ +-----------+ +----------+  |
|  |  DSL / IR   | | Provisioner | |  Cost     | |  Cost    |  |
|  |  load+valid | | plan/apply  | |  Estimator| | Collector|  |
|  +-----+-------+ +------+------+ +-----+-----+ +----+-----+  |
|        \              |               |            /         |
|         \   +---------v---------+     |           /          |
|          \  | Inventory Store   |<----+----------+           |
|           \ | (PG / SQLite)     |                            |
|            \+-------------------+                            |
|             |                                                |
|  +----------v-----------+ +-----------------+ +----------+   |
|  | Cloud Adapters       | | Component       | | Secrets  |   |
|  | aws / azure / gcp    | | Registry +      | | Backends |   |
|  |                      | | Composition Eng | |          |   |
|  +----------+-----------+ +-----------------+ +----------+   |
+-------------|-------------------------------------------------+
              |
              v
+--------------------------------------------------------------+
|  External                                                    |
|  tofu CLI  |  Cloud APIs (pricing, billing)  |  Inventory DB |
|  state backends (S3/GCS/Blob/PG)             |  OIDC IDPs    |
+--------------------------------------------------------------+
```

Frontends never bypass the engine. Adapters never call other adapters. Subsystems communicate via the IR and the inventory store; they do not import each other.

---

## 2. Module boundaries

Go layout: `pkg/` for public surface, `internal/` for impl, `cmd/` for binaries.

| Module | Responsibility | Does NOT own |
|---|---|---|
| `pkg/engine` | Top-level `Engine` interface; wires subsystems together. | Domain logic, SQL, cloud SDKs. |
| `pkg/ir` | IR Go types, JSON Schema, schema-version constants, YAML <-> IR helpers. | Semantic validation, disk, clouds. |
| `internal/dsl/loader` | Reads YAML files; resolves includes/refs/vars; returns unvalidated IR. | Validation. |
| `internal/dsl/validator` | JSON Schema + semantic checks + Composition expansion. | Mutating IR shape beyond expansion. |
| `pkg/provisioner` | IR -> per-target plans; drives tofu-runner; reconciles to inventory. | HCL generation (delegates to adapters). |
| `internal/tofu` | Subprocess wrapper around `tofu` (init/plan/apply/destroy/show/state). | Cloud or component knowledge. |
| `internal/state/bridge` | Introspects Tofu state JSON; reconciles with inventory; drift detection. | Writing state directly. |
| `pkg/inventory` | Inventory DB schema, migrations, repository interfaces. PG + SQLite impls under `internal/inventory/{pg,sqlite}`. | IR semantics, cloud logic. |
| `pkg/cost/estimator` | IR -> pricing keys (via adapters) -> price lookups -> estimate tree. | Deciding what to deploy. |
| `pkg/cost/collector` | Polls cloud billing APIs; normalizes; writes `cost_actuals`. Worker in server mode, on-demand in CLI. | Pricing decisions. |
| `internal/pricing/cache` | Read-through cache over pricing APIs with bundled-snapshot fallback. | Cloud-specific keys (adapters supply those). |
| `pkg/cloud` + `internal/cloud/{aws,azure,gcp}` | Per-cloud `Adapter` impl: emit Tofu JSON, return pricing keys, return billing query params, return state-backend defaults. | Calling Tofu or DB. |
| `pkg/components` | Component registry: maps type names (`network`, `database`, ...) to adapter dispatch. | Cloud specifics. |
| `pkg/composition` | Loads user-defined Compositions; expands them into built-in components at validation time. | Cross-cloud composition execution (v2). |
| `internal/webapi` | HTTP server; thin engine wrappers; SSE for run progress. | Business logic. |
| `internal/webauth` | OIDC + local users; sessions; `org_id` resolution per request. | Anything outside auth. |
| `cmd/cli` | Cobra CLI; instantiates in-process engine; SQLite inventory (or none with `--no-inventory`). | Duplicate validation logic. |
| `pkg/plugin` | Designed-not-implemented v1: Go interfaces destined for gRPC + contract test suite. | Runtime plugin loading (v2). |
| `pkg/secrets` | Pluggable secrets backends (env, file, Vault stub). Names resolved at runtime; no secret values in DB. | Storing credentials. |

---

## 3. The IR (Intermediate Representation)

The IR is the contract between the YAML world and everything downstream. Plugin authors and the runtime schema consume the same Go types in `pkg/ir`.

**Top-level concepts:**

- **Project** — a directory of YAML files; one inventory scope.
- **Stack** — a named environment within a Project (`dev`, `prod`); parameterizes deployments.
- **Component** — the logical thing the user declared (e.g., `type: database`, `name: orders-db`). Has 1..N `DeploymentTargets`.
- **DeploymentTarget** — `(component, cloud, region, credentialRef)`. Replication mode produces N independent targets per component. Composed mode (v2) produces one logical cross-cloud target whose primitives span clouds.
- **ResourcePrimitive** — a single cloud resource produced by a cloud adapter from a DeploymentTarget. Maps 1:1 to a `resource` block in Tofu JSON.
- **Composition** — a user-defined component type expanded into built-in components/primitives at validation time.
- **Deployment** — inventory record of an `Engine.Apply` invocation; references its child Runs.

**Pseudocode shapes** (illustrative, not field-final):

```go
type Project struct {
    APIVersion string              // "infra.dev/v1alpha1"
    Name       string
    Stacks     map[string]Stack
    Components []Component
    Comps      []Composition       // user-defined types
}

type Component struct {
    Name    string
    Type    string                 // "database", "network", or a Composition kind
    Spec    map[string]any         // type-specific; JSON-Schema validated
    Targets []DeploymentTarget     // >=1; replication = one per cloud
    Mode    TargetMode             // "replicate" (v1) or "composed" (v2)
    Refs    []ComponentRef         // dependencies on other components
}

type DeploymentTarget struct {
    Cloud         string           // "aws" | "azure" | "gcp"
    Region        string
    CredentialRef string            // pluggable; resolved via secrets.Backend
    Spec          map[string]any    // per-cloud overrides merged onto Component.Spec
    Primitives    []ResourcePrimitive // populated by the cloud adapter at plan time
}

type ResourcePrimitive struct {
    ID         string              // stable; "<component>.<target>.<localname>"
    Cloud      string
    TofuType   string              // "aws_db_instance"
    TofuName   string
    Attributes map[string]any      // raw Tofu JSON-config body
    DependsOn  []string            // primitive IDs
}
```

**YAML -> IR mapping.** A `components:` list maps directly to `[]Component`. `target: [aws, gcp]` shorthand expands to two `DeploymentTarget`s with `Mode=replicate`. A top-level `composition:` block registers a `Composition` that becomes a usable `type:`.

**IR -> OpenTofu mapping.** Provisioner walks `[]Component -> []DeploymentTarget -> cloud.Adapter.Emit(target) -> []ResourcePrimitive`. Tofu-runner writes one `main.tf.json` per target workspace: `{"resource": {tofuType: {tofuName: attributes}}}`.

**Versioning.** Each minor IR bump is additive-only. Breaking changes require a new `APIVersion` that lives alongside the old one. Adapter interfaces declare the APIVersion they consume. JSON Schema is generated from Go IR types at build time so the YAML schema, runtime validator, and Go structs cannot drift. Pre-1.0 = `v1alpha1`; semver thereafter. Deprecation: one minor with `Deprecated` warning, removal next major.

---

## 4. Data flow (three traced examples)

**A) `mytool plan ./project.yaml`**

1. CLI builds an `Engine` with SQLite inventory (or none if `--no-inventory`) and local secrets backend.
2. `Engine.LoadProject(path)` → dsl/loader returns unvalidated IR.
3. `Engine.Validate(ir)` → validator runs JSON Schema + semantic checks + Composition expansion.
4. `Engine.Plan(ir, stack)` → provisioner asks component-registry → cloud adapters → produces `ResourcePrimitive`s per target; tofu-runner writes `main.tf.json` and runs `tofu init && tofu plan -out plan.bin`.
5. In parallel: cost-estimator walks primitives, calls adapter `PricingKey()`, hits pricing-cache, returns estimate tree.
6. Engine persists `runs` row (status `planned`), `cost_estimates` rows; returns `PlanResult` to the CLI.
7. CLI prints diff + estimate. (No-inventory mode: no persistence; prints only.)

**B) Web app "Deploy" on a 2-cloud parity component**

1. Web backend receives `POST /api/v1/projects/{id}/deployments`. Session resolves `(user_id, org_id)`.
2. Web-api calls `Engine.Apply(projectID, stackID, opts)` async; returns `runID`.
3. Provisioner sees two `DeploymentTarget`s (aws, gcp). Creates two `deployment_targets` rows and two child `runs` rows.
4. Parity orchestrator (§6) spawns two tofu-runner workspaces in parallel; streams logs to `run_logs`; emits SSE on `/api/v1/runs/{id}/events`.
5. AWS succeeds → run row → `applied`; state stored in configured backend; `deployment_targets.status=succeeded`. GCP fails → run row → `failed`; parent `deployments.status=partial_failure`.
6. Cost-estimator persists per-target estimates. Cost-collector ingests actuals on its next poll.
7. Web UI receives final SSE event; renders partial-failure state with retry / rollback options.

**C) Dashboard "monthly cost by cloud"**

1. Cost-collector runs every N minutes (default 6h, configurable) in web/daemon mode. Per `(org_id, cloud)`, it asks the adapter for billing-query params (account IDs, subscription IDs, project IDs from credentials).
2. Adapter calls AWS Cost Explorer / Azure Cost Management / GCP Cloud Billing API → returns normalized rows: `{period_start, period_end, service, resource_id?, region, amount, currency, tags}`.
3. Collector upserts to `cost_actuals` keyed on `(org_id, cloud, period, service, resource_id, tag_set_hash)`. Tags back-link to `deployment_targets` and `components`.
4. `GET /api/v1/costs?groupBy=cloud&period=month` runs a SQL aggregate filtered by `org_id`. Frontend charts the JSON.

---

## 5. Public interface contracts

**Engine library (Go) — the entire frontend-facing surface:**

```go
type Engine interface {
    LoadProject(ctx, path string) (*ir.Project, error)
    Validate(ctx, *ir.Project) (*ValidationReport, error)
    Plan(ctx, *ir.Project, stack string, opts PlanOpts) (*PlanResult, error)
    Apply(ctx, planID string, opts ApplyOpts) (runID string, err error)
    Destroy(ctx, deploymentID string, opts DestroyOpts) (runID string, err error)
    Import(ctx, *ir.Project, mapping ImportMap) (*ImportResult, error)
    GetRun(ctx, runID string) (*Run, error)
    StreamRun(ctx, runID string) (<-chan RunEvent, error)
    EstimateCost(ctx, *PlanResult) (*CostEstimate, error)
    GetCostActuals(ctx, query CostQuery) (*CostReport, error)
    DetectDrift(ctx, deploymentID string) (*DriftReport, error)
}
```

Construction: `engine.New(cfg Config) (Engine, error)`. `Config` carries inventory DSN (or `nil` for no-inventory), secrets backend, log sink, registered adapters.

**Web app:** REST + JSON, versioned `/api/v1/...`. SSE for run streaming on `/api/v1/runs/{id}/events`. Cookie sessions for browser, bearer PATs for programmatic. All mutating endpoints take an idempotency key.

**CLI command tree (skeletal):**

```
mytool init                 — scaffolds project.yaml
mytool validate             — Engine.Validate
mytool plan [stack]         — Engine.Plan + EstimateCost (diff + cost)
mytool apply [stack]        — Engine.Plan + Engine.Apply (synchronous; --detach for async)
mytool up [stack]           — plan+apply with -auto-approve guard
mytool destroy [stack]      — Engine.Destroy
mytool cost estimate        — Engine.EstimateCost from last plan
mytool cost actual          — Engine.GetCostActuals
mytool drift                — Engine.DetectDrift
mytool import               — Engine.Import
mytool state {show,rm,mv}   — tofu-runner passthrough scoped to a deployment
mytool serve                — starts the web backend in-process
Global flags: --no-inventory, --inventory-dsn, --org, --stack, --json
```

**Plugin protocol (v1 = interfaces only).** Interfaces destined for gRPC in v2: `cloud.Adapter`, `components.Type`, `cost.PricingProvider`, `cost.BillingProvider`, `secrets.Backend`. Wire format: protobuf generated from IR Go types via `protoc-gen-go`. Plugin transport (v2): subprocess + gRPC over Unix socket, HashiCorp go-plugin pattern. v1 ships the `.proto` definitions and a `pkg/plugin/contract` test suite that every implementation (in-tree or out-of-tree) must pass.

---

## 6. Multi-cloud parity orchestration

When `target: [aws, gcp]`, the provisioner creates one `deployments` row and N `deployment_targets`. Each target gets its own workspace directory under `<workdir>/<deployment_id>/<cloud>-<region>/` and its own state backend. **No shared Tofu state across clouds**, by design — it's safer for partial failure and parallelism.

Execution: parallel by default via `errgroup`; sequential opt-in via `policy.serial: true` on the Component. Each target runs the full `init -> plan -> apply` pipeline independently.

Partial failure policy chosen at run time (CLI flag or web UI):
- **`leave`** (default): keep successful targets; mark deployment `partial_failure`.
- **`rollback`**: destroy successful targets to match the failed state.
- **`retry-failed`**: rerun only the failed targets.

Inventory records per-target status, so the dashboard renders e.g. "aws: green, gcp: red". Cross-cloud composition (v2) will need a different orchestrator that treats targets as a DAG with cross-cloud `depends_on`; v1 rejects `Mode=composed` with a clear error.

---

## 7. Inventory DB schema (high-level)

Every row-bearing table has `org_id UUID NOT NULL` and an index `(org_id, ...)`. SQLite mode hardcodes a single `org_id`. Migrations versioned with `golang-migrate` or similar.

| Table | Purpose |
|---|---|
| `orgs` | tenant root |
| `users` | local + OIDC users; `(org_id, email)` unique |
| `api_tokens` | PATs |
| `projects` | `(org_id, name)`; pointer to source repo/path |
| `stacks` | per-stack vars and state-backend config |
| `components` | declared IR snapshot per `(project_id, stack_id, component_name)`, stored as JSONB IR |
| `compositions` | user-defined component types per project |
| `deployments` | one row per `Engine.Apply`; status, requested by `user_id`, partial-failure policy |
| `deployment_targets` | per `(deployment_id, cloud, region)`; status, workspace path, state backend ref, `credential_ref` |
| `runs` | per `tofu` invocation: `(deployment_target_id, kind ∈ {plan,apply,destroy}, status, exit_code, started_at, finished_at)` |
| `run_logs` | streamed lines (or object-storage pointer in server mode) |
| `drift_status` | per `deployment_target_id`, latest drift summary |
| `cost_estimates` | per `(run_id, primitive_id)` |
| `cost_actuals` | per `(org_id, cloud, period, service, resource_id, tag_set_hash)` |
| `secrets_refs` | name → backend descriptor (no values stored) |
| `audit_log` | append-only `(org_id, actor, verb, target, payload, ts)` |

---

## 8. Secrets & credentials

Cloud credentials are never stored as plaintext in the inventory. The IR and inventory reference a name; resolution at runtime goes through `secrets.Backend`. In-tree backends: env vars, on-disk files (restrictive perms), Vault (stub in v1). Cloud KMS support is a backend implementation, not a separate concern.

Per-`DeploymentTarget` `credentialRef` means a single Project can deploy into multiple AWS accounts / Azure subscriptions / GCP projects without splitting itself.

OpenTofu 1.7+ state encryption is enabled by default for new deployment targets; the key is fetched through the same `secrets.Backend`. The state backend itself is configured per stack (S3 / GCS / Blob / Postgres) and recorded in `deployment_targets.state_backend`. The inventory DB is NOT a state backend — it indexes state, doesn't replace it.

The web app's own secrets (OIDC client secrets, DB credentials) also live in `secrets.Backend`, not in a separate config table.

---

## 9. Cost estimation & dashboard data path

**Estimation.** Per `ResourcePrimitive`, the cloud adapter returns a structured `PricingKey` (e.g., AWS: `{service: "AmazonRDS", instanceType: "db.t3.medium", region: "us-east-1", engine: "postgres", deploymentOption: "Single-AZ"}`). Cost-estimator passes this to pricing-cache, which either has a fresh entry, fetches from the live API, or falls back to the bundled snapshot. Estimator multiplies by usage assumptions (default 730 hr/month for compute, declared `sizeGb` for storage, etc.); usage assumptions are per-primitive-type defaults overridable via component `spec.usage`. Result aggregated as a tree → persisted as `cost_estimates`.

**Actuals.** Cost-collector polls per `(org_id, cloud)`. Each adapter exposes `BillingQuery(creds, since, until) -> []NormalizedCostRow`. Normalization fields: `period_start, period_end, service, resource_id, region, amount, currency, tags`. Resource IDs match the cloud-native ARNs/IDs the provisioner stored next to primitives, so joining `cost_actuals` → `deployment_targets` is direct. Where the cloud returns only aggregated rows, the collector uses tag-based attribution. Every resource the provisioner creates is tagged with `infra:component`, `infra:deployment_id`, `infra:org_id`.

Pull frequency: configurable, default 6h for in-scope resources, 24h for full account sweep. High-volume installations enable cost-export integrations (AWS CUR, Azure cost exports, GCP BigQuery billing export); the collector reads from object storage in that mode. The cost-export integration design is per-cloud-adapter and deferred to the Cost Dashboard subsystem spec.

---

## 10. Error handling philosophy

At each architectural seam:

- **Frontend ↔ Engine.** Errors implement `UserFacing() (code, message)` with a stable code set (`ErrYAMLInvalid`, `ErrCloudCredsMissing`, `ErrTofuFailure`, `ErrPartialFailure`, ...). Frontends decide rendering. No raw panics cross this seam — engine has a top-level recover.
- **Engine ↔ Adapter.** Adapters return wrapped errors with cloud + resource context. Engine never inspects strings; only checks codes via `errors.As`. Categories: `Transient` (retried with jittered backoff), `Auth` (no retry), `Validation` (no retry), `Unknown` (no retry).
- **Engine ↔ Tofu.** Tofu-runner parses Tofu's JSON output stream. Non-zero exit + parseable diagnostics → structured error. Non-zero + no diagnostics → raw stderr captured, code `ErrTofuOpaque`. State-lock errors get their own code with remediation message. Engine does NOT retry Tofu; user retries.
- **Engine ↔ Cloud API (cost/billing).** Network + 5xx → retry with backoff. 4xx → fail immediately. Pricing-cache falls back to snapshot on any failure and surfaces a warning (not an error).

---

## 11. Testing strategy

- **Unit.** Each cloud adapter has golden-file tests: IR fragment → Tofu JSON. IR validator has table-driven tests over YAML fixtures (valid + invalid). Cost-estimator fixtures pair IR primitives with expected pricing keys.
- **Integration.** Engine + fake `cloud.Adapter` + real `tofu` binary against LocalStack, Azurite, and gcp-emulator-style fakes where possible. Inventory tested against both SQLite and a Postgres container.
- **Plugin contract tests.** `pkg/plugin/contract` exports `RunAdapterSuite(t, adapter cloud.Adapter)`. v1's three in-tree adapters all run it in CI. When gRPC plugins land in v2, the same suite runs against out-of-process adapters via a test harness.
- **End-to-end.** Separate `e2e/` tree using ephemeral cloud accounts; gated behind credentials; not run on every PR. Covers `plan -> apply -> drift -> destroy` for a representative component matrix.
- **Determinism.** Tofu JSON output is byte-stable for a given IR — adapters sort map keys, primitive ordering is deterministic by ID. Tests assert this.

---

## 12. Critical files to scaffold first

All under `/home/kurt/git/cloud-infra-manager/`:

- `pkg/ir/types.go` — IR Go types
- `pkg/ir/schema.go` — JSON Schema generation
- `pkg/engine/engine.go` — top-level `Engine` interface
- `pkg/engine/config.go` — `engine.New` / `Config`
- `pkg/cloud/adapter.go` — `cloud.Adapter` interface (the plugin contract)
- `pkg/components/registry.go` — component-type registry
- `pkg/inventory/repo.go` — repository interfaces
- `pkg/inventory/migrations/0001_init.sql` — initial schema
- `pkg/plugin/contract/adapter_suite.go` — adapter contract test suite
- `internal/tofu/runner.go` — subprocess wrapper
- `internal/dsl/loader/loader.go` — YAML loader
- `internal/dsl/validator/validator.go` — schema + semantic validator
- `cmd/cli/main.go` — Cobra root
- `cmd/server/main.go` — web backend root
- `go.mod`, `tools/build/`, `Makefile`

---

## 13. Out of scope for this spec

These are deferred to per-subsystem specs:

- Specific resource coverage per cloud (which component types in v1, which fields each supports)
- Exact YAML field names and final JSON Schema shapes
- Web UI design / component library / styling
- DAG scheduling internals (v1 is per-target; intra-target relies on Tofu's own DAG)
- Cross-cloud composition execution semantics (v2 spec)
- GitOps daemon design (v2 spec)
- gRPC plugin runtime mechanics (v2 spec) beyond the interfaces
- Detailed per-cloud billing-export integration
- Per-subsystem retry counts, timeouts, default usage assumptions
- Observability backends (assumed: structured `slog` + Prometheus, but not specified here)
- Backup/DR for the inventory DB

---

## 14. Follow-up specs (in implementation order)

1. **DSL & IR spec** — finalize JSON Schema, define v1 component types (`network`, `compute`, `database`, `storage` as starting set), YAML reference syntax, validation rules.
2. **Provisioner & cloud-adapter spec** — exact Tofu JSON emission per primitive type, adapter interface details, multi-target orchestration mechanics, drift detection algorithm.
3. **Cost estimator spec** — pricing-key normalization, usage-assumption model, snapshot format, estimation accuracy targets.
4. **Cost dashboard spec** — billing-API integration per cloud, cost-export integrations, normalization schema, dashboard query shapes.
5. **Web app spec** — API endpoints in full, auth flows, UI surface inventory.
6. **gRPC plugin spec** (v2) — go-plugin wiring, plugin discovery, protocol versioning.
7. **GitOps daemon spec** (v2) — repo polling vs. webhook, reconciliation loop, drift policy.

---

## Verification

This is an architecture-level spec, not an implementation. "Verification" here means confirming the contracts are coherent enough to implement against. Verify by:

1. **IR walkthrough.** Manually trace a small YAML example (1 network component, 1 database component, targeted at AWS and GCP) through Load → Validate → Plan → Apply → cost estimate → actual cost reconciliation. Every step should be doable using only the interfaces and types defined here.
2. **Adapter contract test scaffolding.** Write the `pkg/plugin/contract` adapter suite as the FIRST piece of real code. If the contract is hard to express in tests, the interfaces are wrong — iterate before writing implementations.
3. **Round-trip test.** Generate JSON Schema from `pkg/ir` Go types; validate a sample YAML; load it back; round-trip to Tofu JSON; ensure stable bytes.
4. **Per-subsystem specs reference back.** When the DSL/IR spec, Provisioner spec, etc. are written, each should be implementable without changing this spec. If a follow-up spec needs to change a contract here, that's a signal this spec needs revision before implementation continues.
5. **Frontend stub.** Before any subsystem ships, build a "hello-world" Engine implementation that returns canned `PlanResult` / `RunEvent` values. Stand up both the CLI (`mytool plan`) and the web backend (`POST /api/v1/projects/{id}/deployments`) against it. If the stub works through both frontends with the SAME engine interface, the layering is right.
