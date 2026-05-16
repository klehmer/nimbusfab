# Cost Estimator Subsystem Spec

**Status:** Subsystem spec. Defines the cost-estimation pipeline — from `Adapter.PricingKey()` calls through pricing-cache lookups (bundled snapshot or live API), usage-assumption multiplication, per-target aggregation, and `Estimate` tree output. The estimator never invokes Tofu, never writes inventory, and never inspects cloud-specific resource details: every cloud-specific input flows through PricingKey + adapter Profile.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (§9 cost data path)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (`Adapter.PricingKey()` contract)
- `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` (AWS-specific PricingKey shapes)

**Depended on by:**
- Cost dashboard spec (consumes both estimates and cost_actuals from this subsystem and the future collector subsystem)
- Web app spec (shows estimates in the plan diff UI)
- Inventory Phase 2 (`cost_estimates` table writes from estimator output)

---

## Context

Provisioner Phase 1 + 2 produce plans; AWS Phase 3 populates `Adapter.PricingKey()` with structured maps the AWS Price List API can consume. The estimator turns those keys into actual prices via a pricing cache and multiplies by per-primitive usage assumptions (730 hr/month for compute, declared `sizeGB` for storage, etc.). Result: a tree-shaped `Estimate` showing total monthly cost broken down per target, per primitive.

Phase 1 ships a **bundled snapshot only** — no live AWS Price List API calls. The snapshot is a JSON file under `internal/pricing/snapshot/` covering the AWS instance types, RDS instance classes, and S3 storage classes that Phase 3 actually emits. When the user adds Azure or GCP, those clouds add their own snapshot files. The snapshot format is shared across clouds; lookup is by canonical key.

Live pricing API integration lands later (Phase 2 of cost). The cache interface (`pkg/cost/pricing.Cache`) already declares both `Lookup` and `Refresh`; Phase 1 implements Lookup against the snapshot and stubs Refresh.

**Design principles:**
1. **Pure functions where possible.** `Estimator.Estimate()` is pure given a snapshot — no clocks, no network. Snapshot freshness is metadata.
2. **Snapshot is authoritative for v1.** Users don't pay AWS API fees for plan-time pricing. Snapshot is curated, versioned with releases, and surfaces `fetchedAt` so users know how stale it is.
3. **Pricing data is normalized.** Every snapshot entry is `(cloud, normalized-key, unit-price, unit-of-measure, currency)`. Cloud-specific quirks (multiple SKU dimensions in AWS, e.g.) flatten to a single canonical key string at snapshot-build time.
4. **Usage assumptions are explicit.** `730 hrs/month` for compute is in code, not hidden. Users can override per-component via `spec.usage`.
5. **Estimates are advisory.** Plan/Apply never blocks on cost. The estimator surfaces "missing pricing data" as a warning, not an error — a Plan with one unpriced primitive still produces an Estimate for everything else.

---

## Scope

**In scope (this spec):**
- `pkg/cost/estimator.Estimator` implementation: walks `EstimateInput.Targets`, calls adapter.PricingKey per primitive, looks up via `PricingProvider`, applies usage assumptions, aggregates per-target and total.
- `pkg/cost/pricing.Cache` implementation: bundled-snapshot reader; `Lookup` checks in-memory map; `Refresh` returns ErrNotImplementedYet.
- Bundled snapshot file format: JSON, one file per cloud, embedded via `//go:embed`. Schema documented below.
- Snapshot key canonicalization: how `PricingKey` map → canonical key string.
- Per-primitive usage assumption table: hardcoded defaults per AWS primitive type, overridable via `EstimateInput.TargetInput.Usage`.
- Engine integration: `Engine.EstimateCost(plan)` calls the estimator with adapters from the registry.
- CLI integration: cost summary in `nimbusfab plan` output; new `nimbusfab cost estimate --stack <stack>` command for detailed breakdown.
- Curation process: how new snapshot rows are added (manual curation in Phase 1; live-fetch tool in Phase 2).
- Currency handling: all snapshot prices in USD for v1; multi-currency reserved.
- Period handling: monthly by default; hourly available via `--period hour` CLI flag (later phases).
- Aggregation by component, by cloud, by target — surfaced as flat list in Phase 1 (`Estimate.Targets`); web app spec adds richer aggregations.

**Out of scope (deferred):**
- Live AWS Price List API integration. Reserved in the `Cache.Refresh` method but returns `ErrNotImplementedYet`. Phase 2 of cost.
- Azure / GCP pricing snapshots. Schema supports them; data ships when those adapters ship.
- Cost actuals collection (`pkg/cost/collector` → billing API polling → `cost_actuals` table). Separate Cost Collector spec.
- Inventory writes (`cost_estimates` table). Inventory Phase 2 or web app phase.
- Free-tier / committed-use / reserved-instance pricing. v1 quotes on-demand only.
- Savings plans, spot pricing, data transfer costs. v2.
- Cross-region pricing nuance — Phase 1 uses the region from `PricingKey`; missing regions fall through to a documented fallback.
- Cost optimization recommendations ("you'd save $X by switching to t3.small"). v2.

---

## Module layout

```
pkg/cost/estimator/
  estimator.go         # (existing) types: Estimator, EstimateInput, Estimate, etc.
  runtime.go           # NEW — concrete Estimator impl (Phase 1 wires this)
  usage.go             # NEW — per-primitive usage assumption defaults
  runtime_test.go      # NEW
  usage_test.go        # NEW

pkg/cost/pricing/
  pricing.go           # (existing) Cache + Entry types
  cache.go             # NEW — concrete Cache impl backed by snapshot
  snapshot.go          # NEW — embedded snapshot loader + canonical key derivation
  cache_test.go        # NEW
  snapshot/            # NEW — embedded snapshot JSON files
    aws.json
    README.md          # curation process

pkg/engine/
  cost.go              # NEW — Engine.EstimateCost wiring

cmd/cli/
  cost.go              # NEW — `nimbusfab cost estimate` command
  cost_test.go         # NEW
```

`pkg/cost/estimator` is the public surface; `pkg/cost/pricing` provides the cache. The estimator depends only on the cache interface, not the concrete cache, so tests can substitute a fake cache trivially.

---

## Public surface

### `Estimator` (locked from existing scaffold)

```go
type Estimator interface {
    Estimate(ctx context.Context, in EstimateInput) (Estimate, error)
}
```

Existing `EstimateInput`, `TargetInput`, `Estimate`, `TargetEstimate`, `PrimitiveEstimate`, `PricingProvider`, `UnitPrice` types stay as-is (see `pkg/cost/estimator/estimator.go`). Phase 1 ADDS:

```go
// New returns a runtime Estimator wired against the supplied PricingProvider.
// Usually called with pricing.NewCache() output, but tests can pass any
// PricingProvider implementation.
func New(provider PricingProvider) Estimator
```

### `Cache` (locked from existing scaffold)

```go
type Cache interface {
    Lookup(ctx context.Context, cloudName string, key map[string]any) (Entry, error)
    Refresh(ctx context.Context, cloudName string, keys []map[string]any) error
}
```

Phase 1 ADDS:

```go
// NewCache returns a Cache backed by the embedded snapshots. Refresh returns
// ErrNotImplementedYet in Phase 1.
func NewCache() Cache

// AsPricingProvider adapts a Cache to the estimator's PricingProvider interface.
// (The two interfaces are intentionally distinct — Cache is a storage concern;
// PricingProvider is a query concern. The adapter is a one-liner.)
func AsPricingProvider(c Cache) estimator.PricingProvider
```

The split between `Cache` and `PricingProvider` mirrors the spec's "the estimator never knows about the cache" principle.

---

## Snapshot file format

One JSON file per cloud, embedded via `//go:embed` from `pkg/cost/pricing/snapshot/`.

```json
{
  "$schema": "https://nimbusfab.dev/schema/pricing/snapshot/v1alpha1.json",
  "cloud": "aws",
  "currency": "USD",
  "fetchedAt": "2026-05-16T00:00:00Z",
  "source": "manual-curation-phase-1",
  "entries": [
    {
      "key": {
        "service": "AmazonEC2",
        "instanceType": "t3.small",
        "region": "us-east-1",
        "tenancy": "Shared",
        "operatingSystem": "Linux",
        "preInstalledSw": "NA",
        "capacitystatus": "Used"
      },
      "unitPrice": 0.0208,
      "unitOfMeasure": "Hrs"
    },
    ...
  ]
}
```

Notes:
- `key` is the same shape `Adapter.PricingKey()` returns. Lookup canonicalizes both sides (sort map keys, drop empty fields) and compares as JSON strings.
- `unitPrice` is the on-demand price for one `unitOfMeasure` unit in the snapshot's `currency`.
- `fetchedAt` is the snapshot file's claimed freshness — surfaced in `Entry.FetchedAt` so the CLI can warn ("this estimate uses pricing data 90 days old").
- `source` is freeform metadata for debugging.

Phase 1 snapshot rows cover what AWS Phase 3 actually emits:

| Service | Keys covered |
|---|---|
| EC2 | `t3.small`, `t3.medium`, `t3.large`, `t3.xlarge`, `m6i.large`, `m6i.xlarge` × {us-east-1, us-east-2, us-west-1, us-west-2, eu-west-1, eu-central-1, ap-northeast-1, ap-southeast-2} × Linux |
| RDS | `db.t3.small`, `db.t3.medium`, `db.m6i.large`, `db.m6i.xlarge` × same regions × {postgres, mysql, mariadb} × {Single-AZ, Multi-AZ} |
| S3 | Standard storage class × same regions |

That's ~60 EC2 + ~96 RDS + ~8 S3 = ~164 entries. Manually curated from AWS public pricing docs; refreshed each minor release.

---

## Canonical key derivation

`PricingKey()` returns a `map[string]any`. To use this as a cache key, the cache flattens it deterministically:

1. Drop fields with empty-string or nil values.
2. Stringify each value (`%v` for non-strings).
3. Sort by key alphabetically.
4. Join as `k1=v1;k2=v2;...`.

Example:

```
service=AmazonEC2;instanceType=t3.medium;region=us-east-1;tenancy=Shared;operatingSystem=Linux;preInstalledSw=NA;capacitystatus=Used
```

Snapshot entries undergo the same flattening at load time. Lookup is then a single map `map[string]Entry` keyed by the flattened string per cloud.

---

## Usage assumptions

Per the architecture spec § "Cost estimation & dashboard data path", multiplication factors:

| Primitive Tofu type | Unit | Default |
|---|---|---|
| `aws_instance` | Hrs | 730 (one month, 24×30 hrs ≈ 730.5) |
| `aws_db_instance` | Hrs | 730 |
| `aws_s3_bucket` | GB-Mo | 100 (assumed average; user overrides via `spec.usage.storageGB`) |
| (other AWS Phase 3 primitives) | n/a | not priced |

User overrides come through `EstimateInput.TargetInput.Usage`. Phase 1 honors:
- `Usage["hoursPerMonth"]` — overrides 730 for compute/database.
- `Usage["storageGB"]` — overrides the 100 GB default for S3.

The estimator emits a warning when a primitive has a pricing key but no usage assumption is documented (currently impossible since EC2/RDS/S3 cover all priced primitives, but the warning code path stays).

---

## Engine + CLI integration

### Engine

```go
// pkg/engine/cost.go
func (e *runtimeEngine) EstimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error) {
    // Build EstimateInput from plan.Targets, looking up adapter per cloud.
    // Call estimator.Estimate, wrap the result into the engine's CostEstimate type.
}
```

The existing `engine.CostEstimate` shape stays (currency, period, total, targets, warnings); the engine method's job is to translate between `pkg/cost/estimator.Estimate` and `engine.CostEstimate`.

### CLI

Two surfaces:

**1. Cost summary in `plan` output.** After the parity block, plan prints:

```
Cost:
  Total estimated: $X.XX/month
  Breakdown:
    web-app/aws/us-east-1   $14.96/mo
    orders-db/aws/us-east-1 $62.05/mo
  Warnings: snapshot 0 days old, USD only
```

**2. `nimbusfab cost estimate --stack <stack>`.** Dedicated detailed view: per-primitive line items, group-by-cloud / group-by-component totals, snapshot age warning. `--format json` available for piping to other tools.

Existing CLI commands remain unchanged.

---

## Error model

| Code | Origin | Meaning |
|---|---|---|
| `ErrPricingMissing` | cache | Lookup found no snapshot entry for the key. Estimator records as warning (not error). |
| `ErrPricingStale` | cache (informational) | Snapshot older than 90 days. Surfaced as Estimate.Warnings, not blocking. |
| `ErrNotImplementedYet` | cache.Refresh | Phase 1 doesn't do live fetching. |
| `ErrAdapterMissing` | engine.EstimateCost | Plan references a cloud with no registered adapter — shouldn't happen post-Plan but defensive. |
| `ErrPricingKeyMalformed` | estimator | Adapter returned a non-map[string]any; never happens with v1 adapters but defended. |

Errors propagate to `Estimate.Warnings`; the estimator never fails outright on missing pricing data — the goal is "best-effort cost insight, not block deployment."

---

## Curation process (manual for v1)

`pkg/cost/pricing/snapshot/README.md` documents:

1. **Adding a new EC2 instance type to the snapshot.** Open AWS On-Demand Pricing page → copy the per-region per-OS price → add an entry per region. Sanity-check against the AWS Pricing API (curl) for one region.
2. **Refreshing existing prices.** Quarterly: re-fetch all entries via the AWS Pricing API (`aws pricing get-products` queries documented). Commit the regenerated `aws.json` with an updated `fetchedAt`.
3. **Verification.** A `tools/pricing/verify/` Go program (not in Phase 1; placeholder) will diff snapshot vs live prices and flag stale rows. Phase 1 stays manual.

Snapshot updates ship as part of normal releases; no breaking-API impact.

---

## Determinism

- For the same plan + same snapshot, `Estimate()` returns byte-identical output. Map iteration is sorted; floating-point arithmetic is straightforward multiplication + summation.
- Per-target estimates appear in the same order as `EstimateInput.Targets` (caller-controlled). Per-primitive estimates within a target appear in the same order as the primitives slice.

CI asserts determinism for the Phase-3 full-stack fixture: same plan → same `Estimate`.

---

## Verification

Design-level checks:

1. **EC2 PricingKey shape.** AWS Phase 3's `PricingKey` for an `aws_instance` returns keys `service / instanceType / region / tenancy / operatingSystem / preInstalledSw / capacitystatus`. Confirm snapshot entries use the same key set with the same casing.
2. **RDS Multi-AZ.** AWS Phase 3 sets `deploymentOption = "Single-AZ"` or `"Multi-AZ"` based on `spec.multiAZ`. Confirm snapshot has both variants for each `(engine, instanceClass, region)` triple covered.
3. **S3 usage.** S3 cost is usage-priced (per GB-Mo); the estimator multiplies by `Usage["storageGB"]` (default 100). Confirm `aws_s3_bucket` ends up with a sensible default total (~$2/month per 100 GB) and that user override via `spec.usage.storageGB` works.
4. **Snapshot freshness warning.** Set `fetchedAt` to 100 days ago; confirm CLI prints "snapshot is 100 days old". Set to today; confirm no warning.
5. **Missing pricing.** Add a new EC2 instance type to the AWS adapter without a snapshot row; confirm the estimate succeeds for all other primitives, emits a warning for the unpriced one, and doesn't fail the plan.
6. **Aggregation correctness.** A 2-component, 2-target project; confirm `Estimate.Total == sum(Estimate.Targets[*].Subtotal)` and each `TargetEstimate.Subtotal == sum(PrimitiveEstimate.Subtotal)`.
7. **Engine wiring.** `Engine.EstimateCost(plan)` returns a populated estimate; engine package never imports estimator-internal types.
8. **CLI determinism.** Two plan + cost runs on the same fixture produce the same dollar figures.

---

## Future hooks (not Phase 1)

- **Live AWS Price List API.** `Cache.Refresh` becomes real; CLI gets `--refresh-prices` flag; pricing cache writes refreshed entries to disk under `~/.cache/nimbusfab/pricing/aws.json` for cross-process reuse.
- **Reserved instance pricing.** New snapshot fields `pricingModel: ondemand | ri-1yr | ri-3yr`; estimator gains a `--pricing-model` flag.
- **Savings plans.** Apply an org-wide multiplier per cloud.
- **Data transfer.** Add `aws_egress` synthetic pricing keys; estimator multiplies by user-declared `spec.usage.egressGB`.
- **Multi-currency.** Snapshot can declare any currency; estimator surfaces FX conversion notes.
- **Recommendation engine.** "If you switched orders-db from db.t3.medium to db.t3.small you'd save $X/mo" — requires understanding the workload, deferred to v2.

---

## Relationship to other subsystems

- **Provisioner Plan** drives the inputs: it provides primitives + adapter references. Engine wraps Plan and estimator together.
- **Parity Engine** is orthogonal: parity compares profiles across clouds; cost estimates totals per cloud. Both consume the same Plan output independently. (When the cost dashboard lands, it joins them: "GCP costs 12% more for 18% better parity".)
- **Inventory** writes the estimate to `cost_estimates` table — Phase 2 wires this. Phase 1 estimates exist only in memory + CLI output.
- **Cost Collector** (separate spec, future phase) writes `cost_actuals` from billing APIs. The dashboard then shows estimated vs. actual.
- **Web app** consumes `Engine.EstimateCost` and renders the breakdown UI.
