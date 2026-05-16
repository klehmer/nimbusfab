# Changelog

## Unreleased — Provisioner Phase 2

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
