# Validator Phase 4 (Per-Type Spec Schema) Subsystem Spec

**Status:** Subsystem spec. Wires per-type SpecSchema validation into the DSL validator pipeline. Each component's `spec` is validated against its `Type.SpecSchema()` — the per-type JSON Schemas that already ship at `pkg/components/schema/v1alpha1/` but are not currently applied during validation.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-15-dsl-ir-design.md` (validator phase pipeline)
- `pkg/components/types.go` (built-in Type registry with per-type SpecSchemas)
- Phase 3 of validator (`internal/dsl/validator/phase3_schema.go`) for the top-level project schema pattern to mirror

**Depended on by:**
- All CLI commands (`plan`, `apply`, `destroy`, `drift`, `parity`, `cost`, `validate`) — better error messages when component specs are malformed.
- Future user-defined types (out of scope here, but the registry-driven design is the hook).
- Web app — same validation layer surfaces type-specific errors to users in the IDE.

---

## Context

DSL/IR Phase 1 shipped Phases 1-3 of the validator pipeline:
- Phase 1: convert loader errors into Issues.
- Phase 2: API-version recognition.
- Phase 3: validate the IR against the top-level `project.json` JSON Schema (enforces structure: a component has a name, type, spec, targets, etc.).

What Phase 3 does **not** do: validate the contents of `component.spec`. The top-level schema treats `spec` as an opaque `object`. The actual per-type schemas (`network.json`, `compute.json`, `database.json`, `storage.json`) live in `pkg/components/schema/v1alpha1/` and are exposed via `components.Type.SpecSchema()` — but no validator phase reads them.

As a result:
- A `compute` component with `spec: { engine: "postgres" }` (mis-typed as a database) passes validation. The error surfaces later as an unhelpful adapter Emit failure.
- A `network` component missing `cidr` (required by network.json) passes validation; the adapter defaults to `10.0.0.0/16` silently.
- A `storage` component with `spec: { versioning: "yes" }` (string instead of bool) passes validation; the YAML decoder coerces nothing and the adapter quietly ignores the field.

Phase 4 closes this gap. After Phase 4 lands, a malformed component spec produces a structured `ir.Issue` with the schema path that failed (e.g. `components[2].spec.cidr: required property missing`) — directly comparable to the messages users already see for top-level structural errors.

**Design principles:**
1. **Mirror Phase 3.** Same JSON Schema compiler (`santhosh-tekuri/jsonschema/v5`), same Issue translation pattern. Reviewer sees "per-type version of phase3_schema.go" and the structure clicks.
2. **Registry-driven.** The Validator takes a `components.Registry` at construction time. This makes it possible to register user-defined types in the future without touching the validator. For Phase 4 the only registered types are the four built-ins (`components.DefaultRegistry()`).
3. **Unknown type ≠ schema error.** If `component.type` doesn't match any registered Type, emit a distinct issue (`ErrValidatorUnknownType`) — not a "schema not found" cascade. Users should immediately know whether they have a typo in the type name or a typo in the spec.
4. **Schemas are tier-zero.** The schemas embedded in `pkg/components/schema/v1alpha1/` enforce required fields, value types, and value patterns (e.g. CIDR regex). They do not validate cross-component semantics (refs, dependencies) — that's Phase 5's job.

---

## Scope

**In scope (this spec):**
- New `internal/dsl/validator/phase4_typespec.go` implementing per-component spec validation against the registered Type's SpecSchema.
- Refactor `validator.New()` to accept a `components.Registry`. Production callers pass `components.DefaultRegistry()`; tests can inject custom registries.
- Update all 8 CLI callsites (`apply.go`, `cost.go`, `destroy.go`, `drift.go`, `parity.go`, `plan.go`, `validate.go` + the one in `validate.go` for loader-error path) to use the new signature.
- Issue codes: `ErrValidatorUnknownType` (type not in registry), `ErrValidatorTypeSpec` (per-type schema violation).
- Test fixtures under `internal/dsl/validator/testdata/` for: well-formed-per-type-and-passes; missing-required-field; wrong-type-value; type-name-not-in-registry.
- Update `cmd/cli/testdata/validator-*` fixtures (where present) to cover the new error surfaces.

**Out of scope (deferred):**
- Cross-component semantic validation (ref resolution, dep-cycle detection). Phase 5.
- Custom user-defined types via plugin loading. Phase 6+.
- Per-cloud spec constraints (e.g. AWS supports MariaDB but GCP doesn't — that's adapter `Validate()`, not the type-schema layer). Already handled adapter-side.
- Spec interpolation (`${var.foo}` substitution before validation). Phase 5 or later.
- Schema evolution / version negotiation across `apiVersion`s. Phase 6.
- IDE/LSP integration. Web app phase.

---

## Pipeline placement

Phase 4 runs **after** Phase 3 (top-level schema) and **before** any future Phase 5 (semantic) phases. This ordering matters: Phase 3 ensures `components[i].spec` exists and is an object; Phase 4 can then assume the spec field is structurally valid before checking its contents.

Implementation:

```go
func (v *fsValidator) Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error) {
    report := &ir.ValidationReport{}
    if proj == nil { /* ... */ }
    if err := phase2APIVersion(proj, report); err != nil { return nil, err }
    if err := phase3Schema(proj, report); err != nil { return nil, err }
    if err := phase4TypeSpec(proj, v.registry, report); err != nil { return nil, err }
    return report, nil
}
```

Phase 4 does **not** short-circuit if Phase 3 produced issues. Each phase contributes independently to the same report so the user sees all errors in one pass.

---

## Per-component flow

For each `component := range proj.Components`:

1. **Look up type.** `t, ok := registry.Type(component.Type)`. If `!ok`, append `ErrValidatorUnknownType` issue with the type name, the path `components[i].type`, and the known types list as a hint. Continue to next component.

2. **Compile schema.** Use the same `jsonschema.NewCompiler()` pattern as Phase 3. Schemas are cached per type within the phase invocation (typed map at function scope) so a project with 10 `compute` components compiles `compute.json` once.

3. **Marshal spec to JSON.** `json.Marshal(component.Spec)` then `json.Unmarshal` into `any`, mirroring Phase 3.

4. **Validate.** `schema.Validate(doc)` returns a `*jsonschema.ValidationError` if anything fails; walk its `Causes` tree the same way Phase 3 does to convert each leaf to an `ir.Issue` with the path prefix `components[i].spec.` + the schema field path.

---

## Issue shapes

### ErrValidatorUnknownType

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorUnknownType",
    Message:  fmt.Sprintf("component type %q is not registered (known: %s)", component.Type, strings.Join(registry.Types(), ", ")),
    Path:     fmt.Sprintf("components[%d].type", i),
}
```

### ErrValidatorTypeSpec

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorTypeSpec",
    Message:  schemaErr.Message,
    Path:     fmt.Sprintf("components[%d].spec.%s", i, schemaErr.InstanceLocation),
}
```

Where `schemaErr.InstanceLocation` is the JSON Pointer to the failing field (e.g. `/cidr`), translated to dotted notation before being appended.

---

## Worked examples

### Missing required field

```yaml
# components/web-net.yaml
apiVersion: infra.dev/v1alpha1
name: web-net
type: network
spec:
  subnetCount: 2  # missing required "cidr"
targets:
  - { cloud: aws, region: us-east-1 }
```

Phase 4 produces:

```
ERROR components[0].spec: missing properties: "cidr"
  ErrValidatorTypeSpec  (network: spec.cidr is required)
```

### Wrong-type value

```yaml
spec:
  cidr: 10.0.0.0/16
  subnetCount: "two"  # should be integer
```

```
ERROR components[0].spec.subnetCount: expected integer, but got string
  ErrValidatorTypeSpec  (network: spec.subnetCount must be integer 1-16)
```

### Unknown type

```yaml
type: storrage  # typo
```

```
ERROR components[0].type: component type "storrage" is not registered (known: network, compute, database, storage)
  ErrValidatorUnknownType
```

### CIDR pattern violation

```yaml
spec:
  cidr: not-a-cidr
```

```
ERROR components[0].spec.cidr: does not match pattern "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+/[0-9]+$"
  ErrValidatorTypeSpec
```

---

## Constructor change

```go
// Before:
func New() Validator

// After:
func New(registry components.Registry) Validator
```

Production callers:

```go
// cmd/cli/{apply,cost,destroy,drift,parity,plan,validate}.go
v := validator.New(components.DefaultRegistry())
```

`components.DefaultRegistry()` already exists and returns a Registry pre-populated with the four built-in types — no new factory needed.

Tests construct registries explicitly, often with a single registered type to isolate one schema.

---

## Performance considerations

A project with N components and T distinct types compiles T schemas, not N. The per-invocation cache is a `map[string]*jsonschema.Schema` rebuilt for each `Validate()` call — schemas are cheap to compile (single-digit milliseconds for the v1 schemas) and the validator is not on a hot path. If profiling shows it matters later, the cache moves to the Validator struct.

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrValidatorUnknownType` | phase4_typespec | `component.type` doesn't match any registered Type |
| `ErrValidatorTypeSpec` | phase4_typespec | `component.spec` failed per-type SpecSchema validation |

Both are `SeverityError`. There is no `SeverityWarning` form — if a spec violates its type schema, downstream code will fail or silently misbehave.

---

## Verification (design-level)

1. **Walkthrough: missing required field.** Network spec without `cidr`. Phase 3 passes (`spec` is an object). Phase 4 looks up network type, compiles network.json, validates `{ subnetCount: 2 }` against it, hits `required: ["cidr"]`, emits a single Issue with path `components[0].spec` and the schema's missing-properties message.
2. **Walkthrough: unknown type.** Component with `type: storrage`. Phase 4's registry lookup fails; emits `ErrValidatorUnknownType` with the four known types listed; does NOT attempt schema validation (no schema to validate against).
3. **Walkthrough: well-formed N×T.** A project with 4 components × 2 types compiles 2 schemas (cached per phase invocation), validates each component's spec, produces zero issues.
4. **Walkthrough: cascading errors.** A project with one invalid component does not abort Phase 4 — remaining components are still validated. The report contains all issues across all components.
5. **Backward-compat walkthrough.** Existing fixtures in `cmd/cli/testdata/` pass through Phase 4 unchanged because their specs are already well-formed (Phase 4 is additive validation, not a behavior change for happy-path inputs).

---

## Future hooks (not Phase 4)

- **Cross-ref validation** (Phase 5): for each `component.refs[*]`, confirm the referenced component exists and the named output is declared by its Type's `Outputs()` map.
- **Per-cloud type support** check: emit a warning if a target's cloud isn't in the Type's `SupportedClouds()` list (currently all built-in types support all clouds, so this is latent until v2 types).
- **Spec interpolation** (`${var.foo}`): apply variable substitution *before* Phase 4 so the validated spec reflects the deployed spec.
- **Schema evolution**: when `apiVersion` advances, validator phase 4 looks up the per-type schema directory matching that version (`pkg/components/schema/v1beta1/` etc.).
- **Plugin-loaded types**: user-defined types register with the same Registry interface; Phase 4 picks them up transparently.
