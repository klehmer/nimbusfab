# nimbusfab

Multi-cloud Infrastructure-as-Code framework. Users declare infrastructure components (network, database, compute, storage, etc.) in YAML, target one or more clouds (AWS / Azure / GCP), and the framework generates and runs OpenTofu under the hood. Includes cost estimation and an actual-cost dashboard pulling from cloud billing APIs.

**Status:** pre-alpha. Architecture spec landed; DSL/IR Phase 1 (`nimbusfab validate`) merged; Provisioner Phase 1 (`nimbusfab plan` for AWS network) underway.

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

## License

Apache 2.0. See `LICENSE`.
