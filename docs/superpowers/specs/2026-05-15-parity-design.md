# Parity & SKU Normalization Spec

**Status:** Subsystem spec. Defines the design for cross-cloud parity reporting and SKU normalization. Slots into the architecture spec's follow-up list at position 3 (after the Provisioner spec, before the Cost Estimator spec).

**Date:** 2026-05-15
**Depends on:** `docs/superpowers/specs/2026-05-14-architecture-design.md`
**Depended on by:** The forthcoming Cost Estimator spec (shares `Profile()` infrastructure) and Cost Dashboard spec (parity score is a dashboard widget).

---

## Context

When a user declares one component (`type: database`, `size: small`) and targets multiple clouds (`target: [aws, gcp, azure]`), each cloud adapter picks a cloud-specific SKU. Today those picks are uncoordinated: AWS might pick `db.t3.medium` (2 vCPU, 4 GB RAM), GCP might pick `db-custom-2-7680` (2 vCPU, 7.5 GB RAM), Azure might pick `GP_Std_D2s_v3` (2 vCPU, 8 GB RAM). The deployed resources diverge in capacity, cost, and feature support, and the user has no visibility into how similar they actually are.

This spec defines the **parity subsystem**: a normalization + reporting layer that gives users clear, structured information about how similar their multi-cloud resources actually are, and lets them optionally declare rules that gate deploys when divergence exceeds policy.

**Design principles:**
1. **Informative by default.** Every plan/apply produces a parity report; nothing blocks unless the user opts in.
2. **Predictable contracts.** Framework-owned, versioned contract floors define the minimum each abstract size guarantees. Adapters pick the cheapest-fitting cloud SKU.
3. **Flexible policy.** Users opt into gating via declarative rules — per project, per component, per attribute.
4. **Shared infrastructure with cost.** The same `Profile()` data feeds parity and cost estimation; we build it once.

---

## Scope

**In scope (this spec):**
- New `pkg/parity` module with engine, score function, rule evaluator, reporter
- New `Profile(primitive) -> ResourceProfile` method on `cloud.Adapter`
- Contract floor format and embedded catalog
- T-shirt size resolution to explicit dimensions
- User parity rules YAML grammar and evaluation semantics
- CLI command `nimbusfab parity` + integration with `plan` output
- JSON output format
- REST API endpoints (`/api/v1/parity/...`)

**Out of scope (deferred):**
- Auto-balancing (framework actively upgrading SKUs to match other clouds' attributes) — design hooks reserved; v2.
- Cross-cloud "equivalent region" mapping — v1 trusts user-declared regions.
- Historical parity tracking over time — v2 dashboard widget.
- Per-attribute weight tuning by users — v2.
- Multi-region single-cloud parity ("same component deployed to AWS us-east-1 and AWS eu-west-1") — covered transparently because each is a separate `DeploymentTarget`; no special handling.

---

## Module layout

```
pkg/parity/
  engine.go         # Compare(component, []TargetProfile) -> ParityReport
  score.go          # WeightedScore(report) -> float64
  rules.go          # Evaluate(report, ProjectRules) -> []Violation
  reporter.go       # Render(report, format) -> []byte
  contracts.go      # Load embedded contracts; resolve T-shirt -> dimensions
  contracts/        # embedded via go:embed
    database.yaml
    compute.yaml
    storage.yaml
    network.yaml
  types.go          # public types: ParityReport, Violation, ResourceProfile, ...
```

`pkg/parity` is the only place that compares profiles. Adapters only produce them. The engine and CLI consume the package; subsystems do not import each other.

---

## Public surface

### Adapter contract addition

```go
// pkg/cloud/adapter.go
type Adapter interface {
    // ...existing methods...

    // Profile returns the normalized resource attributes of the chosen
    // cloud SKU for the given primitive. The framework computes parity
    // and cost both off of this data. Pure: no network, no globals.
    Profile(ctx context.Context, p ir.ResourcePrimitive) (ResourceProfile, error)
}

type ResourceProfile struct {
    Class    string            // "compute" | "storage" | "database" | "network"
    Compute  *ComputeProfile
    Storage  *StorageProfile
    Database *DatabaseProfile
    Network  *NetworkProfile
    Features map[string]bool   // e.g. {"pointInTimeRestore": true, "encryptionAtRest": true}
    SKU      string            // human-readable: "db.t3.medium"
    Notes    []string          // adapter-supplied caveats
}

type ComputeProfile  struct {
    VCPU         int
    MemoryGB     float64
    Architecture string   // "x86_64" | "arm64"
    NetworkGbps  float64
}
type StorageProfile  struct {
    SizeGB         int
    IOPS           int
    ThroughputMBps int
    Class          string   // "ssd" | "hdd" | "nvme" | "tiered"
    Encrypted      bool
}
type DatabaseProfile struct {
    Engine   string         // "postgres" | "mysql" | "mariadb" | ...
    Version  string
    Compute  ComputeProfile
    Storage  StorageProfile
    Replicas int
    HA       bool
}
type NetworkProfile  struct {
    CIDR          string
    BandwidthGbps float64
    IPv6          bool
    NAT           bool
}
```

`Profile` is a contract reservation: adapters that don't yet support it return `(ResourceProfile{}, ErrProfileUnavailable)` and the parity engine reports the attribute as unknown rather than failing.

### Parity engine

```go
// pkg/parity/types.go
type ParityReport struct {
    Component    string
    Type         string          // component type: "database" | ...
    Size         string          // "small" | "medium" | "large" | "xlarge" | "custom"
    Contract     ContractFloor   // resolved floor used for this report
    Targets      []TargetProfile
    Comparisons  []AttrComparison
    Score        float64         // 0..1, weighted
    Warnings     []string
    GeneratedAt  time.Time
}

type TargetProfile struct {
    DeploymentTargetID string
    Cloud              string
    Region             string
    Profile            ResourceProfile
}

type AttrComparison struct {
    Attribute    string          // "compute.vCPU", "features.pointInTimeRestore", ...
    Kind         string          // "exact" | "numeric" | "boolean"
    Values       map[string]any  // keyed by cloud
    AllMatch     bool
    MinValue     any
    MaxValue     any
    Score        float64         // 0..1
}

type Violation struct {
    Component string
    Attribute string
    Policy    string             // "exact" | "maxRatio" | "requireAll" | "minScore"
    Detail    string             // human-readable explanation
    Action    string             // "warn" | "block"
}

type ProjectRules struct {
    Default    ModeRules                   // applied when no per-component rule
    Components map[string]ComponentRules   // keyed by Component.Name
}

type ModeRules struct {
    Mode     string  // "warn" | "block" | "off"
    MinScore float64
}

type ComponentRules struct {
    Mode       string                      // "warn" | "block" | "off"
    MinScore   float64
    Attributes map[string]AttributePolicy  // keyed by dotted attribute path
}

type AttributePolicy struct {
    Policy   string  // "exact" | "maxRatio" | "requireAll"
    MaxRatio float64 // used when Policy == "maxRatio"
}

// Engine API
type Engine interface {
    Compare(ctx context.Context, in CompareInput) (*ParityReport, error)
    EvaluateRules(ctx context.Context, report *ParityReport, rules ProjectRules) ([]Violation, error)
}

type CompareInput struct {
    Component  ir.Component
    Profiles   []TargetProfile          // one per DeploymentTarget
    Contract   ContractFloor            // resolved from component type + size
}
```

---

## Contract floors

### Format

Contract floors live in `pkg/parity/contracts/*.yaml`, embedded into the binary via `//go:embed`. They are framework-owned, versioned with the framework release, and **NOT user-overridable**. A user who needs different minimums uses **explicit dimensions** instead (see §"Sizing surface").

```yaml
# pkg/parity/contracts/database.yaml
apiVersion: parity.dev/v1alpha1
kind: ComponentContract
type: database
description: |
  Minimum guarantees for the `database` component type across all
  supported clouds. Adapters select the cheapest cloud SKU that
  satisfies these minimums.
sizes:
  small:
    compute:  { minVCPU: 2,  minMemoryGB: 4 }
    storage:  { minSizeGB: 100, minIOPS: 1000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
  medium:
    compute:  { minVCPU: 4,  minMemoryGB: 16 }
    storage:  { minSizeGB: 250, minIOPS: 3000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
  large:
    compute:  { minVCPU: 8,  minMemoryGB: 32 }
    storage:  { minSizeGB: 500, minIOPS: 5000, classIn: [ssd] }
    features: { pointInTimeRestore: required, multiAZ: required }
  xlarge:
    compute:  { minVCPU: 16, minMemoryGB: 64 }
    storage:  { minSizeGB: 1000, minIOPS: 10000, classIn: [ssd] }
    features: { pointInTimeRestore: required, multiAZ: required }
```

### Authoring rules

- All four T-shirt sizes (`small` through `xlarge`) MUST be defined for every component type.
- Minimums MUST be satisfiable on every supported cloud's standard SKU catalog — i.e., contracts cannot demand features unique to a single cloud.
- Minimums are versioned: a new framework minor release MAY raise a minimum; the release notes call this out. Major releases MAY change them more freely; existing deployments are unaffected (state is canonical) until next plan.
- Adapters declare which contracts they support via `SupportedComponentTypes() []string`; missing types fail validation.

### Loading

`contracts.Load(componentType, size) -> ContractFloor` reads the embedded YAML at startup, caches parsed contracts, and resolves a `(type, size)` tuple to a `ContractFloor` Go value.

---

## Sizing surface

The DSL accepts either a T-shirt size or explicit dimensions. **Mutually exclusive per component.**

### T-shirt size

```yaml
components:
  - name: orders-db
    type: database
    spec:
      engine: postgres
      version: "16"
      size: small        # resolved via database contract
```

Resolution: `contracts.Load("database", "small")` returns the floor; that floor is what adapters see and what parity reports use as the baseline.

### Explicit dimensions

```yaml
components:
  - name: analytics-db
    type: database
    spec:
      engine: postgres
      version: "16"
      compute:
        vCPU: 8
        memoryGB: 32
      storage:
        sizeGB: 500
        iops: 3000
        class: ssd
      features:
        pointInTimeRestore: true
        multiAZ: true
```

Resolution: the explicit dimensions become a synthetic `ContractFloor`. Parity reports against custom floors are labeled `Size: "custom"` in output.

### Conflict rules

- `size:` AND `compute:`/`storage:` set together → validation error: pick one.
- `size:` set with a value not in the contract → validation error.
- Explicit dimensions missing required attributes for the type → validation error with the missing fields listed.

---

## Parity score function

For each `AttrComparison` across N targets:

- **Exact attributes** (`compute.vCPU`, `database.engine`, `database.version`, `storage.class`):
  - All targets equal → `Score = 1.0`
  - Any mismatch → `Score = 0.0`
- **Numeric attributes** (`compute.memoryGB`, `storage.sizeGB`, `storage.iops`, `storage.throughputMBps`, `compute.networkGbps`):
  - `Score = 1 - (max - min) / max`, clamped to `[0, 1]`
  - If `max == 0`, `Score = 1.0`
- **Boolean features** (`features.*`):
  - All targets equal → `Score = 1.0`
  - Otherwise → `Score = 0.0`

**Overall score** = weighted mean. Default weights (NOT user-tunable in v1):

| Group | Weight |
|---|---|
| `compute.vCPU` | 0.20 |
| `compute.memoryGB` | 0.15 |
| `compute.architecture` | 0.05 |
| `storage.sizeGB` | 0.10 |
| `storage.iops` | 0.10 |
| `storage.class` | 0.05 |
| `features.*` (averaged across declared features) | 0.20 |
| `database.engine` + `database.version` | 0.15 |

Attributes not applicable to a class (e.g. `database.*` for a `compute` component) are omitted; remaining weights renormalize.

**Reasoning for fixed weights:** user-tunable weights would make scores incomparable across projects, defeating their use as a shared signal. Users who want different policies use rules (see below), not weight tuning.

---

## User parity rules

Rules are **opt-in**. A project with no rules file gets default informative behavior: every plan emits a parity report, nothing blocks.

### Rules file

```yaml
# parity.yaml  (or `parity:` block in project.yaml)
apiVersion: parity.dev/v1alpha1
kind: ProjectParityRules
parity:
  default:
    mode: warn               # warn | block | off; default: warn if rules file exists
    minScore: 0.7
  components:
    orders-db:
      mode: block
      minScore: 0.9
      attributes:
        compute.vCPU:
          policy: exact
        compute.memoryGB:
          policy: maxRatio
          maxRatio: 2.0
        features.pointInTimeRestore:
          policy: requireAll
    analytics-warehouse:
      mode: off              # divergence accepted by design
```

### Evaluation semantics

- `mode: off` → no rules evaluated for this component; report is still produced for visibility.
- `mode: warn` → violations become `Violation{Action: "warn"}`; plan/apply proceeds; warnings rendered in output.
- `mode: block` → violations become `Violation{Action: "block"}`; `apply` fails (plan succeeds with the warning) until either: divergence is fixed, the rule is loosened, or the user passes `--allow-parity-violations` on the command line.

### Per-attribute policies

- **`exact`**: the attribute value must match across all targets. Mismatch → violation.
- **`maxRatio: N`** (numeric attributes only): `max(values) / min(values)` must not exceed `N`. Exceeds → violation.
- **`requireAll`** (boolean features only): every target must have the feature `true`. Any `false` → violation.
- **`minScore: F`** (component-level): overall parity score for this component must be `>= F`. Below → violation.

`exact` on a numeric is allowed but discouraged (most cloud SKUs differ in fine numeric attributes); `maxRatio` is the realistic choice.

### CLI override

```
nimbusfab apply --allow-parity-violations    # ignore block actions, deploy anyway
nimbusfab apply --strict-parity              # treat all warn-mode violations as block
```

---

## Engine integration & data flow

### During `plan`

1. **Validation phase resolves sizing** — T-shirt → contract floor; explicit dimensions → synthetic floor. Validator rejects conflicts.
2. **Provisioner emits primitives** — for each `DeploymentTarget`, the cloud adapter picks the cheapest SKU satisfying the floor, returns `[]ResourcePrimitive`.
3. **Provisioner profiles primitives** — for each emitted primitive, `Adapter.Profile()` returns the actual normalized attributes.
4. **Parity engine compares** — `parity.Engine.Compare()` builds a `ParityReport` per Component (across its targets).
5. **Rules engine evaluates** — if a `parity.yaml` exists, `parity.Engine.EvaluateRules()` returns `[]Violation`.
6. **PlanResult includes both** — `PlanResult.ParityReports []*ParityReport` and `PlanResult.ParityViolations []Violation`.

### During `apply`

1. Apply reuses the plan's `ParityReports` (already computed; no re-profiling unless `--refresh-parity` is passed).
2. If any `Violation.Action == "block"` and `--allow-parity-violations` is not set, apply fails with a structured error code `ErrParityBlocked` and a remediation message.
3. Otherwise apply proceeds; the parity report is persisted in the inventory's `runs.parity_report_json` column for audit.

### During `nimbusfab parity` (on-demand)

1. CLI loads the most recent `runs.parity_report_json` for the requested component+stack.
2. If `--refresh` is passed, re-runs the profile step against the current plan (or against the current deployed state via `Adapter.Profile()` of state-derived primitives).
3. Renders to terminal or JSON.

---

## CLI surface

### `nimbusfab plan` integration

```
$ nimbusfab plan --stack prod
Planning... ✓
Cost estimate: $1,247/mo (see `nimbusfab cost estimate`)

Parity:
  ✓ web-network         score 1.00  (matches across aws, gcp)
  ⚠ orders-db           score 0.94  (RAM diverges 4 → 7.5 → 8 GB)
  ✗ analytics-warehouse score 0.62  (vCPU diverges, multiAZ missing on gcp)
    Rule violation: minScore 0.85 not met (block)

Plan succeeded. `apply` will fail due to 1 blocking parity violation.
Use --allow-parity-violations to override, or fix the divergence.
```

### `nimbusfab parity [component]`

```
$ nimbusfab parity orders-db --stack prod
Component: orders-db  (database, postgres 16, size=small)
Contract floor: vCPU≥2, RAM≥4GB, storage≥100GB SSD, PITR required

                 AWS              GCP                Azure
  SKU            db.t3.medium     db-custom-2-7680   GP_Std_D2s_v3
  vCPU             2  ✓              2  ✓               2  ✓
  RAM (GB)         4  ✓              7.5 +exceed        8 +exceed
  Storage (GB)   100  ✓            100  ✓             100  ✓
  IOPS          3000  ✓           3000  ✓            3000  ✓
  PITR           yes  ✓            yes  ✓             yes  ✓
  multiAZ        no   ✓             no  ✓              no  ✓

Parity score: 0.94  (RAM diverges above floor)
Rule violations: none
```

### Flags

- `--json` — emit `ParityReport` as JSON.
- `--refresh` — re-profile current plan rather than reading from inventory.
- `--all` — emit reports for every component in the stack.

---

## REST API surface

- `GET /api/v1/projects/{id}/stacks/{stack}/parity` → all `ParityReport`s for the latest plan.
- `GET /api/v1/projects/{id}/stacks/{stack}/parity/{component}` → single component.
- `GET /api/v1/runs/{id}/parity` → reports as of a specific run.
- `POST /api/v1/projects/{id}/stacks/{stack}/parity/refresh` → re-profile against the latest plan.

All endpoints require the same session/PAT auth as the rest of the API and scope by `org_id`.

---

## Inventory schema addendum

This spec adds one column to the existing `runs` table:

```sql
ALTER TABLE runs ADD COLUMN parity_report_json JSONB;
```

Stores the `ParityReport` for the apply run; read by `nimbusfab parity` when surfacing historical results. Migration filename: `pkg/inventory/migrations/0002_parity.sql`. No other inventory changes are required for v1; `parity_history` (time-series) is a v2 addition.

---

## Error handling

| Code | Where | When | Severity |
|---|---|---|---|
| `ErrSizeConflict` | validator | `size:` and explicit dims both set | error (blocks plan) |
| `ErrUnknownSize` | validator | `size:` value not in contract | error |
| `ErrMissingDimension` | validator | explicit dims missing required attribute | error |
| `ErrProfileUnavailable` | parity engine | adapter cannot profile a primitive | warning (attribute shown as `?`) |
| `ErrParityBlocked` | engine.Apply | block-mode violation | error (fails apply) |
| `ErrContractCorrupt` | startup | embedded contract YAML fails parse | fatal at startup |

`ErrProfileUnavailable` is intentionally non-fatal — partial profile data is better than no report. Parity score is calculated over available attributes only; a warning notes "RAM unknown for Azure" rather than failing the plan.

---

## Testing strategy

- **Unit.** `pkg/parity/score` has table-driven tests with hand-built `[]AttrComparison` inputs and expected scores. `pkg/parity/rules` has tests for each policy on each kind of attribute.
- **Contract tests.** `pkg/plugin/contract` adapter suite gains `Profile_returns_valid_shape_for_each_sample` and `Profile_satisfies_declared_size_contract`. Every adapter (AWS, GCP, Azure) MUST pass.
- **Golden file tests.** For a canonical mini-project, `nimbusfab parity --json` output is compared against a checked-in golden file. Changes to scoring or rules surface immediately.
- **Integration.** Engine + fake adapters where AWS returns 4 GB RAM and GCP returns 8 GB RAM — assert score, violations, plan/apply behavior with and without rules.
- **CLI smoke tests.** Tab-separated regex on rendered output to catch alignment regressions.

---

## Future hooks (out of scope; design notes)

- **Auto-balancing.** Reserve a per-target spec field `parityPolicy: { autoUpgrade: true }`. v1 ignores it; v2 lets adapters upgrade SKUs to maximize parity score (with cost ceiling).
- **Cross-region equivalence.** Reserve a `regionEquivalence:` config on the project. v1 trusts user-declared regions; v2 may suggest "closest equivalent region" when targets cross multi-region boundaries.
- **Historical parity dashboard.** `cost_actuals` table already records time-series data per resource; add `parity_history` table keyed by `(deployment_target_id, observed_at)` storing past `ResourceProfile`s. v2.
- **Per-attribute weight tuning by org.** Add a per-org `parity_weights` table. v2 only — keep v1 scores comparable across users.

---

## Verification

This is an architecture-level spec for the parity subsystem, not an implementation. Verify the design by:

1. **Adapter walkthrough.** For each of AWS, GCP, Azure, hand-draft a `Profile()` implementation for a database primitive. If any cloud genuinely cannot supply one of the declared `ResourceProfile` fields, the field is wrong — fix the design.
2. **Contract floor sanity.** Verify each cloud's standard SKU catalog has at least one option satisfying every T-shirt floor for every component type. Adjust floors downward if some cloud lacks a fit.
3. **Score function shape.** Construct three test cases: (a) all targets identical → score 1.0; (b) one numeric attribute diverges 1:2 ratio → expected score reflects only that attribute's weight; (c) a feature mismatch zeros out the feature group. Walk through the math by hand.
4. **Rules grammar.** Write a 50-line `parity.yaml` exercising every policy (`exact`, `maxRatio`, `requireAll`, `minScore`) and every mode (`off`, `warn`, `block`). Confirm the schema parses and the semantics are unambiguous.
5. **CLI output mockup.** Eyeball-check the rendered tables for readability with 2, 3, and 4 targets, with and without violations, with and without `--json`.
6. **Cost subsystem alignment.** When the Cost Estimator spec is written, confirm its pricing-key extraction can be served by the same `Profile()` data the parity engine consumes. If not, either the cost spec changes or `Profile()` gains fields — either is fine, but the question must be asked.