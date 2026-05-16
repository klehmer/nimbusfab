# Validator Phase 5 (Cross-Component Refs) Subsystem Spec

**Status:** Subsystem spec. Wires cross-component reference validation into the DSL validator pipeline. Each `component.refs[*]` is checked against the project component graph and the referenced Type's declared `Outputs()`.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-16-validator-phase4-design.md` (per-type spec validation; same pipeline pattern)
- `pkg/components/types.go` (Type.Outputs() already declares per-type outputs)
- `pkg/ir/types.go` (ComponentRef shape: Component / Output / As)

**Depended on by:**
- All CLI commands — better errors when refs point at nonexistent components or undeclared outputs.
- Provisioner — currently passes ref keys through to adapters as opaque strings; Phase 5 catches typos before they reach adapter Emit.
- Future plugin types — Outputs() declarations become a contract that ref validation enforces.

---

## Context

DSL/IR Phase 1 + Validator Phase 4 together get individual components well-validated: structural shape (Phase 3) and per-type spec contents (Phase 4). What's still unchecked is the *graph* — how components reference each other.

Today a component can declare:

```yaml
refs:
  - component: webnetwork    # typo — actual name is "web-network"
    output: vpc_id
    as: vpcId
```

The loader accepts this (it's structurally a valid ref). Phase 3 accepts it (project.json doesn't enforce semantic links). Phase 4 accepts it (per-type schemas don't know about other components). The error finally surfaces at Provisioner time as a runtime resolution failure with a stack trace.

Similar gap for outputs: a ref to `output: subnet_ids` against a `compute` component is wrong (compute publishes `instance_ids` / `private_ips` / `security_group_id`, not `subnet_ids`). Phase 4 doesn't catch this either.

Phase 5 closes both gaps. After Phase 5 lands, the validator produces structured Issues with names and paths *before* the provisioner ever runs.

**Design principles:**
1. **Mirror Phases 3 and 4.** Same Issue translation pattern. Reviewer sees "ref-validation version of phase4_typespec.go" and the structure clicks.
2. **Registry-aware.** Phase 5 uses the same `components.Registry` Phase 4 takes — looks up `Type.Outputs()` to validate the `output:` field of each ref.
3. **Cycles are errors, not warnings.** A → B → A in the ref graph cannot be resolved at provision time. Emit an explicit `ErrValidatorRefCycle` with the cycle path.
4. **Self-refs are errors.** A component referring to its own output is always a mistake (the output won't exist until after the component is provisioned, which can't happen if the same component is consuming the output).
5. **Phase 5 does NOT validate ref shape** — Phase 3's project.json schema already enforces required fields (`component`, `output`). Phase 5 assumes refs are structurally well-formed.

---

## Scope

**In scope (this spec):**
- New `internal/dsl/validator/phase5_refs.go` implementing component-existence, output-existence, self-reference, and cycle-detection checks.
- Wired into the validator pipeline after Phase 4.
- Issue codes: `ErrValidatorRefUnknownComponent`, `ErrValidatorRefUnknownOutput`, `ErrValidatorRefSelf`, `ErrValidatorRefCycle`.
- Cycle detection uses standard DFS with three-color marking. Reports the cycle as a comma-joined path of component names (e.g. `web-app → orders-db → web-app`).
- Unit tests covering all four error modes plus happy-path (well-formed multi-component ref graph passes).
- A CLI end-to-end test asserting the new error codes surface through `runValidate`.

**Out of scope (deferred):**
- Output *value-shape* validation (e.g. is the `vpcId` declared by network actually a `string` matching what the consumer expects?). The Type.Outputs() Kind field exists but consumers don't declare expected Kinds. Future phase.
- Ref interpolation (`${component.foo.output.bar}` shorthand in spec strings) — Phase 7 if needed.
- Composition-graph validation (v2 Composed components). Phase 4 already noted this as out of scope for the v1 line.
- Per-cloud ref support — refs are cloud-agnostic in v1; an aws-side network can publish vpc_id to a gcp-side compute, even though the *adapter* may not know what to do with it. That's an adapter concern.
- Dynamic refs (computed at apply time from inventory state). Future inventory-aware validation.

---

## Pipeline placement

Phase 5 runs **after** Phase 4. Ordering rationale: Phase 4 ensures `component.type` is registered (Phase 5 looks up Type.Outputs() — must have a valid type). Phase 4 also ensures the spec is well-formed (Phase 5 doesn't depend on it but it's a clean ordering: structural → per-type spec → cross-component graph).

```go
func (v *fsValidator) Validate(ctx, proj) (*ir.ValidationReport, error) {
    // ... existing phases 1-4
    if err := phase5Refs(proj, v.registry, report); err != nil {
        return nil, err
    }
}
```

Phase 5 does NOT short-circuit on prior errors. The whole point of the validator is producing all issues in one pass. If Phase 4 flagged an unknown type, Phase 5 still runs — but skips refs that *originate* from the bad-type component (since Type lookup would fail). Refs *targeting* a bad-type component still produce a meaningful error (the target component name exists; the type-specific output check is what gets skipped for that target).

---

## Per-ref flow

For each `comp := range proj.Components`, for each `ref := range comp.Refs`:

1. **Self-reference check.** If `ref.Component == comp.Name`, emit `ErrValidatorRefSelf` and continue.

2. **Component-existence check.** Look up `ref.Component` in the project's component list (built once at phase start as `map[string]ir.Component`). If absent, emit `ErrValidatorRefUnknownComponent` with the component name; continue (no output check possible without a target).

3. **Output-existence check.** Look up the target's type in the registry; if found, check `t.Outputs()` for `ref.Output`. If the target's type isn't in the registry (Phase 4 already flagged it), skip this check silently. If the output name isn't declared, emit `ErrValidatorRefUnknownOutput` listing the available output names.

4. **Empty output field.** If `ref.Output == ""`, emit `ErrValidatorRefUnknownOutput` with "empty output name". (Phase 3 schema already requires the field, but defense in depth.)

After the per-component pass, run cycle detection over the ref graph.

---

## Cycle detection

Build a directed graph: nodes = component names, edges = `(from comp, to ref.Component)`. Run DFS with three-color marking (WHITE: unvisited; GRAY: in current DFS path; BLACK: fully explored). When DFS encounters a GRAY node, that's a back-edge → cycle.

Edge cases:
- **Self-loop** detected separately (per-ref self-check above) — cycle detection treats self-loops as already-handled and skips them to avoid duplicate issues.
- **Multiple disjoint cycles** — report each separately; don't conflate.
- **Cycle containing an unknown component** — if A → B → C → A and C is unknown, the per-ref pass already emitted ErrValidatorRefUnknownComponent for B's ref to C. The cycle won't be detectable (no edge to follow from C). That's fine; user fixes the unknown ref, re-runs, then the cycle (if still present) surfaces.

Cycle report shape:

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorRefCycle",
    Message:  fmt.Sprintf("ref cycle detected: %s", strings.Join(append(path, path[0]), " → ")),
    Path:     fmt.Sprintf("components[%d].refs", componentIdx(path[0])),
}
```

---

## Issue shapes

### ErrValidatorRefUnknownComponent

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorRefUnknownComponent",
    Message:  fmt.Sprintf("ref points at unknown component %q (known: %s)", ref.Component, strings.Join(knownNames, ", ")),
    Path:     fmt.Sprintf("components[%d].refs[%d].component", compIdx, refIdx),
}
```

### ErrValidatorRefUnknownOutput

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorRefUnknownOutput",
    Message:  fmt.Sprintf("component %q (type %s) does not declare output %q (declared: %s)",
                ref.Component, targetType, ref.Output, strings.Join(outputNames, ", ")),
    Path:     fmt.Sprintf("components[%d].refs[%d].output", compIdx, refIdx),
}
```

### ErrValidatorRefSelf

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorRefSelf",
    Message:  fmt.Sprintf("component %q refs itself", comp.Name),
    Path:     fmt.Sprintf("components[%d].refs[%d].component", compIdx, refIdx),
}
```

### ErrValidatorRefCycle

```go
ir.Issue{
    Severity: ir.SeverityError,
    Code:     "ErrValidatorRefCycle",
    Message:  "ref cycle detected: web-app → orders-db → web-app",
    Path:     "components[N].refs", // N is the index of the first cycle member
}
```

---

## Worked examples

### Typo'd component name

```yaml
# web-app.yaml
refs:
  - component: webnetwork   # actual name is "web-network"
    output: vpc_id
    as: vpcId
```

```
ERROR components[1].refs[0].component: ref points at unknown component "webnetwork" (known: web-network, orders-db, uploads)
  ErrValidatorRefUnknownComponent
```

### Wrong output for type

```yaml
# orders-db.yaml refs against web-app (a compute component)
refs:
  - component: web-app
    output: subnet_ids        # compute publishes instance_ids, not subnet_ids
    as: subnetIds
```

```
ERROR components[2].refs[0].output: component "web-app" (type compute) does not declare output "subnet_ids" (declared: instance_ids, private_ips, security_group_id)
  ErrValidatorRefUnknownOutput
```

### Self-reference

```yaml
# web-app.yaml
refs:
  - component: web-app        # itself
    output: instance_ids
    as: selfIds
```

```
ERROR components[1].refs[0].component: component "web-app" refs itself
  ErrValidatorRefSelf
```

### Two-component cycle

```yaml
# a.yaml: refs: - component: b
# b.yaml: refs: - component: a
```

```
ERROR components[0].refs: ref cycle detected: a → b → a
  ErrValidatorRefCycle
```

### Happy path

The existing full-stack-project fixture (web-network publishes vpc_id/subnet_ids, consumed by orders-db and web-app) produces zero Phase 5 issues.

---

## Performance

Per-validation cost: O(N + E) where N is component count and E is ref count. Single map allocation for component-name lookup; single DFS for cycle detection. Negligible for realistic projects (sub-millisecond for hundreds of components).

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrValidatorRefUnknownComponent` | phase5_refs | `ref.Component` doesn't match any component name |
| `ErrValidatorRefUnknownOutput` | phase5_refs | `ref.Output` isn't in target's `Type.Outputs()` |
| `ErrValidatorRefSelf` | phase5_refs | `ref.Component == comp.Name` |
| `ErrValidatorRefCycle` | phase5_refs | Directed cycle detected in ref graph |

All `SeverityError`. No warning variants — every case is a hard failure at provision time.

---

## Verification (design-level)

1. **Walkthrough: typo'd component name.** A web-app ref with `component: webnetwork` against a project where the actual name is `web-network`. Phase 5 builds the name map, lookup fails, emits a single `ErrValidatorRefUnknownComponent` listing the actual known names; output check is skipped (no target type to query).
2. **Walkthrough: wrong output.** An orders-db ref with `output: subnet_ids` against a compute component. Phase 5 resolves the target, gets its type (compute), checks `Outputs()`, doesn't find `subnet_ids`, emits `ErrValidatorRefUnknownOutput` listing what compute does declare.
3. **Walkthrough: cycle of length 2.** A refs B; B refs A. DFS marks A GRAY, visits B (now GRAY), visits A and sees GRAY → cycle. Reports `a → b → a` as the cycle path.
4. **Walkthrough: self-ref + cycle.** A refs A and B; B refs A. Self-ref emitted by per-ref pass. Cycle detection sees B→A→B and reports separately. (A→A self-loop suppressed in cycle path.)
5. **Walkthrough: refs against bad-type component.** Phase 4 already flagged the bad type. Phase 5 sees the target name exists (component is in the project) — component-existence check passes. Type lookup fails (per spec, registry.Type returns ok=false). Phase 5 silently skips the output check rather than emitting noise about an output on a non-existent type.
6. **Walkthrough: happy path.** full-stack-project fixture (4 components, ~6 refs total, no cycles) produces zero Phase 5 issues; tests stay green.

---

## Future hooks (not Phase 5)

- **Output Kind matching** — verify the consumer's `as:` name's expected type matches the producer's declared Kind (`string`, `list<string>`, etc.). Requires consumer to declare expected types, which the IR doesn't model yet.
- **Ref interpolation in spec strings** — `${component.foo.output.bar}` substitution before adapter Emit. Adjacent feature, separate phase.
- **Dynamic refs from inventory** — refs that resolve against live deployment state rather than declarative graph. Requires inventory-aware validation pass; not blocking for v1.
- **Per-target ref visibility** — a v2 feature where a ref can be scoped to specific (cloud, region) targets. Current model: refs are component-global; the adapter receives the same ResolvedRefs for every target.
