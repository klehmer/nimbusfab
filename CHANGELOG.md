# Changelog

## Unreleased — Validator Phase 5 (Cross-Component Refs)

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
