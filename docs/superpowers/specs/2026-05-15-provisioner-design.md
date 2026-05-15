# Provisioner & Cloud-Adapter Subsystem Spec

**Status:** Subsystem spec. Defines the contract between a validated `*ir.Project` and the OpenTofu workspaces that get planned and applied. Covers the provisioner, the cloud-adapter contract, the Tofu subprocess wrapper, the state-bridge / drift-detection design, and multi-target orchestration. Does NOT enumerate concrete per-cloud emitters (each cloud gets its own follow-up spec / phase plan).

**Date:** 2026-05-15
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (locked-in module boundaries, IR shape, run model)
- `docs/superpowers/specs/2026-05-15-dsl-ir-design.md` (consumes a fully-validated `*ir.Project` after compositions are expanded and lazy refs are typed)
- `docs/superpowers/specs/2026-05-15-parity-design.md` (consumes `Adapter.Profile()` after `Emit()`; this spec defines the `Profile()` contract slot)

**Depended on by:**
- Per-cloud adapter specs (AWS, Azure, GCP) — they implement the contract this spec defines
- Cost estimator spec — consumes `Adapter.PricingKey()` produced here, plus the `runs.cost_estimate_id` linkage
- Cost dashboard spec — consumes `Adapter.BillingQuery()` / `FetchBilling()` defined here
- Web app spec — surfaces `Run`s and SSE streams produced by the Tofu runner
- gRPC plugin spec (v2) — turns this Go interface into a `.proto` service

---

## Context

The DSL/IR spec turns YAML on disk into a validated `*ir.Project` with all eager interpolations resolved, compositions expanded, and sizing normalized. The parity spec defines how cloud-specific resource picks are profiled and compared. This spec defines what happens **between** those two surfaces and the actual OpenTofu invocation: how a `Component` becomes a set of `ResourcePrimitive`s, how those primitives become a workspace on disk, how the workspace gets `tofu init`/`plan`/`apply`'d, and how the result is reconciled back into the inventory.

The provisioner is the only place that knows about workspaces, parallel orchestration, and partial-failure policy. Cloud adapters never see workspaces or run state — they are pure functions from IR shapes to Tofu JSON shapes. The Tofu runner never sees IR — it only knows about directories, plan files, and exit codes. Each module owns one job and the seams are typed.

**Design principles:**
1. **One workspace per `DeploymentTarget`.** No shared state across clouds, regions, or targets — failure is local, parallelism is free, and per-target state backends compose naturally. This was locked in by the architecture spec §6 and is not relitigated.
2. **Adapters are pure.** `Emit()`, `PricingKey()`, and `Profile()` are deterministic functions of their inputs — no network, no globals, no clocks. Side effects (subprocesses, billing API calls, secrets resolution) live in the runner, the secrets backend, or the dedicated `FetchBilling` method. This makes adapters trivially testable via golden files.
3. **Determinism is a contract, not an aspiration.** Same IR + same adapter version → byte-identical Tofu JSON. Adapters sort map keys; primitives are ordered by stable IDs; resource attribute serialization is canonical. CI asserts this.
4. **The runner is dumb.** It knows about directories, env vars, plan files, and exit codes. It does not know about clouds, components, or organizations. This makes the subprocess wrapper testable in isolation against a real `tofu` binary using empty modules.
5. **Lazy refs resolve at the latest possible moment.** `${target.*}` resolves per-target inside `Emit()`; `${component.X.outputs.Y}` resolves between targets after upstream `apply`s succeed; `${secret.*}` resolves at adapter call time via the secrets backend. The IR keeps these as typed marker values; the provisioner walks them down to concrete values just before they're needed.

---

## Scope

**In scope (this spec):**
- Provisioner package layout, public types, and execution model
- Cloud-adapter interface in detail: `Emit`, `PricingKey`, `Profile`, `BillingQuery`, `FetchBilling`, `DefaultStateBackend`, `SupportedComponentTypes`, `SupportedAPIVersions`, `TierOneSchema`, `Validate`
- Component-type registry and dispatch
- Workspace layout on disk (per `DeploymentTarget`)
- Tofu JSON emission contract (canonical form, determinism rules, merge order)
- Tofu subprocess wrapper (`internal/tofu`) — full command surface
- State backend selection (per-stack default → per-target override → adapter default)
- State encryption key wiring through `secrets.Backend`
- Multi-target orchestration: parallel execution, serial opt-in, error-group semantics
- Partial failure policies: `leave`, `rollback`, `retry-failed`
- Cross-target reference resolution between phases
- State-bridge and drift-detection algorithm
- Run lifecycle and persistence (`deployments`, `deployment_targets`, `runs`, `run_logs` rows)
- Tagging contract (every cloud resource carries `infra:component`, `infra:deployment_id`, `infra:org_id`)
- Plugin contract test suite additions for v1 adapters
- CLI integration (`plan`, `apply`, `up`, `destroy`, `state {show,rm,mv,list}`, `drift`, `import`)
- API surface (`/api/v1/.../plan`, `/api/v1/.../apply`, `/api/v1/runs/{id}/events`)
- Error model and remediation messages
- Verification strategy

**Out of scope (deferred):**
- Concrete per-cloud emit logic — each of AWS / Azure / GCP gets its own subsystem spec or implementation plan, which slot in under this contract.
- Cross-cloud composed mode (`Component.Mode == "composed"`) — v2; the provisioner returns `ErrComposedNotSupported` in v1.
- Out-of-tree (gRPC) plugins — v1 ships interfaces and the contract test suite only; subprocess transport is a v2 deliverable per architecture §5.
- Cost estimation — `PricingKey()` is defined here so adapters implement it now, but the estimator that consumes it is a separate spec.
- Cost actuals collection — `BillingQuery()` and `FetchBilling()` are defined here for the same reason; the collector / dashboard ingest path is a separate spec.
- Auto-import inference — `Engine.Import` is wired to the runner in v1 but the IR-inference logic that proposes mappings from real cloud state is a v2 spec.
- GitOps daemon — separate v2 spec; this spec's run model is the substrate it eventually reuses.
- Secrets-backend implementation — only the `secrets.Backend` calling convention from the provisioner is defined here; concrete backends (env, file, Vault) are a separate spec.

---

## Module layout

```
pkg/provisioner/
  provisioner.go         # Provisioner interface + New(); orchestrates a Plan/Apply
  plan.go                # Plan(): IR -> ResourcePrimitives -> workspace -> tofu plan
  apply.go               # Apply(): workspace -> tofu apply -> state read-back -> inventory
  destroy.go             # Destroy(): mirror of Apply for teardown
  refs.go                # ResolvedRefs assembly: lazy ref walk + cross-target join
  orchestrator.go        # parallel/serial fan-out, errgroup, partial-failure policy
  workspace.go           # WorkspaceLayout, write main.tf.json + backend.tf.json + provider.tf.json
  emit.go                # canonical JSON serialization for primitives (sorted keys, stable order)
  tagging.go             # framework-mandated tags injected into every primitive
  events.go              # PlanResult, ApplyResult, RunEvent producer
  errors.go              # provisioner-owned error codes
  contract_test.go       # runs against fake adapter; asserts determinism + tagging

pkg/cloud/
  adapter.go             # (existing) Adapter interface — extended this spec
  registry.go            # adapter registry (string -> Adapter); created here
  refs.go                # ResolvedRefs (existing); typed lazy ref shapes
  emit_helpers.go        # helpers adapters use to build canonical resource blocks

pkg/components/
  registry.go            # (existing) component-type registry; this spec adds dispatch helpers
  type.go                # Type interface (Name, Cloud(), SpecSchema, Outputs); used by adapters

internal/tofu/
  runner.go              # (existing) Runner interface
  exec_runner.go         # subprocess Runner impl (default)
  diagnostics.go         # parse `tofu show -json` diagnostics into structured errors
  workspace.go           # Workspace I/O helpers (write tf.json, atomic file ops)
  state_show.go          # parse `tofu show -json` post-apply state
  fake_runner.go         # in-memory Runner for tests (no `tofu` binary required)

internal/state/bridge/
  bridge.go              # StateBridge: introspect Tofu state JSON; reconcile to inventory
  drift.go               # DetectDrift: compare last apply's state with current `tofu plan`
  reconcile.go           # write-back of `tofu apply` results into deployment_targets

internal/cloud/{aws,azure,gcp}/   # populated by per-cloud specs
  adapter.go             # Adapter implementation
  emit.go                # IR -> Tofu JSON
  pricing.go             # PricingKey()
  profile.go             # Profile()
  billing.go             # BillingQuery() + FetchBilling()
  state_backend.go       # DefaultStateBackend()
  schema.go              # TierOneSchema()
  testdata/              # golden files: IR fragment <-> Tofu JSON
```

The provisioner is the only package that imports both `pkg/cloud` and `internal/tofu`. Cloud adapters never import the runner; the runner never imports adapters; the engine never imports either directly (it goes through `pkg/provisioner`). This breaks dependency cycles by construction.

---

## Public surface

### Provisioner

```go
// pkg/provisioner/provisioner.go
package provisioner

type Provisioner interface {
    // Plan walks a validated IR project and produces per-target plans.
    // Returns a PlanResult that the engine persists and the CLI/web renders.
    Plan(ctx context.Context, in PlanInput) (*PlanResult, error)

    // Apply executes a previously-produced plan. The plan ID is the only
    // input besides options; the plan carries everything needed.
    Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)

    // Destroy tears down a deployment. Mirrors Apply but inverted.
    Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error)
}

type PlanInput struct {
    Project        *ir.Project        // fully validated; compositions expanded; sizing resolved
    Stack          string             // selected stack name; must exist in Project.Stacks
    OrgID          string             // for inventory writes; "" in --no-inventory mode
    DeploymentID   string             // pre-allocated by engine; correlates rows
    PartialFailure PartialFailurePolicy
    Refresh        bool               // pass `-refresh=true` to tofu plan
    Targets        []TargetFilter     // restrict plan scope (component/cloud/region)
}

type PlanResult struct {
    DeploymentID string
    Stack        string
    Targets      []TargetPlan      // one per (component, cloud, region) actually planned
    HasChanges   bool              // OR over targets
    Diagnostics  []Diagnostic      // non-fatal warnings collected across targets
    GeneratedAt  time.Time
}

type TargetPlan struct {
    DeploymentTargetID string
    Component          string
    Cloud              string
    Region             string
    WorkspaceDir       string             // absolute path
    PrimitiveCount     int
    PlanFile           string             // path to the binary plan
    JSONPlanPath       string             // path to `tofu show -json plan` output
    HasChanges         bool
    Adds, Changes, Destroys int
    Profile            []parity.TargetProfile  // one per primitive; for parity engine
    PricingKeys        []map[string]any        // one per primitive; for cost estimator
    Tags               map[string]string
}

type ApplyInput struct {
    PlanResult     *PlanResult
    OrgID          string
    PartialFailure PartialFailurePolicy
    AutoApprove    bool
    AllowParityViolations bool      // see parity spec
}

type ApplyResult struct {
    DeploymentID  string
    TargetResults []TargetApplyResult
    Status        ApplyStatus           // "succeeded" | "partial_failure" | "failed"
    GeneratedAt   time.Time
}

type TargetApplyResult struct {
    DeploymentTargetID string
    RunID              string
    Status             RunStatus     // "succeeded" | "failed" | "skipped"
    State              StateSnapshot // parsed state for upstream consumers
    Outputs            map[string]any
    Error              error         // wrapped; nil on success
}

type DestroyInput struct {
    DeploymentID   string
    PartialFailure PartialFailurePolicy
    AutoApprove    bool
}

type PartialFailurePolicy string

const (
    PartialFailureLeave        PartialFailurePolicy = "leave"        // default
    PartialFailureRollback     PartialFailurePolicy = "rollback"
    PartialFailureRetryFailed  PartialFailurePolicy = "retry-failed"
)
```

### Adapter contract (extended)

The pre-existing `cloud.Adapter` interface (defined in commit 9e1c1ea) gains four methods this spec locks in. Existing methods are unchanged in shape; semantics are pinned below.

```go
// pkg/cloud/adapter.go
package cloud

type Adapter interface {
    Name() string
    SupportedAPIVersions() []string

    // SupportedComponentTypes returns the built-in component type names this
    // adapter implements (e.g., ["network", "compute", "database", "storage"]).
    // The validator and the provisioner consult this to fail fast on
    // (component.Type, target.Cloud) mismatches.
    SupportedComponentTypes() []string

    // TierOneSchema returns the JSON Schema for the `<cloud>:` block under
    // DeploymentTarget.Spec. Loaded once at startup, merged into the
    // generated IR schema by the validator. Adapters MAY include `description`
    // strings; IDE tooltips render them.
    TierOneSchema() []byte

    // Validate runs cloud-specific semantic checks beyond JSON Schema. Called
    // by the validator's phase 7 (semantic). Returns issues with stable codes.
    // MUST be pure: no network, no disk.
    Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue

    // Emit takes one DeploymentTarget (already merged with stack vars and
    // composition expansion) and returns the ResourcePrimitives the
    // provisioner writes into Tofu JSON. Emit is pure: no network, no disk,
    // no globals, no clocks.
    Emit(ctx context.Context, target ir.DeploymentTarget, refs ResolvedRefs) ([]ir.ResourcePrimitive, error)

    // Profile returns normalized resource attributes for a primitive. The
    // parity engine and cost estimator share this data path. Pure.
    Profile(ctx context.Context, primitive ir.ResourcePrimitive) (parity.ResourceProfile, error)

    // PricingKey returns the cloud-native pricing identifier for one primitive.
    // Cost estimator hands this to the pricing cache. Opaque to the engine.
    PricingKey(ctx context.Context, primitive ir.ResourcePrimitive) (map[string]any, error)

    // BillingQuery returns the parameters needed to fetch actual cost rows for
    // a credential over a time window. Cost collector dispatches this to the
    // cloud's billing API.
    BillingQuery(ctx context.Context, creds Credentials, since, until time.Time) (BillingQueryParams, error)

    // FetchBilling executes a billing query and returns normalized rows.
    // Separated from BillingQuery so the collector can mock the API call while
    // still exercising the query construction in tests.
    FetchBilling(ctx context.Context, creds Credentials, params BillingQueryParams) ([]NormalizedCostRow, error)

    // DefaultStateBackend returns the recommended state backend for this
    // cloud when the user has not declared one (per stack or per target).
    DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error)

    // ProviderBlock returns the Tofu `provider` block for this cloud. The
    // provisioner writes one provider.tf.json per workspace using this output.
    // Region and credentialRef are taken from the target; secret material is
    // resolved by the caller and passed in `creds.Payload`.
    ProviderBlock(ctx context.Context, target ir.DeploymentTarget, creds Credentials) (map[string]any, error)
}
```

### Adapter registry

```go
// pkg/cloud/registry.go
package cloud

type Registry interface {
    Register(a Adapter) error              // idempotent for same name+version; conflict otherwise
    Get(name string) (Adapter, bool)       // O(1); returns false if unknown
    List() []Adapter                       // deterministic order: alpha by Name()
}

func NewRegistry() Registry
```

The engine constructs a `Registry`, registers all in-tree adapters at startup (`aws.New()`, `azure.New()`, `gcp.New()`), and passes it to the provisioner via `engine.Config.CloudAdapters`. Out-of-tree adapters (v2) plug in via the same registry once gRPC discovery lands.

### Component type registry (extended)

```go
// pkg/components/registry.go
package components

type Type interface {
    Name() string                     // "database", "compute", "network", "storage"
    SupportedClouds() []string        // intersection with adapter.SupportedComponentTypes()
    SpecSchema() []byte               // JSON Schema for component.spec
    Outputs() map[string]OutputType   // declared output names + types (for ref resolution)
}

type OutputType struct {
    Kind string  // "string" | "number" | "boolean" | "list<string>" | "map<string,string>" | ...
}

type Registry interface {
    Register(t Type) error
    Get(name string) (Type, bool)
    List() []Type
}
```

The component-type registry is populated by `pkg/components` init functions (one per built-in type — `network`, `compute`, `database`, `storage`). User-defined Compositions don't appear here (they are expanded into built-in types at validation time per the DSL/IR spec).

---

## Workspace layout

Every `DeploymentTarget` gets a directory under `<workdir>/<deployment_id>/<cloud>-<region>/<component>/`. The provisioner writes exactly four files into it before invoking `tofu init`:

```
<workdir>/<deployment_id>/<cloud>-<region>/<component>/
  main.tf.json                # all resource blocks for this target
  provider.tf.json            # provider configuration (one per cloud)
  backend.tf.json             # state backend configuration
  versions.tf.json            # required_version + required_providers
```

All files are JSON (not HCL). HCL exists for human authors; this is machine output. JSON tooling beats HCL tooling for byte-stable round-trips.

### File contents

`versions.tf.json` is template-fixed per cloud (the adapter declares the provider source + version constraint via a `Versions()` helper bundled in `pkg/cloud/emit_helpers.go`):

```json
{
  "terraform": {
    "required_version": ">= 1.7.0",
    "required_providers": {
      "aws": { "source": "hashicorp/aws", "version": "~> 5.30" }
    }
  }
}
```

`provider.tf.json` is `Adapter.ProviderBlock(target, creds)` wrapped in `{"provider": {...}}`:

```json
{
  "provider": {
    "aws": {
      "region": "us-east-1",
      "default_tags": { "tags": { "infra:component": "orders-db", "infra:deployment_id": "...", "infra:org_id": "..." } }
    }
  }
}
```

Credentials are NEVER written to a file. The provisioner sets `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (or `AZURE_*` / `GOOGLE_APPLICATION_CREDENTIALS`) in the runner's `Workspace.Environment`, scoped to the subprocess only.

`backend.tf.json` is the resolved state backend (see §"State backends"):

```json
{
  "terraform": {
    "backend": {
      "s3": {
        "bucket": "nimbusfab-state",
        "key": "orders/prod/orders-db/aws-us-east-1.tfstate",
        "region": "us-east-1",
        "encrypt": true
      }
    }
  }
}
```

`main.tf.json` is the canonicalized output of `Adapter.Emit()` for this target, wrapped in `{"resource": {...}}`:

```json
{
  "resource": {
    "aws_db_instance": {
      "orders_db": {
        "identifier": "orders-db",
        "engine": "postgres",
        "engine_version": "16",
        "instance_class": "db.t3.medium",
        "allocated_storage": 100,
        "iops": 3000,
        "storage_type": "gp3",
        "username": "postgres",
        "password": "${var.db_password}",
        "db_subnet_group_name": "${aws_db_subnet_group.orders_db.name}",
        "tags": {
          "infra:component": "orders-db",
          "infra:deployment_id": "...",
          "infra:org_id": "..."
        }
      }
    },
    "aws_db_subnet_group": {
      "orders_db": { "...": "..." }
    }
  }
}
```

### Atomic write protocol

The provisioner writes files via the standard `tmpfile + fsync + rename` pattern so a crash mid-write never leaves a half-written workspace. Each file's content is hashed (sha256) and the hash is recorded in `deployment_targets.workspace_hash` so subsequent plans can detect external tampering.

### Workspace lifecycle

- Created on the first `Plan()` call for the target.
- Reused on subsequent `Plan()` and `Apply()` calls — `tofu init` is skipped if `.terraform/` exists and `versions.tf.json` is unchanged.
- Garbage-collected by `nimbusfab gc` (separate command, scope deferred): removes workspaces whose `deployment_targets` row was deleted more than N days ago.
- Never shared across processes — workspace operations take a file lock (`flock`) on `<workspace>/.lock` to make concurrent CLI invocations safe.

### Deterministic ordering

Inside `main.tf.json`:
- Resource type blocks ordered alphabetically by Tofu type name (`aws_db_instance` before `aws_db_subnet_group`).
- Within a type, resources ordered alphabetically by Tofu name.
- Map keys sorted alphabetically at every nesting depth.
- Lists preserve order from the adapter (adapters MUST return primitives in deterministic order, typically by ID).

CI asserts byte-identical output for the same IR + same adapter version (see §"Verification").

---

## Tofu JSON emission contract

`Adapter.Emit(target, refs)` returns `[]ir.ResourcePrimitive`. Each primitive is one Tofu resource block:

```go
type ResourcePrimitive struct {
    ID         string         // stable; "<component>.<target_id>.<localname>"
    Cloud      string
    TofuType   string         // "aws_db_instance"
    TofuName   string         // identifier safe per Tofu rules (no dots, no leading digits)
    Attributes map[string]any // raw Tofu JSON-config body
    DependsOn  []string       // primitive IDs this depends on (intra-target)
    Tags       map[string]string  // for cloud resources that support tagging
}
```

### Merge order at Emit

Per the DSL/IR spec, `Component.Spec`, `target.Spec.<cloud>` (tier-1 typed override), and `target.Spec.raw` (tier-2 raw passthrough) merge in that order. The adapter sees the already-merged `target.Spec`; the provisioner does the merge before calling Emit. Adapters MUST honor tier-2 `raw:` keys by merging them into the emitted Attributes map after the adapter's own attribute construction. The adapter emits one `WarnRawEscape` per raw key so the diagnostic surface stays consistent.

### Tagging contract

The provisioner injects three framework-mandated tags into every primitive's `Tags` map after `Emit()`:

| Tag key | Value |
|---|---|
| `infra:component` | `Component.Name` |
| `infra:deployment_id` | `Deployment.ID` |
| `infra:org_id` | `Org.ID` (or `local` in `--no-inventory` mode) |

Adapters then translate `Tags` into the cloud-native shape (`tags = {...}` for AWS/GCP, `tags = {...}` block for Azure resources, `labels` for GCP IAM, etc.). This contract guarantees that `cost_actuals` rows can be joined back to `deployment_targets` even when the cloud's billing API only returns aggregated rows. Resources whose cloud type doesn't support tagging (rare; e.g., some IAM primitives) have `Tags` ignored — adapters declare the constraint via a documented per-resource note in their golden files.

### Lazy-ref encoding

Lazy refs that the validator preserved as `ir.LazyRef` markers become Tofu interpolation strings at emit time:

| LazyRef shape | Tofu encoding |
|---|---|
| `${target.cloud}` | substituted to the literal cloud string by the provisioner before Emit |
| `${target.region}` | same |
| `${target.credentialRef}` | substituted (used by provider block, not resource bodies) |
| `${component.X.outputs.Y}` | rewritten by provisioner to `data.terraform_remote_state.X.outputs.Y` after upstream apply succeeds |
| `${secret.<ref>.<key>}` | resolved via `secrets.Backend` and substituted as a literal string OR injected as a `var.<name>` reference whose value flows through `Workspace.Vars` |
| `${ref.<as>}` | substituted to the resolved `${component.<src>.outputs.<output>}` form using the component's `refs:` aliases |

Adapters never see `LazyRef` types — the provisioner walks them down to concrete strings or into the `Workspace.Vars` table before invoking Emit. This keeps adapters pure.

### Cross-target outputs

When component B references component A's outputs (e.g., `subnetIds: ${component.web-network.outputs.subnet_ids}`), the provisioner inserts a `data "terraform_remote_state"` block in B's workspace:

```json
{
  "data": {
    "terraform_remote_state": {
      "web_network": {
        "backend": "s3",
        "config": { "bucket": "...", "key": "...", "region": "..." }
      }
    }
  },
  "resource": { "...": "..." }
}
```

A's apply MUST complete before B's plan runs (orchestrator handles ordering via the refs DAG; see §"Orchestration"). Same-stack cross-target refs are resolved through this data block; cross-stack refs are explicitly v2.

### Determinism rules

- Map keys serialize sorted (every level).
- Numbers serialize as integers when integral, floats otherwise. Adapters never emit a float for an integer-valued attribute.
- Strings serialize without escaping unless required by JSON; no `&` for `&`, etc.
- Empty maps and empty lists serialize as `{}` / `[]`, not omitted.
- Adapters emit `null` for explicit nullification only; missing keys are simply absent.
- Adapters MUST produce identical output for identical input across runs and across processes. CI runs the same Emit twice and asserts byte equality.

---

## State backends

### Selection algorithm

The state backend for a `DeploymentTarget` is resolved in this order (first non-empty wins):

1. `Component.Spec.target[cloud].stateBackend` (per-target override; rare, surfaced as a tier-1 escape hatch field on the typed `<cloud>:` block).
2. `Stack.StateBackend` (the user's per-stack default, declared in `project.yaml`).
3. `Adapter.DefaultStateBackend(target)` (the cloud's recommendation; e.g. AWS returns `{"kind": "s3", "config": {...}}` keyed off the stack name).

Resolution happens once per Plan and is recorded in `deployment_targets.state_backend`. Subsequent Apply / Destroy / Plan reuse the recorded value — changing a stack's state backend mid-deployment requires `nimbusfab state migrate` (separate command, scope deferred).

### Encryption

OpenTofu 1.7+ supports built-in state encryption (`encryption {}` block). The provisioner enables it by default for new deployment targets:

```json
{
  "terraform": {
    "encryption": {
      "key_provider": { "pbkdf2": { "passphrase": "${var.tofu_encryption_passphrase}" } },
      "method":       { "aes_gcm":  { "keys": "${key_provider.pbkdf2}" } },
      "state":        { "method": "${method.aes_gcm}" },
      "plan":         { "method": "${method.aes_gcm}" }
    },
    "backend": { ... }
  }
}
```

The passphrase resolves through `secrets.Backend` (per-org key, derived from `key_provider.<org_id>` lookup). The variable definition lives in `versions.tf.json`; the value flows through `Workspace.Vars`. Users who want to opt out of encryption set `stateBackend.encryption: false` per stack — surfaced as an explicit decision in the diff.

### Backend kinds

The IR's `StateBackend` discriminates by `kind`:

| Kind | Per-cloud default | Notes |
|---|---|---|
| `local` | (none) | dev only; the default when `--no-inventory` is set and no other backend declared |
| `s3` | AWS default | one bucket per project, key path `<project>/<stack>/<component>/<cloud>-<region>.tfstate` |
| `gcs` | GCP default | analogous; `cloud.google.storage` |
| `azurerm` | Azure default | analogous; one storage account, one container per project |
| `pg` | (none) | shared-DB option for users who don't want object storage |

Adapter `DefaultStateBackend` MUST return a kind that adapter understands; never another cloud's. Mixed-cloud projects with no per-stack default end up with one S3 bucket for AWS targets, one GCS bucket for GCP targets, etc. — which is the right answer.

---

## Tofu subprocess wrapper (`internal/tofu`)

### Runner interface (locked)

The interface declared in commit 9e1c1ea is the locked contract; this spec adds two methods (`Validate` and `Output`) and pins semantics:

```go
type Runner interface {
    Init(ctx context.Context, ws Workspace) error
    Validate(ctx context.Context, ws Workspace) (*ValidateResult, error)
    Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error)
    Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error
    Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error
    Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error)   // JSON
    StateShow(ctx context.Context, ws Workspace) ([]byte, error)               // tofu show -json
    StateRm(ctx context.Context, ws Workspace, address string) error
    StateMv(ctx context.Context, ws Workspace, from, to string) error
    Output(ctx context.Context, ws Workspace) (map[string]any, error)          // tofu output -json
    Version(ctx context.Context) (string, error)
}
```

### Argument construction

Every Tofu command is invoked with:
- `-no-color` (machine-parseable output)
- `-input=false` (never block on prompts)
- `-lock-timeout=300s` (state lock contention is common in CI; wait up to 5min)
- `-json` for `plan`, `apply`, `destroy`, `validate` (machine-readable progress stream)
- Working directory set to `Workspace.Dir`
- Environment from `Workspace.Environment` (credentials live here, not in files)

`Init()` additionally passes `-backend-config` flags for any backend keys the provisioner injects out-of-band (rare).

### Output stream parsing

Tofu's `-json` mode emits one JSON object per line over stdout. The runner parses these into typed `RunEvent`s:

```go
type RunEvent struct {
    Timestamp time.Time
    Level     string  // "info" | "warn" | "error"
    Type      string  // "version" | "planned_change" | "apply_start" | "apply_complete" | "diagnostic"
    Resource  string  // Tofu address, when applicable
    Action    string  // "create" | "update" | "delete" | "no-op" | "read"
    Message   string  // human-readable
    Raw       map[string]any  // the full source object
}
```

Events stream to `Workspace.EventsOut` (a `chan<- RunEvent`) in real time. The provisioner consumes events to update `runs.progress` and stream them to web SSE subscribers. The full raw stream is also written to `Workspace.Stdout` for log archival.

### Diagnostic mapping

When Tofu exits non-zero, the runner parses the trailing diagnostics from the `-json` stream. Each diagnostic becomes a structured error:

```go
type Diagnostic struct {
    Severity string   // "error" | "warning"
    Summary  string
    Detail   string
    Address  string   // Tofu resource address when known
    Range    *Range   // file:line:col when Tofu reports source position
}
```

The runner maps known diagnostic patterns to engine error codes:

| Tofu diagnostic | Engine code |
|---|---|
| state lock timeout | `ErrTofuStateLocked` |
| credentials not configured | `ErrTofuCredsMissing` |
| provider plugin not found | `ErrTofuProviderMissing` |
| version constraint not satisfied | `ErrTofuVersionMismatch` |
| any other parseable diagnostic | `ErrTofuDiagnostic` (carries the structured `Diagnostic`) |
| non-zero exit, no diagnostics parseable | `ErrTofuOpaque` (carries raw stderr, last 1KB) |

Engine never inspects diagnostic strings; only checks the code via `errors.As`.

### Default implementation

`internal/tofu/exec_runner.go` shells out to `tofu` (resolved via `$PATH` or `Engine.Config.TofuBinary`), assembles the workspace, streams events. Process supervision uses `os/exec` with `Cancel` set to `SIGINT`-then-`SIGKILL` after 10s grace. No process pooling — each Tofu invocation is a fresh subprocess.

### Fake implementation for tests

`internal/tofu/fake_runner.go` is an in-memory Runner that records inputs and returns scripted outputs. Provisioner unit tests use this to assert workspace contents and orchestration without spawning Tofu. Integration tests use the real `exec_runner` against fixture projects with `-backend=local`.

---

## Orchestration

### Refs DAG

Before any plan, the provisioner builds a DAG of components from the validated IR:

- Nodes: `Component`s.
- Edges: `Component.Refs[].Component` (explicit) ∪ inline `${component.X.outputs.Y}` references discovered during eager interpolation (these were recorded by the validator in `Project.RefGraph`).
- Topological sort produces the plan/apply order. Cycles were rejected at validation (`ErrCycleInRefs`).

A target-level DAG is built per component DAG node by exploding into `(component, cloud, region)` tuples. Targets within a component are independent of each other (no intra-component cross-target refs in v1) so they fan out fully parallel.

### Execution model

```
for each component in topo order:
    if component.policy.serial:
        for each target in component.targets:
            run(target)
    else:
        errgroup.WithContext():
            for each target in component.targets:
                go run(target)
        wait()
    if any target failed and component is required by downstream:
        propagate failure per partial-failure policy
```

`run(target)` is the per-target pipeline:

```
1. Resolve credentials via secrets.Backend
2. Resolve DefaultStateBackend (if not already)
3. Resolve cross-target refs (read upstream remote-state outputs)
4. Adapter.Emit(target, refs) -> []ResourcePrimitive
5. Provisioner injects framework tags
6. Provisioner serializes to canonical Tofu JSON (workspace files written atomically)
7. tofu init (skipped if cached)
8. tofu plan -out plan.bin
9. tofu show -json plan.bin -> JSONPlanPath
10. (PLAN phase ends here)
--- if Apply phase ---
11. tofu apply plan.bin
12. tofu show -json -> StateSnapshot
13. tofu output -json -> outputs map
14. State bridge persists deployment_targets.state_backend, runs row, outputs
```

### Concurrency limits

- Default global parallelism: `runtime.NumCPU()` targets at once across all components.
- Per-cloud parallelism: capped at 8 concurrent targets per cloud (avoids API throttle).
- Per-credential parallelism: capped at 4 concurrent targets per `credentialRef` (avoids per-account rate limits).
- Configurable via `engine.Config.MaxConcurrentTargets` and `MaxConcurrentPerCloud`.

These caps are enforced via three semaphores held in series; deadlock-free because acquisition is always in the same order: global → cloud → credential.

### Partial failure policies

When some targets succeed and others fail within the same `Apply`:

**`leave` (default).** Successful targets remain deployed. Failed targets remain unprovisioned. `deployments.status = partial_failure`. `deployment_targets[i].status` reflects each. CLI prints a colorized summary; web UI surfaces a partial-failure banner with retry / rollback buttons.

**`rollback`.** After waiting for in-flight applies to finish, the provisioner runs `tofu destroy` on every target that successfully applied within this run (NOT pre-existing successful targets — only what this run created). On rollback success: `deployments.status = failed` (the user's intent never reached steady state). On rollback failure: `deployments.status = rollback_failed` with both the original and rollback errors recorded; this requires manual intervention.

**`retry-failed`.** After all in-flight applies finish, the provisioner reruns ONLY the failed targets, up to `--max-retries` (default 1). Successes are recorded; remaining failures fall back to `leave` semantics.

The policy is selected at run time per Apply call (CLI flag or web UI). It does NOT change between Plan and Apply — the plan records the chosen policy as part of `PlanResult.PartialFailure`.

### Cross-target ordering

When `B.refs[].component = A`, A's apply must complete before B's plan runs. The orchestrator implements this by:
1. Dispatching A's targets first (parallel across A's clouds/regions, since A is one DAG node).
2. After ALL of A's targets succeed, reading their outputs via `tofu output -json`.
3. Persisting outputs to `deployment_targets.outputs` for replay.
4. Resolving `${component.A.outputs.X}` lazy refs in B's workspace by inserting `data.terraform_remote_state.A` blocks pointing at A's state.
5. Dispatching B's targets.

If ANY of A's targets fail under `leave` or `retry-failed` policy, B's targets are marked `skipped` (not failed) — the user sees that B was never attempted because of an upstream issue. Under `rollback`, B is never attempted at all; the rollback runs against A's already-applied targets.

---

## Cross-cutting: ResolvedRefs

```go
// pkg/cloud/refs.go
type ResolvedRefs map[string]any

// Keys are the user's chosen `as` aliases (or the raw output name when `as` is empty).
// Values are the actual outputs from upstream `tofu output -json`, typed per the
// component's declared output schema.
//
// Adapters consume these as opaque values; the provisioner has already done all
// type checking and stringification. Cloud adapters do NOT need to chase
// remote_state lookups — those happen in workspace JSON, not Go.
```

For the AWS adapter emitting an EC2 instance whose `subnet_id` comes from a network component:

```go
// In Adapter.Emit():
subnetID, ok := refs["subnetIds"].([]string)
if !ok || len(subnetID) == 0 {
    return nil, fmt.Errorf("adapter aws: missing required ref subnetIds")
}
attrs["subnet_id"] = subnetID[0]  // serialized by provisioner as a literal Tofu interpolation string
```

The provisioner has already inserted the `data.terraform_remote_state.web_network` block in B's workspace, so the literal string `${data.terraform_remote_state.web_network.outputs.subnet_ids[0]}` resolves correctly at Tofu time. Adapters can either return the literal interpolation string (preferred, fully visible in `tofu plan` output) or return the materialized value (post-apply only; harder to debug). v1 in-tree adapters use the interpolation form.

---

## State bridge & drift detection

### State bridge

`internal/state/bridge` reads `tofu show -json` output after every Apply and reconciles to inventory:

```go
type StateBridge interface {
    Reconcile(ctx context.Context, target ir.DeploymentTarget, stateJSON []byte) error
    Snapshot(ctx context.Context, deploymentTargetID string) (*StateSnapshot, error)
}

type StateSnapshot struct {
    DeploymentTargetID string
    TofuVersion        string
    SerialNumber       int64                 // Tofu state serial; incremented per apply
    ResourceCount      int
    Resources          []StateResource       // Tofu address + cloud-native ID + attributes-hash
    Outputs            map[string]any
    CapturedAt         time.Time
}

type StateResource struct {
    Address       string  // "aws_db_instance.orders_db"
    Type          string  // "aws_db_instance"
    Name          string  // "orders_db"
    CloudResourceID string  // ARN, GCP self-link, Azure resource ID
    AttributesHash  string  // sha256 of canonicalized attributes
}
```

`Reconcile` writes one row per resource to `tofu_resources` (new table, see §"Inventory schema additions"), upserts `deployment_targets.state_serial`, and updates `deployment_targets.state_backend` if it changed.

### Drift detection

`DetectDrift(deploymentID)` runs `tofu plan -refresh-only -out drift.bin` per target, parses the resulting plan, and produces:

```go
type DriftReport struct {
    DeploymentID string
    TargetReports []TargetDriftReport
    GeneratedAt  time.Time
}

type TargetDriftReport struct {
    DeploymentTargetID string
    HasDrift           bool
    DriftedResources   []DriftedResource
    GoneResources      []DriftedResource  // exists in state but not in cloud
    DiscoveredResources []DriftedResource // exists in cloud but not in state (rare; only via `tofu refresh`)
}

type DriftedResource struct {
    Address          string
    AttributesBefore map[string]any  // from state
    AttributesAfter  map[string]any  // from cloud (post-refresh)
    DiffSummary      string          // "instance_type changed: t3.medium -> t3.large"
}
```

Persisted to `drift_status` keyed on `deployment_target_id`. The CLI's `nimbusfab drift` command reads from this table; the web UI polls it. Drift detection does NOT auto-remediate — it surfaces a report; the user re-applies (which propagates state-as-canonical) or imports (which writes cloud-as-canonical into state).

### Reconciliation algorithm

After `tofu apply` succeeds:
1. `runner.StateShow(ws)` returns the `tofu show -json` byte stream.
2. `bridge.parse(json) -> StateSnapshot`.
3. Snapshot diffed against existing `tofu_resources` rows for the same `deployment_target_id`:
   - New address → INSERT.
   - Existing address with different `attributes_hash` → UPDATE.
   - Address present in DB but missing in snapshot → DELETE (resource was removed).
4. `runner.Output(ws)` returns the outputs map; bridge UPSERTs to `deployment_targets.outputs`.
5. `runs.status = succeeded`, `runs.finished_at = now()`.

All of step 3-5 are one transaction; partial reconcile failures roll the run back to `applied_partial` (the cloud changed but the inventory didn't fully update — surfaced clearly so the user knows next-plan diff may be misleading).

---

## Run model & inventory

### Persistence

`Plan` writes:
- `runs(kind=plan, deployment_target_id, status=in_progress, started_at)` per target.
- On per-target plan success: `runs.status = planned, finished_at, plan_file_path, json_plan_path`.
- On per-target plan failure: `runs.status = failed, finished_at, error_code, error_message`.

`Apply` writes:
- `runs(kind=apply, deployment_target_id, parent_run_id=<plan run>, status=in_progress, started_at)`.
- On per-target apply success: status `succeeded`; bridge writes `tofu_resources`; outputs written.
- On per-target apply failure: status `failed`; partial-failure policy invoked.

`Destroy` mirrors Apply with `kind=destroy`.

`run_logs` receives every `RunEvent` from the runner stream, one row per event, indexed by `(run_id, sequence)`. Server mode optionally diverts `run_logs` to object storage past N events to keep the table small.

### Inventory schema additions

This spec adds these tables / columns beyond the architecture spec's outline. Migration filename: `pkg/inventory/migrations/0003_provisioner.sql`.

```sql
-- Per-target workspace and state metadata.
ALTER TABLE deployment_targets
    ADD COLUMN workspace_dir       TEXT NOT NULL,
    ADD COLUMN workspace_hash      TEXT,                -- sha256 of canonical workspace files
    ADD COLUMN state_backend       JSONB NOT NULL,
    ADD COLUMN state_serial        BIGINT,
    ADD COLUMN outputs             JSONB,
    ADD COLUMN last_apply_run_id   UUID REFERENCES runs(id);

-- Adds plan/apply/destroy run tracking.
ALTER TABLE runs
    ADD COLUMN parent_run_id   UUID REFERENCES runs(id),  -- plan -> apply linkage
    ADD COLUMN plan_file_path  TEXT,
    ADD COLUMN json_plan_path  TEXT,
    ADD COLUMN error_code      TEXT,
    ADD COLUMN error_message   TEXT;

-- One row per resource in Tofu state, per target.
CREATE TABLE tofu_resources (
    id                     UUID PRIMARY KEY,
    org_id                 UUID NOT NULL,
    deployment_target_id   UUID NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    tofu_address           TEXT NOT NULL,            -- "aws_db_instance.orders_db"
    tofu_type              TEXT NOT NULL,
    tofu_name              TEXT NOT NULL,
    cloud_resource_id      TEXT,                     -- ARN/self-link/Azure ID; NULL until apply
    attributes_hash        TEXT NOT NULL,
    captured_at            TIMESTAMPTZ NOT NULL,
    UNIQUE (deployment_target_id, tofu_address)
);

CREATE INDEX tofu_resources_org_idx ON tofu_resources (org_id, deployment_target_id);
CREATE INDEX tofu_resources_cloud_id_idx ON tofu_resources (cloud_resource_id) WHERE cloud_resource_id IS NOT NULL;

-- Drift detection persisted state.
CREATE TABLE drift_status (
    deployment_target_id   UUID PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    org_id                 UUID NOT NULL,
    has_drift              BOOLEAN NOT NULL,
    last_checked_at        TIMESTAMPTZ NOT NULL,
    drifted_count          INT NOT NULL DEFAULT 0,
    gone_count             INT NOT NULL DEFAULT 0,
    discovered_count       INT NOT NULL DEFAULT 0,
    report_json            JSONB
);
```

The `tofu_resources` table is the join key for cost actuals: `cost_actuals.resource_id` matches `tofu_resources.cloud_resource_id`.

---

## Error model

### Codes

| Code | Origin | Meaning |
|---|---|---|
| `ErrAdapterUnknown` | provisioner | `target.cloud` not in registry |
| `ErrAdapterRefuses` | adapter | `Validate()` returned a fatal issue |
| `ErrAdapterEmit` | adapter | `Emit()` returned an error (wrapped) |
| `ErrCloudNotSupported` | provisioner | adapter doesn't support `component.Type` |
| `ErrCredsResolve` | provisioner | secrets backend cannot resolve `credentialRef` |
| `ErrStateBackendResolve` | provisioner | none of stack/target/adapter defaults produced a usable backend |
| `ErrWorkspaceWrite` | provisioner | filesystem failure writing workspace files |
| `ErrWorkspaceLocked` | provisioner | another process holds `.lock` (see remediation) |
| `ErrTofuVersionMismatch` | runner | installed `tofu` doesn't satisfy `versions.tf.json` |
| `ErrTofuStateLocked` | runner | state lock held; remediation: wait, or `nimbusfab state unlock --target=...` |
| `ErrTofuCredsMissing` | runner | env credentials not picked up by provider |
| `ErrTofuProviderMissing` | runner | provider plugin not installed |
| `ErrTofuDiagnostic` | runner | structured diagnostic; carries `Diagnostic` |
| `ErrTofuOpaque` | runner | non-zero exit with no parseable diagnostics |
| `ErrPartialFailure` | provisioner | some targets succeeded, others failed; carries per-target results |
| `ErrRollbackFailed` | provisioner | rollback policy ran and itself failed; manual intervention required |
| `ErrComposedNotSupported` | provisioner | `Component.Mode == "composed"`; v1 only supports replicate |
| `ErrUpstreamSkipped` | provisioner | a target was skipped because an upstream component failed |
| `ErrDriftRefresh` | bridge | `tofu plan -refresh-only` failed; drift report unavailable |

All errors implement `UserFacing() (code, message)` per architecture §10. The web app and CLI render the `code + message + remediation` triplet uniformly.

### Remediation messages

Each error carries a `Remediation` string aimed at the user, not the developer:

```
ErrTofuStateLocked:
  "OpenTofu state for orders-db (aws/us-east-1) is locked by another process.
   If a previous run was interrupted, force-unlock with:
       nimbusfab state unlock --deployment <id> --target orders-db --cloud aws
   Otherwise wait for the in-flight operation to complete."
```

Remediation messages are part of the error code stable contract — they appear in user docs, support transcripts, and automated diagnostics.

---

## Plugin contract test suite additions

`pkg/plugin/contract/adapter_suite.go` (existing) gains these test cases that EVERY adapter (in-tree and out-of-tree) must pass:

```go
func RunAdapterSuite(t *testing.T, a cloud.Adapter) {
    t.Run("name_is_stable",                NameIsStable)
    t.Run("supports_at_least_one_apiver",  SupportsAtLeastOneAPIVersion)
    t.Run("supports_at_least_one_type",    SupportsAtLeastOneComponentType)
    t.Run("tier_one_schema_is_valid_json", TierOneSchemaIsValidJSON)
    t.Run("emit_is_pure",                  EmitIsPure)            // run twice; assert byte equal
    t.Run("emit_emits_required_tags",      EmitEmitsRequiredTags) // post-provisioner-tag injection
    t.Run("emit_handles_lazy_refs",        EmitHandlesLazyRefs)   // refs map drives subnet_id, etc.
    t.Run("profile_satisfies_contract",    ProfileSatisfiesContract) // parity spec dep
    t.Run("pricing_key_round_trip",        PricingKeyRoundTrip)   // adapter -> JSON -> adapter
    t.Run("billing_query_constructible",   BillingQueryConstructible)
    t.Run("default_state_backend_kind",    DefaultStateBackendKind) // kind matches adapter.Name() family
    t.Run("provider_block_minimal",        ProviderBlockMinimal)  // no nil values; no plaintext secrets
}
```

The fake adapter shipped with the provisioner package passes the entire suite; in-tree AWS / Azure / GCP adapters all run it in CI.

---

## CLI integration

```
nimbusfab plan [stack]                       # Engine.Plan; prints per-target plan diffs + cost + parity
nimbusfab apply [stack]                      # Engine.Plan + Engine.Apply (synchronous; --detach for async)
nimbusfab up [stack]                         # plan+apply with -auto-approve guard
nimbusfab destroy [stack]                    # Engine.Destroy
nimbusfab drift [deployment-id]              # Engine.DetectDrift; prints DriftReport
nimbusfab import <component> <cloud-id>      # Engine.Import; v1 takes explicit address mapping
nimbusfab state show [target]                # tofu show -json scoped to a deployment_target
nimbusfab state list [target]                # list all addresses
nimbusfab state rm <address>                 # tofu state rm; recorded in audit_log
nimbusfab state mv <from> <to>               # tofu state mv; recorded in audit_log
nimbusfab state unlock                       # forced unlock; requires --confirm
```

### Plan output format

```
$ nimbusfab plan --stack prod
Validating... ✓
Resolving credentials... ✓
Planning 3 targets across 2 clouds (parallel)...

  ✓ web-network        aws/us-east-1     (+3, ~0, -0)
  ✓ web-network        gcp/us-central1   (+2, ~0, -0)
  ⚠ orders-db          aws/us-east-1     (+1, ~1, -0)  password change requires apply

Plan summary:
  Targets:    3 planned, 0 failed, 0 skipped
  Resources:  +6, ~1, -0
  Workspace:  /var/run/nimbusfab/<deployment-id>/
  Plan file:  saved (run `nimbusfab apply` to deploy)

Cost:    $1,247/mo estimated      (see `nimbusfab cost estimate`)
Parity:  score 0.94                (see `nimbusfab parity`)
```

### Apply progress format

Streams per-target progress via the `RunEvent` channel; renders one line per resource action:

```
$ nimbusfab apply --stack prod
Applying 3 targets across 2 clouds (parallel, leave-on-failure)...

  ▶ web-network        aws/us-east-1     creating aws_vpc.main ...
  ▶ web-network        gcp/us-central1   creating google_compute_network.main ...
  ✓ web-network        gcp/us-central1   complete (2 created, 0 changed, 0 destroyed) [12s]
  ✓ web-network        aws/us-east-1     complete (3 created, 0 changed, 0 destroyed) [18s]
  ▶ orders-db          aws/us-east-1     creating aws_db_subnet_group.orders_db ...
  ▶ orders-db          aws/us-east-1     creating aws_db_instance.orders_db ... (this can take 5-10min)
  ✓ orders-db          aws/us-east-1     complete (1 created, 1 changed, 0 destroyed) [6m32s]

Apply complete:
  Targets:    3 succeeded, 0 failed
  Total time: 6m50s
  Outputs:    web-network.vpc_id = vpc-0abc..., web-network.subnet_ids = [subnet-1, subnet-2, ...]
```

### Flags

- `--partial-failure {leave|rollback|retry-failed}` — orchestration policy.
- `--max-concurrent` — cap parallelism (default `NumCPU()`).
- `--no-refresh` — skip Tofu refresh on plan (faster, may miss drift).
- `--target <component>` — restrict to one component (and its dependencies).
- `--target <component>/<cloud>/<region>` — restrict to one deployment_target.
- `--auto-approve` — apply without confirmation (always implicit in CI; required explicit interactively).
- `--detach` — return as soon as the run is queued; check status with `nimbusfab runs status <id>`.

---

## API surface

- `POST /api/v1/projects/{id}/stacks/{stack}/plan` → starts a plan; returns `{deployment_id, plan_run_ids[]}`. Idempotency-Key required.
- `GET  /api/v1/deployments/{id}/plan` → consolidated `PlanResult`.
- `POST /api/v1/deployments/{id}/apply` → starts apply against a previously-planned deployment; returns `{apply_run_ids[]}`.
- `POST /api/v1/deployments/{id}/destroy` → starts destroy; returns `{destroy_run_ids[]}`.
- `GET  /api/v1/runs/{id}` → run record + final status.
- `GET  /api/v1/runs/{id}/events` → SSE stream of `RunEvent`s; reconnect via `Last-Event-ID`.
- `GET  /api/v1/runs/{id}/logs` → archived log stream (post-completion).
- `GET  /api/v1/deployments/{id}/state/{target_id}` → current state snapshot (proxied through `tofu show -json`).
- `POST /api/v1/deployments/{id}/drift` → triggers drift detection; returns `{drift_run_ids[]}`.
- `POST /api/v1/deployments/{id}/state/unlock` → forced unlock (audit-logged).

All endpoints `org_id`-scoped via the session/PAT; bodies validated against generated JSON Schemas.

---

## Determinism guarantees

- For the same `(IR, adapter version, framework version)`, `Adapter.Emit()` returns byte-identical primitives.
- For the same set of primitives, `provisioner.serialize()` writes byte-identical workspace files.
- For the same workspace files, `tofu init && tofu plan` produces a byte-identical `tofu show -json` plan output (modulo timestamps in metadata, which we strip before comparison).
- For the same plan, `tofu apply` is non-deterministic in cloud-side outcomes, but the inventory write-back (`tofu_resources` rows) is keyed on cloud-stable identifiers (ARNs, GCP self-links) so reapply produces the same row-level diff.

CI asserts each level of this chain. A failing determinism assertion blocks the commit — non-determinism is treated as a bug.

---

## Security boundaries

- Credentials never written to disk; flow only through `Workspace.Environment` to the subprocess.
- State encryption enabled by default; passphrase via `secrets.Backend`.
- `provider.tf.json` MUST NOT contain plaintext secrets; the contract test asserts this by scanning emitted blocks.
- `nimbusfab state rm` and `nimbusfab state unlock` require `--confirm` flag and write `audit_log` entries.
- Workspace files have mode `0600`; workspace directory `0700`. Provisioner enforces on creation.
- Multi-tenant safety: every workspace path includes `<org_id>` so two orgs sharing a host can't see each other's files; container deployments isolate further.

---

## Verification

This is an architecture-level spec, not an implementation. Verify by:

1. **Adapter walkthrough.** For each of AWS / Azure / GCP, hand-draft the `Emit()` for a `database` component and a `network` component. Confirm every required attribute can be produced from `(target, refs)` alone, with no hidden state. If any cloud needs out-of-band data, the adapter contract is wrong — fix before implementing.
2. **Workspace file walkthrough.** For a 2-component, 2-cloud project, hand-write what each of the four workspace files would look like for each of the four resulting targets. Confirm they pass `tofu validate` mentally.
3. **Refs DAG walkthrough.** Construct a 4-component project where B depends on A, C depends on B, D depends on A. Confirm the orchestrator produces the right execution order (`A → {B, D in parallel} → C`) and that target-level fan-out is correct within each component.
4. **Partial-failure walkthrough.** A 2-cloud component where AWS succeeds and GCP fails. Trace the inventory rows for each policy (`leave`, `rollback`, `retry-failed`) — confirm the user can recover via the documented commands, and that the next `nimbusfab plan` correctly reflects current state.
5. **Determinism walkthrough.** Construct an IR fragment whose Emit involves two map keys (`tags`, `ingress_rules`). Confirm the serialization rules guarantee byte-identical output regardless of map iteration order.
6. **Lazy-ref walkthrough.** A `${component.X.outputs.subnet_ids}` reference. Trace from validator (kept as `LazyRef`) → provisioner (workspace gets `data.terraform_remote_state.X` block) → adapter (sees `refs["subnetIds"] = []string{...}`) → emitted Tofu JSON (literal interpolation string). Confirm each handoff is well-typed.
7. **State-bridge walkthrough.** A target whose Tofu state went from 5 resources to 4 (one was destroyed). Confirm `tofu_resources` ends up with 4 rows and 1 deletion; confirm `cost_actuals` joins still work for the 4 remaining via `cloud_resource_id`.
8. **Concurrency walkthrough.** A project with 20 targets across 3 clouds and 2 credentials. Confirm the three-semaphore acquisition (global → cloud → credential) caps as expected and never deadlocks (lock order is fixed).
9. **Tofu version-drift walkthrough.** User has `tofu 1.6.0` installed; project requires `>= 1.7.0`. Confirm `runner.Version()` is checked once at startup; confirm the error surfaces as `ErrTofuVersionMismatch` with a remediation pointing at the install docs.
10. **Cost-spec alignment.** When the Cost Estimator spec is written, confirm `PricingKey()`'s shape is sufficient as input to its pricing-cache lookups. If not, either the cost spec adapts or `PricingKey()` returns more — but the question must be asked.

---

## Future hooks (out of scope; design notes)

- **Composed mode (`Component.Mode == "composed"`).** Reserved in the IR. v2 will introduce a CrossCloudOrchestrator that treats deployment_targets as a single DAG with cross-cloud `depends_on`. The current per-target workspace model is the building block; composed mode adds a synthesis step that collects multi-cloud primitives into a single workspace per cross-cloud Component.
- **Out-of-tree adapters via gRPC.** The Adapter interface in this spec is the source for the `.proto` definition. Reserve method numbers; never reuse an old method number even after deprecation.
- **`tofu apply --replace=<address>` support.** Reserve a `Replace []string` field on `ApplyOpts` for forced recreation; v1 doesn't surface it in the CLI but the runner pipes it through.
- **GitOps daemon.** Reserve a `policy.driftReconciliation: auto` field on Component. v1 stores it; the daemon (v2) acts on it. Drift detection in this spec already produces the report the daemon will consume.
- **Workspace caching across runs.** `.terraform/` is reused across plan/apply for the same target (skip `tofu init`). Reserve a `workspace.cache_max_age_hours` config; v1 hardcodes 168h (1 week).
- **Multi-statefile per target.** Some users want per-component-cluster state aggregation. Out of scope; the current "one statefile per `(deployment_target_id)`" rule is fixed for v1.

---

## Implementation phasing (non-binding)

This spec is large; implementation will arrive in phases, each its own plan document under `docs/superpowers/plans/`. Suggested staging:

1. **Phase 1 — Tofu runner + workspace + minimal AWS adapter for `network`.** End-to-end: `nimbusfab plan` produces a real `aws_vpc` + subnet plan against LocalStack; `nimbusfab apply` actually creates them. Forms the substrate everything else builds on.
2. **Phase 2 — Provisioner orchestration + state bridge + drift detection.** Multi-target parallel apply, partial failure policies, drift detection. Requires Phase 1.
3. **Phase 3 — AWS adapter expansion to `database`, `compute`, `storage`.** Completes AWS coverage of the v1 component types. PricingKey + Profile + BillingQuery shapes locked in.
4. **Phase 4 — Azure adapter (all four types).** Full parity with AWS for v1 component set.
5. **Phase 5 — GCP adapter (all four types).** Full parity with AWS for v1 component set.

Phases 4 and 5 can run in parallel after Phase 3 if multiple contributors exist. Cost estimator spec slots in after Phase 3 (when `PricingKey()` has at least one real adapter to validate against). Web app spec slots in after Phase 2 (when the SSE stream has real data).
