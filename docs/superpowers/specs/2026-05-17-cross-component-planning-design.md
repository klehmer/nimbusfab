# Cross-Component Planning — v1.1 Phase 1 Design

**Status:** Subsystem spec. Replaces nimbusfab's `data.terraform_remote_state` cross-component wiring with provisioner-bound tofu variables, adds topological ordering at plan and apply, and makes `nimbusfab plan` produce a usable artifact for multi-component projects without an apply round-trip.

**Date:** 2026-05-17

**Depends on:**
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (Plan/Apply orchestration; partial-failure policies)
- `docs/superpowers/specs/2026-05-15-dsl-ir-design.md` (`ir.ComponentRef` shape; validator Phase 5 cycle detection)
- `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` (`components.Type` registry; per-type adapter dispatch)

**Depended on by:**
- Future v1.1 user-test follow-on phases (Azure / GCP per-type adapter audits — those rely on `tofu plan` working end-to-end against multi-component fixtures).
- Composition spec (v2) — compositions expand to multiple components with refs between them; topological planning is a prerequisite.

---

## Context

User-test session #1 (2026-05-16) exercised `nimbusfab plan` against the full-stack-project fixture with real `tofu`. The single-component fixture (`network-only-project`) succeeded end-to-end. The full-stack fixture (4 components — network → compute → database → storage, with refs between them) did not: dependent components fail at `tofu plan` because the upstream's state file doesn't exist yet.

Three independent defects compound:

1. **Source order, not topological order.** `pkg/provisioner/plan.go` iterates `Project.Components` in YAML source order. A dependent declared above its upstream plans first and the upstream's workspace doesn't exist yet.

2. **Wrong state path.** Each (component, target) gets its own workspace under `<work-root>/<deployment-id>/<cloud>-<region>/<component>/`. The dependent's workspace contains a `data "terraform_remote_state" "<upstream>"` block with `backend: local, config: {}`. Tofu's `local` backend defaults to `./terraform.tfstate`, which means the **dependent's** workdir, not the upstream's — so even if the upstream had a state file, the dependent can't see it.

3. **No state file at plan time.** `terraform_remote_state` requires the file to exist when the data source is read (plan-time read). The `defaults` argument helps when the file exists but is missing a specific output — it does not help when the file itself is missing. Upstream state files do not exist until `apply`. So `nimbusfab plan` on a fresh project must — under the current scheme — either be re-shaped into interleaved plan→apply→plan, or stop relying on `terraform_remote_state`.

The clean fix is to stop relying on `terraform_remote_state` for cross-component wiring. The provisioner already knows everything about every workspace; it can resolve refs into tofu variables and bind values directly. `data.terraform_remote_state` is removed entirely.

## Design principles

1. **`nimbusfab plan` stays single-stage.** One CLI invocation produces one `PlanResult` over all targets, without applying. Plan is for preview; apply is for execution.
2. **Plan is structurally honest.** Resource counts, types, and shapes are real. Values that depend on upstream outputs are obvious placeholders; the diff reflects what will be created, not what the runtime values will be.
3. **Apply binds values from real state.** Dependent targets re-plan inline at apply time with `-var` values extracted from upstream state. Placeholder plan.bin files are never applied.
4. **The provisioner controls ordering.** Toposort happens once at plan time (deterministic component-level order) and again at apply time (deterministic target-level order). Adapters stay pure — they don't know they're inside a multi-component graph.
5. **Pairing is by (cloud, region).** A dependent target in `(aws, us-east-1)` reads outputs from the upstream target in `(aws, us-east-1)`. Cross-cloud or cross-region refs are explicitly unsupported in v1.1 and fail-fast at plan.

## Architecture

### Workspace shape change

The dependent's `main.tf.json` loses its `data "terraform_remote_state"` block. In its place:

```json
{
  "variable": {
    "upstream_web_network_subnet_ids": { "type": "list(string)" },
    "upstream_web_network_vpc_id":     { "type": "string" }
  },
  "resource": { ... emitted as today, but with var.* interpolations ... }
}
```

Adapters' emitted resource attributes substitute `${var.upstream_<component>_<output>}` (or `${var.upstream_<component>_<output>[0]}` for the singular-of-list case) instead of `${data.terraform_remote_state.<component>.outputs.<output>}`. This is a string change inside `buildResolvedRefs` — adapter resource-emit code is unaffected.

**Upstream workspaces emit `output` blocks.** For an upstream's `tofu apply` to write outputs into `terraform.tfstate` (so the dependent can read them), the upstream workspace must contain `output "vpc_id" { value = aws_vpc.foo.id }` blocks. Adapters declare these via a new method `OutputBindings(target, primitives) map[string]string` returning `{outputName: tofuExpression}`. The workspace renderer emits one tofu `output` block per entry. The mapping is per-adapter because the tofu expressions differ between AWS / Azure / GCP (e.g., `aws_vpc.foo.id` vs. `azurerm_virtual_network.foo.id`).

At plan time, the provisioner passes `-var "upstream_web_network_subnet_ids=[\"__nimbusfab_placeholder_upstream_web_network_subnet_ids_0__\"]"` (etc.) on the `tofu plan` command line. Values are typed-correctly via the `variable` block.

At apply time, the provisioner reads the upstream target's `terraform.tfstate`, extracts outputs, and passes them as `-var` flags to a fresh `tofu plan` against the dependent's workspace, then runs `tofu apply` against that plan.

### New package: `pkg/provisioner/upstream`

Single-purpose package with three responsibilities:

**Toposort.** Input: `[]ir.Component`. Output: ordered `[]ir.Component` such that for any ref `A → B`, B appears before A. Cycle detection is already handled by validator Phase 5; this package assumes its input is acyclic and returns an internal error if it isn't. Uses Kahn's algorithm; deterministic via stable secondary sort on component name.

**Pairing.** Input: dependent `(component, target)`, upstream component name, full list of upstream targets. Output: the upstream `ir.DeploymentTarget` with matching `(cloud, region)` — or `ErrCrossTargetRefUnsupported` if no match. The pairing is purely structural; no I/O.

**Output extraction.** Input: upstream workspace dir. Output: `map[outputName]any` parsed from `terraform.tfstate`. Reuses the state-bridge's existing JSON walker where possible; if state-bridge is too resource-focused, we add a small `outputs.go` that reads the top-level `outputs` field directly. Errors map to `ErrUpstreamStateUnreadable` and `ErrUpstreamOutputMissing`.

### `components.Type` gains `Outputs()`

The four v1 component types declare their outputs in code:

| Type     | Output           | Tofu type      |
|----------|------------------|----------------|
| network  | `vpc_id`         | `string`       |
| network  | `subnet_ids`     | `list(string)` |
| network  | `cidr`           | `string`       |
| compute  | `instance_ids`   | `list(string)` |
| compute  | `private_ips`    | `list(string)` |
| database | `endpoint`       | `string`       |
| database | `port`           | `number`       |
| storage  | `bucket_name`    | `string`       |
| storage  | `bucket_arn`     | `string`       |

This list is the contract validator Phase 5 already uses for "unknown output" checks (`Type.Outputs()`); we extend each output to carry a `TofuType` string. The corresponding `output {}` blocks are emitted by adapters via the new `OutputBindings` method (see "Workspace shape change" above). Adapter audit confirms every (cloud × component-type) combination produces all declared outputs.

### Plan flow

```
provisioner.Plan(input)
  ├── validate refs (existing Phase 5)
  ├── topo := upstream.Toposort(input.Project.Components)
  ├── for comp in topo:
  │     for target in comp.Targets:
  │       if !matchesFilter(...) continue
  │       refs := buildResolvedRefs(comp.Refs)              // now emits ${var.*}
  │       placeholders := upstream.PlanPlaceholders(comp.Refs, project)
  │       primitives := adapter.Emit(target, refs)
  │       outputs := adapter.OutputBindings(target, primitives)
  │       layout := WorkspaceLayout{ ..., Variables: varDecls, OutputBindings: outputs }
  │       WriteWorkspace(layout)
  │       Runner.Init(ws)
  │       Runner.Plan(ws, PlanOpts{Vars: placeholders, OutFile: plan.bin})
  │       append TargetPlan to result
  └── return result
```

`upstream.PlanPlaceholders` walks the component's refs, looks up each upstream component's `Outputs()`, and produces typed placeholder values:
- `string` → `"__nimbusfab_placeholder_<varname>__"`
- `list(string)` → `["__nimbusfab_placeholder_<varname>_0__"]`
- `number` → `0`
- `bool` → `false`

Length-1 list placeholders are sufficient because the `[0]` indexing heuristic in `buildResolvedRefs` is the only way a list value gets subscripted today.

### Apply flow

```
provisioner.Apply(deploymentID, opts)
  ├── targets := inventory.ListDeploymentTargets(deploymentID)
  ├── topo := upstream.ToposortTargets(targets, project)
  │       // orders targets so all upstreams of comp X in (cloud, region) Y
  │       // come before X in (cloud, region) Y
  ├── for target in topo:
  │     if any upstream(target) failed:
  │       mark target Status="blocked"; continue
  │     upstreams := upstream.Pair(target, project, targets)
  │     vars := {}
  │     for u in upstreams:
  │       state := readState(u.WorkspaceDir + "/terraform.tfstate")
  │       outputs := upstream.ExtractOutputs(state)
  │       vars[varName(u, output)] = outputs[output]
  │     Runner.Plan(ws, PlanOpts{Vars: vars, OutFile: plan.bin})  // re-plan with REAL values
  │     Runner.Apply(ws, ApplyOpts{Vars: vars, PlanFile: plan.bin})
  │     persist target state, status
  └── return result
```

The existing partial-failure policies (leave / rollback / retry-failed) cover what happens to the **failed** target and its peers. New rule: dependent targets of a failed upstream are marked `"blocked"` (not `"failed"`) and skipped without running tofu. The CLI surfaces blocked targets distinctly in the apply summary so the user can see "5 succeeded, 1 failed, 3 blocked downstream."

`destroy` runs the reverse toposort (downstream first). `drift` plans each target in toposort order with real `-var` values from current state (no placeholders — drift is post-apply by definition).

## Components

### `pkg/provisioner/upstream` (new)

- `Toposort(components []ir.Component) ([]ir.Component, error)`
- `ToposortTargets(targets []TargetPlan, project *ir.Project) ([]TargetPlan, error)` — orders by (component-toposort-rank, cloud, region) for stable apply
- `Pair(dep TargetPlan, upstream string, all []TargetPlan) (TargetPlan, error)`
- `PlanPlaceholders(refs []ir.ComponentRef, project *ir.Project) (map[string]string, error)` — returns `{varName: "<json-encoded placeholder>"}` for `-var` flag formatting
- `VarName(componentName, outputName string) string` — `upstream_<tofuIdent(component)>_<output>`
- `ExtractOutputs(state []byte) (map[string]any, error)`

### `pkg/provisioner/refs.go` (modified)

`buildResolvedRefs` rewrites the right-hand side from `${data.terraform_remote_state.X.outputs.Y}` to `${var.upstream_X_Y}`. Keeps the `[0]` indexing heuristic for the singular-of-list case. Returns the same `ResolvedRefs` shape; adapters are unaffected.

### `pkg/provisioner/workspace.go` (modified)

- `WorkspaceLayout` gains:
  - `Variables []UpstreamVariable` (replacing `UpstreamRefs`).
  - `OutputBindings map[string]string` — `{output_name: tofu_expression}` for the upstream side.
- `UpstreamVariable` = `{Name, TofuType}`. Values are never baked into the workspace file; they always flow through `-var` flags so the same on-disk workspace can be re-planned with different values.
- `WriteWorkspace` emits top-level `variable {}` blocks (one per `Variables` entry) and `output {}` blocks (one per `OutputBindings` entry). The `data.terraform_remote_state` rendering branch is deleted.

### `pkg/provisioner/plan.go` and `apply.go` (modified)

- `Plan` calls `upstream.Toposort` before iterating, and `upstream.PlanPlaceholders` per dependent.
- `Apply` calls `upstream.ToposortTargets`, then per-target reads upstream state, builds the `-var` map, re-plans, applies.
- `Destroy` runs the reverse toposort.
- `Drift` runs the same forward toposort, with real values (no placeholders).

### `internal/tofu` (modified)

`PlanOpts.Vars`, `ApplyOpts.Vars`, `DestroyOpts.Vars` — `map[string]string` where the value is a tofu-CLI-formatted literal (e.g. `"abc"` for strings, `["a","b"]` for lists). `ExecRunner` flattens to `-var name=value` repeated flags. `FakeRunner` stores the map for assertions.

### `pkg/components` (modified)

`Type` interface gains `Outputs() []Output`. `Output = {Name, TofuType}`. The four v1 implementations declare their outputs as a static slice. Validator Phase 5's existing `Outputs()` call site is updated to use the new return shape (it currently expects `[]string`).

### `pkg/cloud.Adapter` (modified)

New method: `OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error)`. Returns the tofu expression for each output declared by the component's `Type.Outputs()`. Adapters are responsible for knowing which primitive carries the output value (e.g. AWS network's `vpc_id` comes from the `aws_vpc` primitive's tofu local name; `subnet_ids` is a list comprehension over `aws_subnet` primitives).

### `pkg/errs` (modified)

New codes:
- `ErrCrossTargetRefUnsupported` — pairing failure at plan time.
- `ErrUpstreamOutputMissing` — apply-time output not present in upstream state.
- `ErrUpstreamStateUnreadable` — apply-time state file unreadable or malformed.
- `ErrUpstreamApplyBlocked` — informational; surfaces in TargetPlan.Status as `"blocked"`.

### `inventory` schema (unchanged)

The existing `deployment_targets.status` column already accepts free-form strings. We add `"blocked"` as a recognized value at the application layer. No migration needed.

## Data flow

```
       project YAML
            │
            ▼
       loader → IR
            │
            ▼
       validator (Phase 5: cycle detection unchanged)
            │
            ▼
       provisioner.Plan
            │
            ├──▶ upstream.Toposort(components)
            │        ▼
            │   ordered components
            │
            ├──▶ for each (component, target):
            │       │
            │       ├──▶ buildResolvedRefs(comp.Refs)         → ${var.*} strings
            │       ├──▶ upstream.PlanPlaceholders            → {varName: typed-placeholder}
            │       ├──▶ adapter.Emit(target, refs)           → primitives
            │       ├──▶ adapter.OutputBindings(target, prim) → {outputName: tofuExpr}
            │       └──▶ WriteWorkspace(layout w/ Variables, OutputBindings)
            │              ▼
            │           variable + resource + output blocks on disk
            │
            └──▶ Runner.Plan(ws, Vars=placeholders) → plan.bin (placeholder values)
                     ▼
                  TargetPlan with HasChanges, Adds, etc.

       provisioner.Apply
            │
            ├──▶ upstream.ToposortTargets(targets)
            │        ▼
            │   ordered targets
            │
            └──▶ for each target:
                   │
                   ├──▶ check upstream statuses → "blocked" if any failed
                   ├──▶ for each upstream of this target:
                   │       │
                   │       ├──▶ read upstream workspace's terraform.tfstate
                   │       └──▶ upstream.ExtractOutputs → values
                   │
                   ├──▶ Runner.Plan(ws, Vars=REAL values) → plan.bin (real)
                   └──▶ Runner.Apply(ws, plan.bin)
                          ▼
                       target state persisted
```

## Error handling

### Plan-time errors

| Condition | Behavior |
|-----------|----------|
| Dependent's `(cloud, region)` has no matching upstream target | `ErrCrossTargetRefUnsupported`; plan fails for that target; existing `PartialFailureLeave` lets remaining targets continue. |
| Upstream component declares an output the validator already validated, but the upstream's adapter doesn't emit it | Caught at apply time as `ErrUpstreamOutputMissing`. (Validator Phase 5 cannot check adapter emit fidelity; the integration test backstops it.) |
| Cycle in component refs | Already handled by validator Phase 5. Provisioner trusts validator output; internal-error if toposort sees a cycle. |
| Type mismatch between upstream output and consumer expectation | Tofu rejects the `-var` value at plan time with a parse error; the runner surfaces this as a target plan failure. |

### Apply-time errors

| Condition | Behavior |
|-----------|----------|
| Upstream target failed in this apply | Dependent target marked `"blocked"`, not run. Surfaced in CLI summary. |
| Upstream state file missing | `ErrUpstreamStateUnreadable`. Fails this target. |
| Upstream state file malformed JSON | `ErrUpstreamStateUnreadable`. Fails this target. |
| Upstream state file present, expected output absent | `ErrUpstreamOutputMissing`. Fails this target. |
| Re-plan succeeds, apply fails | Existing partial-failure policy handles. |

### Drift-time errors

Drift extracts outputs from the **current** state (post-apply, by definition exists). If a target was never applied, drift skips it with `Status="pending"`. Drift never uses placeholders.

## Testing

### Unit tests

- `upstream.Toposort`: empty, single-component, linear chain, fanout (A→B, A→C), fanin (A→C, B→C), diamond (A→B, A→C, B→D, C→D). Stable ordering across runs.
- `upstream.ToposortTargets`: per-(cloud, region) sub-ordering; cross-cloud refs flagged before sort runs.
- `upstream.Pair`: exact match, no match (cross-region), no match (cross-cloud), multiple components sharing target identity.
- `upstream.VarName`: hyphenated component names, all-numeric prefix, edge characters.
- `upstream.PlanPlaceholders`: string, list(string), number, bool, missing output type defaults.
- `upstream.ExtractOutputs`: well-formed state, missing outputs key, malformed JSON, output with sensitive=true.
- `buildResolvedRefs`: `${var.*}` rewriting, `[0]` heuristic preserved.
- `components.Type.Outputs`: each v1 type declares the documented outputs.

### Provisioner tests

- Plan over a 2-component fixture (network → compute, single cloud): dependent workspace contains correct `variable` blocks; FakeRunner captures `-var` flags with placeholder values; toposort places network first.
- Apply over the same fixture with a FakeRunner that injects a known state file after network applies: compute's apply receives real `-var` values, not placeholders; toposort places network's apply first.
- Apply with `PartialFailureLeave`: network fails → compute marked `"blocked"`.
- Apply with `PartialFailureRollback`: network fails → compute marked `"blocked"`, rollback policy applies to the network only (downstream wasn't run).
- Plan with `ErrCrossTargetRefUnsupported`: compute target in `us-east-2` referencing network only in `us-east-1` → fails at plan with the error code.

### Integration tests (`-tags=integration`)

Extends `cmd/cli/integration_validate_test.go` (or adds a sibling `integration_plan_test.go`):

- Renders the full-stack-project fixture with real `tofu`.
- Runs `tofu init` per target.
- Runs `tofu plan` per target with placeholder vars.
- Asserts: every target's plan succeeds (current state: AWS plan succeeds, Azure / GCP plans need real creds per [v1 user-test memory]; gate Azure/GCP plan tests on `NIMBUSFAB_SECRET_*` env vars).
- Asserts: dependent target's plan output contains the expected resource shape (e.g., compute has `aws_instance.<n>` with `subnet_id = "__nimbusfab_placeholder_..."` in the diff).

### CLI tests

- `nimbusfab plan testdata/full-stack-project` succeeds end-to-end with FakeRunner.
- `nimbusfab apply <id>` over the same fixture, with FakeRunner injecting state files between component layers, completes all 12 targets in topological order.

## Migration

No data migration. Existing deployments in inventory predate this work — they were created with FakeRunner-only paths, so their `terraform.tfstate` files are placeholders themselves. New deployments use the new flow from creation.

## Non-goals (deferred to v1.2+)

- **Cross-cloud refs.** Dependent in Azure consuming AWS network outputs. Possible but requires reasoning about cross-cloud value semantics (does an AWS subnet ID mean anything in Azure?). Out of scope.
- **Cross-region refs within a cloud.** Same logic — `subnet_id` from `us-east-1` is meaningless in `us-east-2`. Out of scope.
- **Cross-stack refs.** Already deferred per architecture spec.
- **Remote backends for cross-component state.** Local backend is the only supported state backend for ref-using components in v1.1. Remote-backend support requires either reading state via the cloud SDK (S3 / Azure Blob / GCS) or surfacing per-target backend configs to the upstream-extraction step. Documented as a v1.2 follow-on.
- **Composition expansion.** Multi-component compositions surface the same ref problem at a higher level. The toposort infrastructure here is the prerequisite — composition expansion happens in the composition spec.
- **Background drift detection over multi-component projects.** Drift cron is v1.1+; this design supports the per-component drift call but the cron scheduler is out of scope.

## Implementation phasing

Single phase: **Cross-Component Planning Phase 1.**

Estimated implementation tasks (~14, mirroring prior phases):

1. `pkg/provisioner/upstream` package skeleton + `VarName` + `Toposort` + unit tests.
2. `components.Type.Outputs()` interface extension + v1 type implementations + validator Phase 5 call-site update + tests.
3. `upstream.PlanPlaceholders` + unit tests.
4. `upstream.Pair` + `upstream.ToposortTargets` + unit tests.
5. `internal/tofu`: `Vars` field on `PlanOpts` / `ApplyOpts` / `DestroyOpts`; `ExecRunner` flag rendering; `FakeRunner` capture; tests.
6. `pkg/cloud.Adapter.OutputBindings` interface method + AWS / Azure / GCP implementations across all four v1 component types + tests.
7. `pkg/provisioner/workspace.go`: replace `UpstreamRefs` with `Variables`; emit `variable` and `output` blocks; delete `data.terraform_remote_state` branch.
8. `pkg/provisioner/refs.go`: rewrite `buildResolvedRefs` to `${var.*}`; existing tests get updated assertions.
9. `pkg/provisioner/plan.go`: integrate toposort + placeholders + `OutputBindings` wiring; tests.
10. `pkg/provisioner/upstream.ExtractOutputs` + state-bridge integration; tests over real tofu state samples.
11. `pkg/provisioner/apply.go`: toposort, re-plan with real vars, blocked-target handling; tests.
12. `pkg/provisioner/destroy.go`: reverse-toposort; tests.
13. `pkg/provisioner/drift.go`: forward toposort with real vars; tests.
14. Integration test: `tofu plan` against full-stack-project across AWS targets. CLI test: 12-target apply through FakeRunner.

## Open questions

None that block this phase. Possible v1.2 items surfaced:
- Should remote-backend support layer in via a `cloud.Adapter` method for "read state from your own backend"? (Plausible — the AWS adapter could read S3 directly with the same credentials it already has.)
- Should compute → database refs trigger a security-group rule auto-generation? (User-facing convenience; explicitly out of scope for this phase.)
