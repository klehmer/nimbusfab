# DSL & IR Subsystem Spec

**Status:** Subsystem spec. Defines the user-facing YAML grammar, the loader, the validator, the interpolation engine, JSON Schema generation, and how user YAML maps to the IR types defined in `pkg/ir`.

**Date:** 2026-05-15
**Depends on:** `docs/superpowers/specs/2026-05-14-architecture-design.md`, `docs/superpowers/specs/2026-05-15-parity-design.md`
**Depended on by:** Provisioner spec (consumes validated IR), every per-cloud adapter (consumes IR component specs), web app (consumes validation reports and rendered schemas).

---

## Context

The architecture spec locked in YAML + JSON Schema as the user-facing format and pinned the IR shape (`Project / Stack / Component / DeploymentTarget / ResourcePrimitive / Composition`). The parity spec locked in T-shirt sizes (`small`/`medium`/`large`/`xlarge`) plus explicit `compute:` / `storage:` dimensions as alternative sizing surfaces, the two-tier escape hatch (typed per-cloud overrides + `raw:` passthrough), and the `Profile()` method on `cloud.Adapter`.

This spec fills in the concrete user-facing grammar that produces those IR objects, and the loader/validator/interpolation behavior that turns YAML on disk into a validated `*ir.Project` ready for the provisioner.

**Design principles:**
1. **No magic.** Every per-stack variation is named and traceable. Variables are explicit; references are scoped; composition expansion is snapshot-per-deployment.
2. **Strong validation, helpful errors.** Multiple diagnostics per pass; every error has a stable code, a dotted IR path, and a file:line source where available.
3. **One way to do common things.** Two ways to size a component (T-shirt OR explicit dimensions), but never both at once. Two escape hatch tiers, but typed before raw.
4. **The contract is generated.** JSON Schema is `go generate`'d from the `pkg/ir` Go types — schema, validator, and Go structs cannot drift.

---

## Scope

**In scope (this spec):**
- Project file layout convention and discovery
- Top-level YAML grammar (project, components, compositions, stack values)
- Component spec surface: T-shirt vs explicit sizing, typed and raw escape hatches
- Stack overlay semantics (variables + structural toggles)
- Composition syntax and expansion semantics
- Interpolation engine grammar, scopes, and evaluation order
- Cross-component output references (syntax; type checking)
- Naming rules and reserved identifiers
- JSON Schema generation strategy
- Validator phases and diagnostics
- Error model
- Loader behavior (file discovery, merge order, includes)
- CLI integration (`nimbusfab validate`, `nimbusfab show`)
- API surface (`/api/v1/schema`, `/api/v1/projects/{id}/render`)

**Out of scope (deferred):**
- Per-component-type schemas (`network`, `compute`, `database`, `storage`) — deferred to a separate **v1 Component Types Reference** spec. This spec confirms those four types ship and defines the *shape every type's spec follows*; their concrete fields are catalogued elsewhere.
- Cross-stack references (`${stack.dev.component.X.outputs.Y}`) — v2.
- Interpolation functions (`${trim(var.x)}`, `${coalesce(...)}`) — v2.
- Auto-completion / LSP server — v2. The JSON Schema this spec generates is sufficient for any LSP-aware editor.
- YAML linting beyond well-formedness — users use `yamllint` or similar.

---

## Project file layout

A project is a directory. The loader discovers files from a canonical set of locations, but everything can also collapse into a single file for tiny projects.

### Canonical layout

```
myproject/
  project.yaml              # required; declares apiVersion, name, stacks
  components/               # optional; one file per component (preferred at scale)
    orders-db.yaml
    web-network.yaml
  compositions/             # optional; user-defined component types
    tuned-postgres.yaml
  stacks/                   # optional; per-stack overlays
    dev/
      values.yaml
    prod/
      values.yaml
  parity.yaml               # optional; opt-in parity rules
```

### Single-file fallback

A tiny project can put everything in `project.yaml` using YAML document separators or array fields:

```yaml
# project.yaml (everything inline)
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: { stateBackend: { kind: local } }
  prod: { stateBackend: { kind: s3, config: { bucket: nimbusfab-state } } }
components:
  - name: orders-db
    type: database
    spec: { ... }
    targets: [ ... ]
compositions:
  - kind: TunedPostgres
    schema: { ... }
    template: { ... }
```

### Discovery rules

1. **`project.yaml` is required** at the project root. It carries `apiVersion`, `name`, and `stacks`. It MAY also contain inline `components:` and `compositions:` lists.
2. **`components/*.yaml`** is scanned recursively (one directory level deep). Each file declares one or more components (separated by `---` for multi-doc YAML).
3. **`compositions/*.yaml`** is scanned the same way for compositions.
4. **`stacks/<stack-name>/values.yaml`** is loaded ONLY for the stack the user selected (`--stack prod`). Other stacks are not parsed unless explicitly requested.
5. **`parity.yaml`** at the project root is loaded if present.
6. **Anything else** (other `.yaml` files outside these locations) is ignored. The loader emits a warning if it finds top-level YAML files that aren't part of the convention.

### Merge order

When the same Component name appears in both `project.yaml` inline AND `components/*.yaml`, the loader fails with `ErrDuplicateComponent`. There is no automatic precedence — explicit > implicit. If users want overlays, they use stack values.

### Includes

Files MAY use `!include <relative-path>` YAML tags to compose shared snippets. Includes are loaded relative to the file declaring them. Cycles are rejected. The included file is parsed as a YAML node and substituted in place — there's no string templating.

```yaml
# components/orders-db.yaml
name: orders-db
type: database
spec:
  engine: postgres
  size: small
  tags: !include ../shared/tags.yaml      # YAML map merged in place
targets:
  - !include ../shared/aws-target.yaml
  - !include ../shared/gcp-target.yaml
```

---

## Top-level YAML grammar

### `project.yaml`

```yaml
apiVersion: infra.dev/v1alpha1     # required; rejected if unknown
name: orders                       # required; project identifier
description: |
  Optional human-readable description.

stacks:                            # required; at least one
  dev:
    stateBackend:
      kind: local                  # local | s3 | gcs | azurerm | pg
    description: "ephemeral dev environment"
  prod:
    stateBackend:
      kind: s3
      config:
        bucket: nimbusfab-state
        region: us-east-1
        key: orders/${stack.name}/terraform.tfstate

components: []                     # optional; can also live in components/*.yaml
compositions: []                   # optional; can also live in compositions/*.yaml
```

### `components/<name>.yaml`

```yaml
apiVersion: infra.dev/v1alpha1     # required if file standalone
name: orders-db                    # required; DNS-1123 subdomain
type: database                     # required; built-in or composition kind
description: |
  Optional human-readable description.
mode: replicate                    # optional; default `replicate`; v2 adds `composed`

spec:                              # required; type-specific schema
  engine: postgres
  version: "16"
  size: small                      # T-shirt size; mutually exclusive with compute/storage
  # OR:
  # compute:
  #   vCPU: 8
  #   memoryGB: 32
  # storage:
  #   sizeGB: 500
  #   iops: 3000
  #   class: ssd

targets:                           # required; at least one
  - cloud: aws
    region: ${var.aws_region}
    credentialRef: ${var.aws_creds}
    spec:                          # optional; merged into Component.Spec for this target
      aws:                         # typed escape hatch (tier 1)
        instanceClass: db.t3.medium
        backupRetentionDays: 14
      raw:                         # raw passthrough (tier 2; warned)
        apply_immediately: true
  - cloud: gcp
    region: ${var.gcp_region}
    credentialRef: ${var.gcp_creds}
    spec:
      gcp:
        availabilityType: REGIONAL

refs:                              # optional; cross-component dependencies
  - component: web-network
    output: vpc_id
    as: vpcId
  - component: web-network
    output: subnet_ids
    as: subnetIds

policy:                            # optional; per-component policies
  serial: false                    # set true to disable parallel target fan-out
```

### `compositions/<kind>.yaml`

```yaml
apiVersion: infra.dev/v1alpha1
kind: TunedPostgres                # registers a new component type named "TunedPostgres"
description: |
  A postgres database with tuned defaults for write-heavy workloads.
schema:                            # JSON Schema for this composition's spec field
  type: object
  required: [size]
  properties:
    size:
      type: string
      enum: [small, medium, large]
    backupRetentionDays:
      type: integer
      minimum: 1
      default: 14
template:                          # the expansion body
  resources:
    - name: ${input.name}-db        # ${input.*} scope: the consuming Component's fields
      type: database
      spec:
        engine: postgres
        version: "16"
        size: ${input.size}
      targets: ${input.targets}     # forwarded verbatim
```

### `stacks/<stack>/values.yaml`

```yaml
apiVersion: infra.dev/v1alpha1
vars:
  aws_region: us-east-1
  gcp_region: us-central1
  aws_creds: aws-prod
  gcp_creds: gcp-prod
  db_size: large
disabled:
  components: [analytics-warehouse]    # not deployed in this stack
  targets:                             # disable a specific (component, cloud, region) triple
    - component: web-network
      cloud: azure
```

### `parity.yaml`

Defined in the parity spec; this DSL spec recognizes its presence and feeds it to the parity engine after validation.

---

## Component spec: sizing and escape hatches

### Sizing surfaces (mutually exclusive)

**T-shirt size:**

```yaml
spec:
  engine: postgres
  size: small        # resolves via parity contracts (small | medium | large | xlarge)
```

**Explicit dimensions:**

```yaml
spec:
  engine: postgres
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

Mixing `size:` with `compute:`/`storage:`/`features:` is `ErrSizeConflict`. Per-type schemas declare which fields are required when explicit; missing fields are `ErrMissingDimension`.

### Two-tier escape hatch

Per-target `spec:` may carry both abstract knobs and escape hatches:

```yaml
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-prod
    spec:
      aws:                         # tier 1: typed, schema-validated
        instanceClass: db.t3.medium
        backupRetentionDays: 14
        performanceInsightsEnabled: true
      raw:                         # tier 2: raw Tofu JSON merge, unvalidated
        deletion_protection: true
        apply_immediately: false
```

**Tier 1 (`<cloud>:` blocks).** Each cloud adapter declares a JSON Schema for its tier-1 fields. Validator runs the right schema based on `target.cloud`. Adapter `Emit()` honors these fields when producing primitives. **Validated; warnings on unknown fields.**

**Tier 2 (`raw:`).** A YAML map whose contents are merged directly into the emitted Tofu JSON resource block for that target. No validation. Validator emits a warning (`WarnRawEscape`) at every use so it shows up in PR review.

**Merge order at Emit():** `Component.spec` → `target.spec.<cloud>` (tier 1) → `target.spec.raw` (tier 2). Later wins. Empty / unset fields don't overwrite set ones.

---

## Stack overlays

### Variables

`stacks/<stack>/values.yaml` declares `vars:`. Components reference them via `${var.<name>}`. Variable types are inferred from value shape (string, number, boolean, list, map) and type-checked at use sites (a string-context interpolation requires a string-typed var).

```yaml
# stacks/prod/values.yaml
vars:
  db_size: large
  backup_days: 30
  feature_flags:
    pi_enabled: true
    multi_az: true
```

```yaml
# components/orders-db.yaml
spec:
  size: ${var.db_size}                     # string
targets:
  - cloud: aws
    spec:
      aws:
        backupRetentionDays: ${var.backup_days}    # number
        performanceInsightsEnabled: ${var.feature_flags.pi_enabled}  # boolean from map
```

Dotted access into map vars uses standard scope syntax (`${var.feature_flags.pi_enabled}`); lists are accessed via numeric index (`${var.regions.0}`).

### Structural toggles

`disabled:` removes components or specific targets from a stack:

```yaml
disabled:
  components:
    - analytics-warehouse        # whole component excluded
  targets:
    - component: web-network
      cloud: azure              # only the Azure target of web-network excluded
    - component: orders-db
      cloud: gcp
      region: us-east1          # an even more specific filter
```

Disabled components are removed from the IR *after* validation (so they still validate, but never reach the provisioner). Disabled targets are removed from their component's `Targets` slice.

There is **no field-level override mechanism** in v1. Anything that needs to differ per stack flows through a `${var.*}` reference. (Rationale: variables are named and traceable; field overrides accumulate untyped patch sprawl. Reassessed in v2 if real users hit walls.)

---

## Compositions

A Composition registers a new value for `type:`. When a Component's `type` matches a registered Composition `kind`, the validator replaces that Component with the Composition's expanded resources.

### Schema

```yaml
apiVersion: infra.dev/v1alpha1
kind: TunedPostgres
schema:                            # JSON Schema for spec field
  type: object
  required: [size]
  properties:
    size:
      type: string
      enum: [small, medium, large]
    backupRetentionDays:
      type: integer
      default: 14
template:
  resources:                       # list of Components
    - name: ${input.name}-db
      type: database
      spec:
        engine: postgres
        version: "16"
        size: ${input.size}
      targets: ${input.targets}
```

### Expansion semantics

1. Parser registers each Composition by `kind`. Duplicate kinds across files → `ErrDuplicateComposition`.
2. Validator: for each Component whose `type` matches a Composition kind, validate the Component's `spec` against the Composition's `schema`.
3. Composition engine: expand the template, evaluating `${input.<field>}` against the consuming Component (`input.name`, `input.spec.*`, `input.targets`, `input.refs`).
4. Replace the consuming Component with the expanded resources. Names of expanded resources may collide with names of other Components; collision → `ErrDuplicateComponent`.
5. Expansion is recursive (a Composition's template may itself reference another Composition). Cycles → `ErrCompositionCycle`.

### Snapshot semantics

Composition expansion is snapshot-per-deployment (architecture spec lock-in). Editing a Composition does NOT live-update existing deployments — users must re-plan to pick up changes. The expanded IR is stored verbatim in the inventory's `components.ir_json` so audits show exactly what was deployed.

---

## Interpolation engine

### Grammar

Interpolation is a simple template grammar embedded in YAML scalar strings.

```
template      := segment*
segment       := literal | interp
interp        := '${' expression '}'
expression    := scope '.' path
scope         := 'var' | 'env' | 'project' | 'stack' | 'target' | 'component' | 'input' | 'secret'
path          := identifier ('.' identifier)*
identifier    := DNS_1123_LABEL_RELAXED | NUMERIC_INDEX
```

A literal `$` is escaped as `$$` (rarely needed; YAML strings rarely contain `${`). A `${` that is not followed by a well-formed expression and `}` is a `ErrInterpolationParse`.

### Scopes

| Scope | Source | Resolved by | Available during |
|---|---|---|---|
| `var.<path>` | stack `values.yaml` vars + `project.yaml` defaults | validator | all phases |
| `env.<NAME>` | process environment variables | loader (eager) | all phases |
| `project.<field>` | `project.yaml` top-level fields (`name`, `description`) | loader | all phases |
| `stack.<field>` | the active stack's fields (`name`, `stateBackend.kind`) | loader | all phases |
| `target.<field>` | the current target's fields (`cloud`, `region`, `credentialRef`) | provisioner (target-bound) | per-target emit |
| `component.<n>.outputs.<k>` | another component's declared outputs | apply-time | apply only |
| `input.<field>` | the consuming Component's fields (only inside Composition templates) | composition engine | composition expansion |
| `secret.<ref>.<key>` | secrets backend, lazily resolved | provisioner | adapter emit |

### Evaluation order

1. **Eager scopes** (`env.*`, `project.*`, `stack.*`, `input.*`, `var.*`) are evaluated during validation. Any unresolvable interpolation is `ErrInterpolationUnresolved`.
2. **Lazy scopes** (`target.*`, `component.X.outputs.Y`, `secret.*`) are kept as opaque markers in the IR and resolved at plan/apply time. The validator type-checks references but does not execute them.
3. Mixed strings (`"${target.cloud}-${target.region}"`) are split into segments; eager segments substitute, lazy segments remain.

### Type checking

- `var.x` typed by the value in `values.yaml` (string / number / boolean / list / map).
- `component.X.outputs.Y` typed by component-type's declared output schema. Schema mismatch (e.g., using a list output as a string) → `ErrOutputTypeMismatch`.
- Interpolation inside a string-typed field coerces scalar values to strings (`true` → `"true"`, `42` → `"42"`); list/map values cannot be coerced and produce `ErrInterpolationTypeMismatch`.

### No functions in v1

Functions like `${trim(var.x)}` or `${coalesce(var.a, var.b)}` are explicitly v2. v1's grammar rejects parentheses.

---

## Cross-component references

```yaml
# components/orders-db.yaml
spec:
  networkRef: ${component.web-network.outputs.vpc_id}
  subnetRefs: ${component.web-network.outputs.subnet_ids}
refs:
  - component: web-network
    output: vpc_id
    as: vpcId
  - component: web-network
    output: subnet_ids
    as: subnetIds
```

Two complementary mechanisms:

1. **Inline interpolation** — `${component.X.outputs.Y}` anywhere a value is expected. Simple, ad hoc.
2. **Explicit `refs:` list** — declares dependencies the engine uses to order plan/apply, and exposes outputs under a short alias (`refs[0].as: vpcId`) usable as `${ref.vpcId}` within the component's spec.

The `refs:` list is also what the engine uses to compute the **component DAG** for parallel execution. Inline interpolations also create dependencies, but explicit `refs:` is preferred when ordering matters because it's auditable.

### Output schemas

Each built-in component type declares its outputs (per-type spec). Cloud adapters fill in actual values after `tofu apply` by parsing `tofu output -json`. Outputs are persisted in the inventory and read back during subsequent plans.

**For v1 component types:**
- `network.outputs`: `vpc_id`, `subnet_ids` (list), `route_table_ids` (list)
- `compute.outputs`: `instance_ids` (list), `private_ips` (list), `public_ips` (list)
- `database.outputs`: `endpoint`, `port`, `connection_string`, `db_name`
- `storage.outputs`: `bucket_name`, `bucket_url`

Full output schemas are defined in the v1 Component Types Reference spec.

---

## Naming rules

All identifiers (project name, stack name, component name, composition kind, ref `as`) follow **DNS-1123 subdomain rules**:

- Lowercase ASCII letters, digits, and hyphens
- Must start with a letter or digit
- Must end with a letter or digit
- Max 63 characters per identifier
- Composition `kind` MAY use PascalCase identifiers (matches `kind:` conventions in Kubernetes/Crossplane) — recognized via a separate regex; uppercase ASCII allowed.

Reserved identifiers (cannot be used for component or composition names): `nimbusfab`, `tofu`, `terraform`, `default`, `system`, `_internal`.

Names must be **unique within scope**:
- Component name unique within `(project, stack)`.
- Composition kind unique within `project`.
- Stack name unique within `project`.
- Variable name unique within `(project, stack)`.

Duplicate names → `ErrDuplicate<Thing>` with both source locations.

---

## JSON Schema generation

### Strategy

JSON Schema for the entire IR is generated from the Go types in `pkg/ir` at build time via `go generate ./pkg/ir/...`. The generated files live under `pkg/ir/schema/`:

```
pkg/ir/schema/
  v1alpha1/
    project.json
    component.json
    composition.json
    stack-values.json
    parity-rules.json
```

These files are:
- Committed to git (so contributors can review schema changes in PRs).
- Embedded into the binary via `//go:embed pkg/ir/schema/v1alpha1/*.json`.
- Served read-only at `GET /api/v1/schema/v1alpha1/{name}.json` so IDEs (VSCode `yaml.schemas`, JetBrains JSON Schema) can fetch them.

### Schema generation tool

A small Go program in `tools/schemagen/` walks `reflect.Type` of the IR root types, generates JSON Schema 2020-12 documents, applies per-field overrides from struct tags (`schema:"description=..."`, `schema:"format=date-time"`, etc.), and writes the JSON files. Round-trip tests assert that `GenerateJSONSchema()` (the embedded version) matches the file output.

### Adapter contributions

Each cloud adapter exports a `TierOneSchema()` returning the JSON Schema for its `<cloud>:` block. Schemas are merged into the per-target spec schema at startup:

```jsonc
// pkg/ir/schema/v1alpha1/component.json — fragment
{
  "$ref": "#/$defs/DeploymentTarget",
  "$defs": {
    "DeploymentTarget": {
      "properties": {
        "spec": {
          "type": "object",
          "properties": {
            "aws":   { "$ref": "#/$defs/AWSTargetSpec" },
            "gcp":   { "$ref": "#/$defs/GCPTargetSpec" },
            "azure": { "$ref": "#/$defs/AzureTargetSpec" },
            "raw":   { "type": "object", "additionalProperties": true }
          }
        }
      }
    }
  }
}
```

### Per-component-type spec schemas

Each built-in component type registers its spec schema during init. The validator looks up `componentTypes.Get(component.Type).SpecSchema()` and validates `component.Spec` against it.

For Compositions, the validator uses the Composition's own `schema:` field for spec validation, instead of a built-in type's schema.

---

## Validator phases

The validator runs phases in this order. Each phase emits zero or more diagnostics; phases continue even after errors (multiple errors per pass), but a phase MAY refuse to run if earlier phases produced fatal-severity errors that would make it meaningless (e.g., phase 4 doesn't run if phase 3 says the IR is malformed).

| # | Phase | Reads | Emits | Fatal? |
|---|---|---|---|---|
| 1 | YAML well-formedness | raw bytes | `ErrYAML*` | yes |
| 2 | APIVersion check | doc roots | `ErrUnknownAPIVersion`, `ErrMissingAPIVersion` | yes |
| 3 | JSON Schema | parsed IR | `ErrSchema<field>`, `WarnUnknownField` | yes |
| 4 | Interpolation parse | string fields | `ErrInterpolationParse`, `ErrInterpolationUnknownScope` | no |
| 5 | Composition expansion | components, compositions | `ErrCompositionCycle`, `ErrDuplicateComponent` | yes |
| 6 | Reference resolution | components, refs | `ErrRefUnknownComponent`, `ErrRefUnknownOutput`, `ErrCycleInRefs` | yes |
| 7 | Semantic checks | full IR | `ErrCloudNotSupported`, `ErrCredentialRefMissing`, `ErrSizeConflict`, `ErrDuplicate*` | yes |
| 8 | Parity contract resolution | components with sizing | `ErrUnknownSize`, `ErrMissingDimension` | yes |
| 9 | Variable resolution | interpolations + vars | `ErrInterpolationUnresolved`, `ErrInterpolationTypeMismatch` | no |

After phase 9, the validator returns:

```go
type ValidationReport struct {
    OK     bool       // true iff no `ErrSeverity` issues
    Issues []Issue    // every diagnostic, in source order
}
```

`Issue` carries: severity, stable code, message, dotted IR path, source `file:line` where available. The CLI renders this with color and grouping by severity; the web app surfaces it as inline lints on the YAML editor.

---

## IR mapping

The validator produces a fully-resolved `*ir.Project`. After validation:

- All eager interpolations are substituted (string values are concrete).
- All Compositions are expanded; `Project.Comps` is preserved for audit but no Component references them.
- Disabled components/targets are removed.
- Each `Component.Spec` has been merged with stack vars; no `${var.*}` remains.
- Lazy interpolations (`${component.X.outputs.Y}`, `${target.*}`, `${secret.*}`) remain as typed marker values in the IR (`ir.LazyRef`).
- Sizing has been normalized: T-shirt sizes are resolved against parity contracts into explicit `compute:` / `storage:` / `features:` minimums. The original `size: small` is retained alongside as metadata.
- Components are ordered by the engine's topological sort of the `refs:` graph for downstream consumption.

The provisioner consumes this validated IR; cloud adapters see one `DeploymentTarget` at a time and emit primitives.

---

## Error model

### Codes

Every diagnostic carries a stable code. Codes prefixed `Err` are errors (block plan/apply); prefixed `Warn` are warnings (proceed, emit notice). Codes are documented in `docs/error-codes.md` with explanations and remediation. A non-exhaustive sample:

| Code | Meaning |
|---|---|
| `ErrYAMLMalformed` | YAML parse failure |
| `ErrUnknownAPIVersion` | `apiVersion:` not recognized |
| `ErrSchemaRequiredField` | required field missing per JSON Schema |
| `ErrSchemaUnknownField` | extra field rejected per JSON Schema (strict mode) |
| `ErrInterpolationParse` | `${...}` not well-formed |
| `ErrInterpolationUnknownScope` | scope not in `var/env/...` set |
| `ErrInterpolationUnresolved` | eager-scope reference has no value |
| `ErrInterpolationTypeMismatch` | wrong type used in interpolation context |
| `ErrSizeConflict` | both `size:` and explicit dimensions set |
| `ErrUnknownSize` | T-shirt size not in parity contract |
| `ErrMissingDimension` | explicit dimensions missing required attribute |
| `ErrCloudNotSupported` | component type does not support the requested cloud |
| `ErrCredentialRefMissing` | `credentialRef` resolves nowhere |
| `ErrRefUnknownComponent` | `${component.X...}` X does not exist |
| `ErrRefUnknownOutput` | output Y not declared by component X |
| `ErrCycleInRefs` | `refs:` graph contains a cycle |
| `ErrCompositionCycle` | Composition templates form a cycle |
| `ErrDuplicateComponent` | same Component name from two sources |
| `WarnRawEscape` | `raw:` block used; flagged for PR review |
| `WarnUnknownField` | adapter's typed schema doesn't recognize a `<cloud>:` field |
| `WarnNoTargets` | Component declared with empty `targets:` |

### Diagnostic shape

```go
type Issue struct {
    Severity Severity   // "error" | "warning" | "info"
    Code     string     // stable; e.g., "ErrSizeConflict"
    Message  string     // human-readable
    Path     string     // dotted IR path, e.g. "components[2].targets[0].spec"
    Source   Source     // file:line:col when YAML provenance is available
    Hint     string     // remediation suggestion when applicable
}

type Source struct {
    File   string
    Line   int
    Column int
}
```

YAML provenance comes from `go-yaml`'s line/column metadata; preserved through include resolution by tagging each node with its origin file.

---

## Loader + composition expander internals

### Loader

`internal/dsl/loader` walks the project directory, parses files, and returns an unvalidated `*ir.Project` plus a `*Provenance` map (`ir.NodePath → Source`). It does NOT validate semantics; it just builds the tree.

Steps:
1. Open `project.yaml`. Parse `apiVersion`, `name`, `stacks`, optional inline `components`, `compositions`.
2. Walk `components/` recursively (one level deep), parse each file, append to `Project.Components`.
3. Walk `compositions/`, append to `Project.Comps`.
4. Resolve `!include` tags, recursively, with cycle detection.
5. If a stack was selected, load `stacks/<stack>/values.yaml` and attach to the loader state (for the validator).
6. If `parity.yaml` exists, load and attach (delivered to the parity engine post-validation).

### Composition expander

`pkg/composition/composition.go` implements `Expand(ctx, *ir.Project) error`. Called by the validator during phase 5.

For each Component whose Type matches a registered Composition Kind:
1. Validate the Component's spec against the Composition's `schema`.
2. Evaluate the Composition's `template.resources` with `${input.*}` resolving to the Component's fields.
3. Recursively expand any Compositions used by the template.
4. Replace the Component in `Project.Components` with the expanded resources.

Cycle detection: walk the Composition reference graph; reject cycles before expansion begins.

---

## CLI integration

```
nimbusfab validate                  # run phases 1–9; print report; exit 0 if no errors
nimbusfab validate --strict          # warnings treated as errors
nimbusfab show project               # print resolved IR after validation
nimbusfab show component <name>     # print resolved IR for one component
nimbusfab show --merged <name>      # print fully-merged IR including target.spec merges
nimbusfab show --diff <stack> <stack>  # diff resolved IRs between two stacks
nimbusfab schema [--format json]     # print the JSON Schema bundle
```

`nimbusfab validate` is what users put in pre-commit hooks and CI. `nimbusfab show` is the answer to "what does this actually produce?" — essential because of stack overlays and Composition expansion.

---

## API surface

- `GET /api/v1/schema/v1alpha1/{name}.json` — serves the generated JSON Schemas; supports `If-None-Match`/`ETag` for IDE caching.
- `POST /api/v1/projects/{id}/validate` — accepts an uploaded project (tar.gz or git ref), runs the validator, returns `ValidationReport`.
- `GET /api/v1/projects/{id}/render?stack=prod[&component=orders-db]` — returns the resolved IR after validation, optionally scoped.

All authenticated and `org_id`-scoped per the web app spec (forthcoming).

---

## Verification

1. **Round-trip schema test.** `go generate` writes JSON Schema; load embedded schema; validate every fixture YAML in `testdata/`; assert results match the validator's results from the Go side. Catches drift between schema and validator.
2. **Provenance preservation.** Take a 3-file project (`project.yaml` + `components/orders-db.yaml` + `stacks/prod/values.yaml`); deliberately introduce one error per file; assert that the resulting `Issue`s point to the correct file:line in each case.
3. **Composition expansion fixture.** A Composition whose template expands to a Component that itself uses another Composition; assert recursive expansion works and the final IR has no Composition-typed Components.
4. **Interpolation grammar.** Table-driven test over `${...}` strings: well-formed eager refs, well-formed lazy refs, mixed-segment strings, malformed inputs. Each gets a deterministic outcome.
5. **Sizing resolution.** YAML with `size: small` produces explicit dimensions matching the parity contract; YAML with both `size:` and `compute:` produces `ErrSizeConflict`; explicit dimensions missing `storage.sizeGB` produces `ErrMissingDimension`.
6. **Stack overlay tests.** Same components + two stacks; verify `disabled.components` removes the component from the validated IR; verify `disabled.targets` removes only the named target; verify `${var.x}` resolves to stack-specific values.
7. **Refs DAG and cycles.** Three components in a chain — verify topological order in the validated IR. Add a back-edge — verify `ErrCycleInRefs`.
8. **CLI snapshot tests.** `nimbusfab validate` and `nimbusfab show` output compared against golden files for canonical inputs.
9. **JSON Schema IDE integration smoke test.** Open a sample project in VSCode with the schema URL configured; verify autocomplete fires on `type:` (offering `database`, `compute`, `network`, `storage`, plus any registered compositions) and that hovering shows descriptions.
