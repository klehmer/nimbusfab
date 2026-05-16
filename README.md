# nimbusfab

Multi-cloud Infrastructure-as-Code framework. Users declare infrastructure components (network, database, compute, storage, etc.) in YAML, target one or more clouds (AWS / Azure / GCP), and the framework generates and runs OpenTofu under the hood. Includes cost estimation and an actual-cost dashboard pulling from cloud billing APIs.

**Status:** pre-alpha. Architecture spec landed; DSL/IR Phase 1, Provisioner Phases 1–2, Inventory Persistence Phase 1, AWS Expansion Phase 3, Parity Engine Phase 1, Cost Estimator Phase 1, Azure Adapter Phase 4, GCP Adapter Phase 5, Validator Phase 4 (per-type spec schemas), Validator Phase 5 (cross-component refs), Secrets Phase 1 (env-var + file credential backends), and Web App UI Phase 1 (read-only browser pages over SQLite inventory at `/ui/projects`, `/ui/deployments/{id}`, `/ui/runs/{id}`) merged. `credentialRef: aws-dev` now resolves through `NIMBUSFAB_SECRET_AWS_DEV` env var (JSON) or `~/.nimbusfab/secrets/aws-dev.json`; the resolved keys flow into the Tofu runner's per-command env so cloud providers authenticate. Three-cloud projects work end-to-end: a project targeting `[aws, azure, gcp]` produces per-cloud Tofu workspaces, the parity engine reports real cross-cloud SKU divergence with 3-way weighted scores, and the cost estimator shows per-cloud subtotals.

## Design

See `docs/superpowers/specs/2026-05-14-architecture-design.md` for the full architecture: module boundaries, the IR (intermediate representation), public interface contracts, data flow, and the v2 plugin / GitOps roadmap.

## Layout

| Path | Purpose |
|---|---|
| `pkg/ir` | Intermediate representation: Go types, JSON Schema, versioning. |
| `pkg/engine` | Top-level `Engine` interface used by all frontends. |
| `pkg/cloud` | `Adapter` interface — the per-cloud plugin contract. |
| `pkg/components` | Component-type registry. |
| `pkg/composition` | User-defined Composition expansion. |
| `pkg/inventory` | Inventory DB repository interfaces and schema. |
| `pkg/cost/estimator` | Pre-deploy cost estimation. |
| `pkg/cost/collector` | Polls cloud billing APIs for actual costs. |
| `pkg/cost/pricing` | Pricing-cache interfaces. |
| `pkg/secrets` | Pluggable secrets backends (env, file, Vault). |
| `pkg/plugin/contract` | Adapter contract test suite (run against every adapter). |
| `internal/tofu` | Subprocess wrapper around the `tofu` CLI. |
| `internal/dsl/{loader,validator}` | YAML loading and validation. |
| `internal/state/bridge` | Reconcile OpenTofu state JSON with inventory. |
| `internal/cloud/{aws,azure,gcp}` | In-tree cloud adapters. |
| `internal/inventory/{sqlite,pg}` | Inventory DB implementations. |
| `internal/webapi` | HTTP server. |
| `internal/webauth` | OIDC + local users. |
| `cmd/cli` | The CLI (`nimbusfab`). |
| `cmd/server` | The web backend. |

## Build & run

Requires Go 1.22+ and `tofu` 1.7+ on `PATH`.

```bash
make build              # build the CLI and server binaries into ./bin/
make test               # run unit tests
make test-integration   # run unit + integration tests (needs `tofu` on PATH)
make lint               # gofmt + go vet
```

## Commands

### `nimbusfab validate [path]`

Validate a project directory of YAML files. Runs the loader + validator phases
1–3 (YAML well-formedness, APIVersion check, JSON Schema validation) and
prints a structured report. Exit codes: 0 OK, 1 validation failed, 2 validator crash.

### `nimbusfab plan --stack <stack> [path]`

Reads the project, validates it, then asks each cloud adapter to emit Tofu
primitives for every `DeploymentTarget`. Writes canonical workspace files
(`provider.tf.json`, `backend.tf.json`, `versions.tf.json`, `main.tf.json`)
into a per-target directory under `$TMPDIR/nimbusfab/<deployment-id>/`, runs
`tofu init && tofu plan -out plan.bin`, and prints a summary.

**Phase 1 scope:** AWS only; `network` component type only (emits one
`aws_vpc` per target). Other clouds and component types arrive in subsequent
phases.

### `nimbusfab apply [deployment-id | path]`

With a deployment ID: looks the plan up in the inventory and applies it.
Without: requires `--stack`, plans+applies in one shot. Partial-failure
policies via `--partial-failure {leave|rollback|retry-failed}`.

### `nimbusfab destroy [deployment-id | path]`

Same shape as apply. With a deployment ID, tears down the recorded
deployment; without, plans+destroys against `--stack`.

### `nimbusfab drift [deployment-id | path]`

Same shape. With a deployment ID, runs `tofu plan -refresh-only` against
the recorded workspaces and upserts `drift_status` rows.

## Component types

Phase 3 ships four v1 types, registered automatically via
`components.DefaultRegistry()`:

| Type | AWS primitives | Outputs |
|---|---|---|
| `network` | VPC + IGW + RT + N subnets + RT associations | `vpc_id`, `subnet_ids`, `route_table_ids` |
| `compute` | Security group + N EC2 instances (T-shirt sized) | `instance_ids`, `private_ips`, `security_group_id` |
| `database` | DB subnet group + RDS instance (T-shirt sized; postgres/mysql/mariadb) | `endpoint`, `port`, `db_name` |
| `storage` | S3 bucket + versioning + public-access-block + SSE | `bucket_name`, `bucket_arn`, `bucket_url` |

See `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` for
per-type spec schemas, T-shirt size resolution, and the `PricingKey` /
`Profile` shapes the cost estimator + parity engine consume.

### `nimbusfab parity --stack <stack> [path]`

Renders a parity report per component: contract floor, per-cloud values,
weighted parity score, rule-violation summary (parity.yaml when present).
Single-cloud reports score 1.0 trivially; once Azure / GCP land, real
divergence surfaces here. Contract floors in
`pkg/parity/contracts/*.yaml` validate that adapter choices satisfy
T-shirt minimums.

### `nimbusfab cost estimate --stack <stack> [path]`

Computes per-primitive monthly cost estimates from the bundled AWS price
snapshot (`pkg/cost/pricing/snapshot/aws.json`). Output: target totals
+ per-primitive line items (`$0.0416 × 730 Hrs = $30.37` for a t3.medium).
Compute / database default to 730 hr/month; storage defaults to 100 GB-Mo
(override via `spec.usage.hoursPerMonth` / `spec.usage.storageGB`).
Live AWS Pricing API integration lands in Cost Phase 2.

## Inventory

Inventory Phase 1 ships a SQLite-backed inventory that persists every
Plan / Apply / Destroy / Drift across processes. By default
`~/.config/nimbusfab/inventory.db` is used; override with
`--inventory-dsn sqlite:///path/to/inventory.db`, or disable entirely with
`--no-inventory` (useful in CI).

In inventory mode, `nimbusfab plan` returns a Deployment ID. The deployment
+ per-target + per-target plan-run rows are committed before the command
returns. `apply <deployment-id>` (possibly from a different shell, days
later) deploys it; `destroy <deployment-id>` tears it down; `drift
<deployment-id>` reports drift. Postgres support is a future phase; the
contract is shared.

## License

Apache 2.0. See `LICENSE`.
