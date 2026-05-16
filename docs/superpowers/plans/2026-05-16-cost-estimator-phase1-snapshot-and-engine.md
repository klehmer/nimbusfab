# Cost Estimator Phase 1 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working `nimbusfab cost estimate --stack <stack>` that produces a per-target / per-primitive cost estimate from a bundled AWS price snapshot. `nimbusfab plan` adds a cost summary alongside the existing parity summary. Engine's `EstimateCost(plan)` returns a populated `CostEstimate`.

**Architecture:** `pkg/cost/pricing.NewCache()` loads embedded JSON snapshots (one per cloud) at startup; canonicalizes both `PricingKey` queries and snapshot entries to flat strings; lookup is O(1) in a `map[string]Entry`. `pkg/cost/estimator.New(provider)` iterates `EstimateInput.Targets` × primitives, calls `adapter.PricingKey`, queries the provider, multiplies by usage assumptions (730 hrs/mo for compute/db; 100 GB-Mo default for storage). Engine wraps it via `EstimateCost`. CLI adds a `cost` subcommand.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-cost-estimator-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for all go commands.
- One commit per task.

**Out of scope:**
- Live AWS Pricing API. `Cache.Refresh` returns `ErrNotImplementedYet`.
- Azure / GCP snapshots. Schema supports them; data ships with those adapters.
- Cost actuals collection.
- Inventory writes of estimates.
- RI / savings plans / spot pricing.
- Multi-currency.

---

## Task 1: Bundled AWS snapshot + schema

**Files:**
- Create: `pkg/cost/pricing/snapshot/aws.json`
- Create: `pkg/cost/pricing/snapshot/README.md`

- [ ] **Step 1: Curate AWS prices**

Hand-write `pkg/cost/pricing/snapshot/aws.json` with on-demand prices for every Phase 3-emittable AWS SKU × supported region. Use AWS public pricing pages as the source. Prices in USD/Hr (compute/db) or USD/GB-Mo (storage).

EC2 (Linux on-demand, us-east-1 reference):
- t3.small: $0.0208/hr
- t3.medium: $0.0416/hr
- t3.large: $0.0832/hr
- t3.xlarge: $0.1664/hr
- m6i.large: $0.0960/hr
- m6i.xlarge: $0.1920/hr

RDS (postgres Single-AZ, us-east-1 reference; Multi-AZ ~2x):
- db.t3.small postgres Single-AZ: $0.034/hr
- db.t3.medium postgres Single-AZ: $0.068/hr
- db.m6i.large postgres Single-AZ: $0.197/hr
- db.m6i.xlarge postgres Single-AZ: $0.395/hr

S3 Standard us-east-1: $0.023/GB-Mo for first 50 TB.

Cover 8 regions for completeness: us-east-1, us-east-2, us-west-1, us-west-2, eu-west-1, eu-central-1, ap-northeast-1, ap-southeast-2. Apply rough per-region multipliers (eu/ap regions typically +5-15% over us-east-1; use AWS pricing page for exact numbers).

For Phase 1, prioritize correctness for us-east-1 (the default region in our fixtures). Other regions can use the same prices as us-east-1 — the schema supports per-region data, and live-fetch in Phase 2 will fill in true per-region pricing.

- [ ] **Step 2: Write README**

```markdown
# AWS Pricing Snapshot

Manually curated on-demand prices for the AWS resource types nimbusfab's AWS
adapter (`internal/cloud/aws`) emits in Phase 3. All prices in USD.

## Format

See `aws.json`. Top-level shape:

```json
{
  "$schema": "https://nimbusfab.dev/schema/pricing/snapshot/v1alpha1.json",
  "cloud": "aws",
  "currency": "USD",
  "fetchedAt": "ISO-8601 timestamp",
  "source": "manual-curation | live-fetch | ...",
  "entries": [ ... ]
}
```

Each entry:

```json
{
  "key": { /* same shape as Adapter.PricingKey() */ },
  "unitPrice": 0.0208,
  "unitOfMeasure": "Hrs | GB-Mo | ..."
}
```

## Refreshing

Phase 1 is manual. To refresh:

1. Pull current AWS On-Demand prices per region from
   https://aws.amazon.com/ec2/pricing/on-demand/ (and equivalent for RDS, S3).
2. Update `unitPrice` for each entry.
3. Update top-level `fetchedAt` to today.
4. Optionally update `source` to describe the refresh method.
5. Run `go test ./pkg/cost/...` and confirm the bundled-snapshot tests still pass.

Phase 2 of cost will add live AWS Pricing API integration plus a
`tools/pricing/verify/` program to diff snapshot vs. live and flag stale rows.
```

- [ ] **Step 3: Commit**

```bash
mkdir -p pkg/cost/pricing/snapshot
# write the two files
git add pkg/cost/pricing/snapshot/
git commit -m "pricing: bundled AWS snapshot covering Phase-3 EC2 / RDS / S3 SKUs"
```

---

## Task 2: Snapshot loader + canonical key

**Files:**
- Create: `pkg/cost/pricing/snapshot.go`
- Create: `pkg/cost/pricing/snapshot_test.go`

- [ ] **Step 1: Implement**

```go
// pkg/cost/pricing/snapshot.go
package pricing

import (
    "embed"
    "encoding/json"
    "fmt"
    "sort"
    "time"
)

//go:embed snapshot/*.json
var snapshotFS embed.FS

// SnapshotEntry is one curated price-list row.
type SnapshotEntry struct {
    Key           map[string]any `json:"key"`
    UnitPrice     float64        `json:"unitPrice"`
    UnitOfMeasure string         `json:"unitOfMeasure"`
}

// Snapshot is one cloud's full snapshot file.
type Snapshot struct {
    Cloud     string          `json:"cloud"`
    Currency  string          `json:"currency"`
    FetchedAt time.Time       `json:"fetchedAt"`
    Source    string          `json:"source"`
    Entries   []SnapshotEntry `json:"entries"`
}

// LoadSnapshots reads every embedded snapshot file and returns them keyed by cloud.
func LoadSnapshots() (map[string]*Snapshot, error) {
    out := map[string]*Snapshot{}
    entries, err := snapshotFS.ReadDir("snapshot")
    if err != nil {
        return nil, fmt.Errorf("LoadSnapshots: %w", err)
    }
    for _, e := range entries {
        if e.IsDir() || filepath_ext(e.Name()) != ".json" {
            continue
        }
        body, err := snapshotFS.ReadFile("snapshot/" + e.Name())
        if err != nil {
            return nil, fmt.Errorf("LoadSnapshots: read %s: %w", e.Name(), err)
        }
        var s Snapshot
        if err := json.Unmarshal(body, &s); err != nil {
            return nil, fmt.Errorf("LoadSnapshots: parse %s: %w", e.Name(), err)
        }
        if s.Cloud == "" {
            return nil, fmt.Errorf("LoadSnapshots: %s missing 'cloud'", e.Name())
        }
        out[s.Cloud] = &s
    }
    return out, nil
}

// CanonicalKey flattens a PricingKey-shaped map to a deterministic string.
// Drops empty values, sorts keys, joins as k1=v1;k2=v2;...
func CanonicalKey(key map[string]any) string {
    keys := make([]string, 0, len(key))
    for k, v := range key {
        if v == nil {
            continue
        }
        s := fmt.Sprintf("%v", v)
        if s == "" {
            continue
        }
        keys = append(keys, k)
    }
    sort.Strings(keys)
    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        parts = append(parts, fmt.Sprintf("%s=%v", k, key[k]))
    }
    return joinSemi(parts)
}

func joinSemi(parts []string) string {
    out := ""
    for i, p := range parts {
        if i > 0 {
            out += ";"
        }
        out += p
    }
    return out
}

func filepath_ext(name string) string {
    for i := len(name) - 1; i >= 0 && name[i] != '/'; i-- {
        if name[i] == '.' {
            return name[i:]
        }
    }
    return ""
}
```

- [ ] **Step 2: Test**

Cover: load all snapshots; canonical key determinism; canonical key drops empty values; canonical key sorts keys.

- [ ] **Step 3: Commit**

---

## Task 3: Cache implementation + PricingProvider adapter

**Files:**
- Create: `pkg/cost/pricing/cache.go`
- Create: `pkg/cost/pricing/cache_test.go`

- [ ] **Step 1: Implement**

```go
// pkg/cost/pricing/cache.go
package pricing

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/cost/estimator"
)

// ErrPricingMissing is returned by Lookup when no snapshot entry matches.
var ErrPricingMissing = errors.New("pricing: no snapshot entry for key")

// ErrNotImplementedYet is returned by Refresh in Phase 1.
var ErrNotImplementedYet = errors.New("pricing: not implemented yet (live fetch)")

// NewCache returns a Cache backed by the embedded snapshots. Panics on
// malformed snapshot data (build-time issue).
func NewCache() Cache {
    snaps, err := LoadSnapshots()
    if err != nil {
        panic(fmt.Sprintf("pricing.NewCache: %v", err))
    }
    c := &snapshotCache{byCloud: map[string]*snapshotIndex{}}
    for cloudName, s := range snaps {
        idx := &snapshotIndex{
            Cloud:     cloudName,
            Currency:  s.Currency,
            FetchedAt: s.FetchedAt,
            Entries:   map[string]Entry{},
        }
        for _, e := range s.Entries {
            ck := CanonicalKey(e.Key)
            idx.Entries[ck] = Entry{
                UnitPrice:     e.UnitPrice,
                UnitOfMeasure: e.UnitOfMeasure,
                Currency:      s.Currency,
                Source:        "snapshot",
                FetchedAt:     s.FetchedAt,
            }
        }
        c.byCloud[cloudName] = idx
    }
    return c
}

type snapshotCache struct {
    byCloud map[string]*snapshotIndex
}

type snapshotIndex struct {
    Cloud     string
    Currency  string
    FetchedAt time.Time
    Entries   map[string]Entry
}

func (c *snapshotCache) Lookup(ctx context.Context, cloudName string, key map[string]any) (Entry, error) {
    idx, ok := c.byCloud[cloudName]
    if !ok {
        return Entry{}, fmt.Errorf("%w: no snapshot for cloud %q", ErrPricingMissing, cloudName)
    }
    ck := CanonicalKey(key)
    entry, ok := idx.Entries[ck]
    if !ok {
        return Entry{}, fmt.Errorf("%w: %s/%s", ErrPricingMissing, cloudName, ck)
    }
    return entry, nil
}

func (c *snapshotCache) Refresh(ctx context.Context, cloudName string, keys []map[string]any) error {
    return ErrNotImplementedYet
}

// AsPricingProvider adapts a Cache to the estimator's PricingProvider interface.
func AsPricingProvider(c Cache) estimator.PricingProvider {
    return &pricingProviderAdapter{cache: c}
}

type pricingProviderAdapter struct {
    cache Cache
}

func (p *pricingProviderAdapter) Price(ctx context.Context, cloudName string, key map[string]any) (estimator.UnitPrice, error) {
    entry, err := p.cache.Lookup(ctx, cloudName, key)
    if err != nil {
        return estimator.UnitPrice{}, err
    }
    return estimator.UnitPrice{
        UnitPrice:     entry.UnitPrice,
        UnitOfMeasure: entry.UnitOfMeasure,
        Currency:      entry.Currency,
        Source:        entry.Source,
    }, nil
}

// SnapshotAge returns the staleness of the bundled snapshot for cloud.
// Returns 0 if no snapshot exists.
func SnapshotAge(c Cache, cloudName string) time.Duration {
    sc, ok := c.(*snapshotCache)
    if !ok {
        return 0
    }
    idx, ok := sc.byCloud[cloudName]
    if !ok {
        return 0
    }
    return time.Since(idx.FetchedAt)
}
```

- [ ] **Step 2: Test**

Cover: Lookup hits known key (AWS EC2 t3.small us-east-1); Lookup misses unknown cloud; Lookup misses unknown key; Refresh returns ErrNotImplementedYet; SnapshotAge returns reasonable duration; AsPricingProvider returns UnitPrice for known.

- [ ] **Step 3: Commit**

---

## Task 4: Usage assumptions

**Files:**
- Create: `pkg/cost/estimator/usage.go`
- Create: `pkg/cost/estimator/usage_test.go`

- [ ] **Step 1: Implement**

```go
// pkg/cost/estimator/usage.go
package estimator

// HoursPerMonth is the assumed billing hours for monthly compute/db estimates.
// 24 hours × 30.4375 days = 730.5 (rounded to 730).
const HoursPerMonth = 730.0

// DefaultStorageGB is the assumed storage usage for S3 buckets when the user
// hasn't provided spec.usage.storageGB.
const DefaultStorageGB = 100.0

// UnitsFor returns the multiplier to apply to a primitive's unit price for one
// month's estimated cost, given any user-provided usage overrides.
//
// Examples:
//   - aws_instance × 730 hours = monthly EC2 cost
//   - aws_db_instance × 730 hours = monthly RDS cost
//   - aws_s3_bucket × storage GB = monthly S3 cost
func UnitsFor(tofuType string, usage map[string]any) float64 {
    switch tofuType {
    case "aws_instance", "aws_db_instance":
        if hr, ok := numberFrom(usage["hoursPerMonth"]); ok {
            return hr
        }
        return HoursPerMonth
    case "aws_s3_bucket":
        if gb, ok := numberFrom(usage["storageGB"]); ok {
            return gb
        }
        return DefaultStorageGB
    default:
        return 0
    }
}

func numberFrom(v any) (float64, bool) {
    switch t := v.(type) {
    case int:
        return float64(t), true
    case int64:
        return float64(t), true
    case float64:
        return t, true
    case float32:
        return float64(t), true
    }
    return 0, false
}
```

- [ ] **Step 2: Test**

Cover: defaults for each priced type; user override; unpriced type returns 0.

- [ ] **Step 3: Commit**

---

## Task 5: Runtime estimator

**Files:**
- Create: `pkg/cost/estimator/runtime.go`
- Create: `pkg/cost/estimator/runtime_test.go`

- [ ] **Step 1: Implement**

```go
// pkg/cost/estimator/runtime.go
package estimator

import (
    "context"
    "errors"
    "fmt"
)

// New returns a runtime Estimator wired against the provider.
func New(provider PricingProvider) Estimator {
    return &runtime{provider: provider}
}

type runtime struct {
    provider PricingProvider
}

func (r *runtime) Estimate(ctx context.Context, in EstimateInput) (Estimate, error) {
    est := Estimate{
        Currency: "USD", // v1 single-currency
        Period:   "month",
    }
    for _, target := range in.Targets {
        tEst := TargetEstimate{
            DeploymentTargetID: target.DeploymentTargetID,
            Cloud:              target.Cloud,
            Region:             target.Region,
        }
        for _, prim := range target.Primitives {
            key, err := target.Adapter.PricingKey(ctx, prim)
            if err != nil || key == nil {
                // Free primitive; no estimate contribution.
                continue
            }
            unit, err := r.provider.Price(ctx, target.Cloud, key)
            if err != nil {
                est.Warnings = append(est.Warnings, fmt.Sprintf(
                    "missing pricing for %s (%s): %v", prim.ID, prim.TofuType, err))
                continue
            }
            units := UnitsFor(prim.TofuType, target.Usage)
            if units == 0 {
                est.Warnings = append(est.Warnings, fmt.Sprintf(
                    "no usage assumption for %s (%s); skipping", prim.ID, prim.TofuType))
                continue
            }
            subtotal := unit.UnitPrice * units
            tEst.Primitives = append(tEst.Primitives, PrimitiveEstimate{
                PrimitiveID:   prim.ID,
                PricingKey:    key,
                UnitPrice:     unit.UnitPrice,
                Units:         units,
                UnitOfMeasure: unit.UnitOfMeasure,
                Subtotal:      subtotal,
            })
            tEst.Subtotal += subtotal
        }
        est.Targets = append(est.Targets, tEst)
        est.Total += tEst.Subtotal
    }
    return est, nil
}

// ErrEstimatorEmpty is reserved; estimator never errors-out today.
var ErrEstimatorEmpty = errors.New("estimator: empty input")
```

- [ ] **Step 2: Test**

Cover: empty input (returns zero estimate); one target with one EC2 instance (subtotal = unit price × 730); aggregation correctness (total = sum of target subtotals); missing pricing recorded as warning, not error; free primitive (nil pricing key) skipped silently.

Use a fake PricingProvider for unit tests rather than the full cache; that keeps the estimator tests independent of snapshot curation.

- [ ] **Step 3: Commit**

---

## Task 6: Engine integration

**Files:**
- Create: `pkg/engine/cost.go`
- Create: `pkg/engine/cost_test.go`
- Modify: `pkg/engine/plan.go` (replace EstimateCost stub)

- [ ] **Step 1: Implement**

```go
// pkg/engine/cost.go
package engine

import (
    "context"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/cost/estimator"
    "github.com/klehmer/nimbusfab/pkg/cost/pricing"
)

// estimateCost is the inventory-agnostic core: takes a PlanResult, builds the
// EstimateInput from per-target primitives via the cloud registry, runs the
// estimator, wraps the result into the engine's CostEstimate.
func (e *runtimeEngine) estimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error) {
    // The estimator needs adapter references per cloud + primitives per target.
    // We re-derive primitives by calling Plan's saved tracking from
    // TargetPlan.PrimitiveProfiles (which carries the per-primitive profile
    // each adapter emitted). For pricing we need the raw primitives, so we
    // call Plan-time data only — Phase 1 limitation: we re-emit per target by
    // looking up the adapter and re-running emit. (Plan re-emission is cheap
    // and pure; not ideal for v2 but bounded.)
    if plan == nil {
        return nil, fmt.Errorf("engine.EstimateCost: nil plan")
    }
    cache := pricing.NewCache()
    est := estimator.New(pricing.AsPricingProvider(cache))

    in := estimator.EstimateInput{}
    for _, tp := range plan.Targets {
        adapter, ok := e.cfg.CloudAdapters.Get(tp.Cloud)
        if !ok {
            return nil, fmt.Errorf("engine.EstimateCost: no adapter for %q", tp.Cloud)
        }
        // Re-derive primitives by reading the workspace's main.tf.json.
        // Phase 1 simplification: we use the PrimitiveProfiles list to know
        // which primitives exist, but we need PricingKeys which come from
        // adapter.PricingKey(primitive). Each primitive's identity is preserved
        // in the IDs we recorded — but we don't currently keep the raw
        // primitives. So Phase 1 either:
        //   (a) re-emits the IR through the adapter to recover primitives, OR
        //   (b) extends TargetPlan to keep primitives.
        //
        // (a) requires the original IR (we'd need it from caller).
        // (b) is a one-field addition to TargetPlan.
        //
        // Phase 1 takes path (b) below.
        primitives := tp.RawPrimitives
        in.Targets = append(in.Targets, estimator.TargetInput{
            DeploymentTargetID: tp.DeploymentTargetID,
            Cloud:              tp.Cloud,
            Region:             tp.Region,
            Adapter:            adapter,
            Primitives:         primitives,
        })
    }
    out, err := est.Estimate(ctx, in)
    if err != nil {
        return nil, err
    }
    return convertEstimate(out), nil
}

func convertEstimate(e estimator.Estimate) *CostEstimate {
    out := &CostEstimate{
        Currency: e.Currency,
        Period:   e.Period,
        Total:    e.Total,
        Warnings: append([]string{}, e.Warnings...),
    }
    for _, t := range e.Targets {
        te := TargetCostEstimate{
            DeploymentTargetID: t.DeploymentTargetID,
            Cloud:              t.Cloud,
            Region:             t.Region,
            Subtotal:           t.Subtotal,
        }
        for _, p := range t.Primitives {
            te.Primitives = append(te.Primitives, PrimitiveCostEstimate{
                PrimitiveID:   p.PrimitiveID,
                PricingKey:    p.PricingKey,
                UnitPrice:     p.UnitPrice,
                Units:         p.Units,
                UnitOfMeasure: p.UnitOfMeasure,
                Subtotal:      p.Subtotal,
            })
        }
        out.Targets = append(out.Targets, te)
    }
    return out
}
```

Update `pkg/engine/plan.go`:

```go
func (e *runtimeEngine) EstimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error) {
    return e.estimateCost(ctx, plan)
}
```

Add `RawPrimitives []ir.ResourcePrimitive` field to `pkg/provisioner/types.go` TargetPlan; populate it in `pkg/provisioner/plan.go` `planOne` alongside `PrimitiveProfiles`.

- [ ] **Step 2: Test**

Cover: engine wires through; plan with full-stack fixture yields non-zero Total; warning surfaces in the engine output for missing pricing.

- [ ] **Step 3: Commit**

---

## Task 7: Plan + cost summary in CLI

**Files:**
- Modify: `cmd/cli/plan.go` (append cost summary after parity block)

- [ ] **Step 1: Implement**

After the parity block in `runPlan`, add:

```go
if est, err := eng.EstimateCost(ctx, result); err == nil && est != nil && est.Total > 0 {
    fmt.Fprintln(in.Stdout)
    fmt.Fprintln(in.Stdout, "Cost:")
    fmt.Fprintf(in.Stdout, "  Total estimated: $%.2f/%s\n", est.Total, est.Period)
    for _, t := range est.Targets {
        if t.Subtotal == 0 {
            continue
        }
        fmt.Fprintf(in.Stdout, "    %s/%s  $%.2f/%s\n", t.Cloud, t.Region, t.Subtotal, est.Period)
    }
    for _, w := range est.Warnings {
        fmt.Fprintf(in.Stdout, "  warning: %s\n", w)
    }
}
```

- [ ] **Step 2: Commit**

---

## Task 8: `nimbusfab cost estimate` command

**Files:**
- Create: `cmd/cli/cost.go`
- Create: `cmd/cli/cost_test.go`
- Modify: `cmd/cli/main.go` (register `cost` subcommand tree)

- [ ] **Step 1: Implement**

```go
// cmd/cli/cost.go
package main

import (
    "context"
    "fmt"
    "io"
    "os"

    "github.com/spf13/cobra"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/dsl/loader"
    "github.com/klehmer/nimbusfab/internal/dsl/validator"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/engine"
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func newCostCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "cost",
        Short: "Cost commands (estimate, actual — actual is a future phase)",
    }
    cmd.AddCommand(newCostEstimateCommand())
    return cmd
}

type costEstimateArgs struct {
    ProjectPath    string
    Stack          string
    Adapters       cloud.Registry
    Runner         tofu.Runner
    Inventory      inventory.Repo
    WorkRoot       string
    Stdout, Stderr io.Writer
}

func newCostEstimateCommand() *cobra.Command {
    var stack string
    cmd := &cobra.Command{
        Use:   "estimate [path]",
        Short: "Pre-deploy cost estimate from bundled price snapshot",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            projectPath := "."
            if len(args) == 1 {
                projectPath = args[0]
            }
            reg := cloud.NewRegistry()
            if err := reg.Register(aws.New()); err != nil {
                return err
            }
            repo, err := openInventory(cmd.Context(), flagInventoryDSN, flagNoInventory)
            if err != nil {
                fmt.Fprintf(cmd.ErrOrStderr(), "inventory: %v\n", err)
                os.Exit(1)
            }
            defer repo.Close()
            code := runCostEstimate(cmd.Context(), costEstimateArgs{
                ProjectPath: projectPath, Stack: stack,
                Adapters:  reg, Runner: tofu.NewExecRunner(), Inventory: repo,
                Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
            })
            if code != 0 {
                os.Exit(code)
            }
            return nil
        },
        SilenceUsage: true, SilenceErrors: true,
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

func runCostEstimate(ctx context.Context, in costEstimateArgs) int {
    if ctx == nil {
        ctx = context.Background()
    }
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack is required")
        return 2
    }
    project, err := loader.New().Load(ctx, in.ProjectPath)
    if err != nil {
        fmt.Fprintf(in.Stderr, "load: %v\n", err)
        return 1
    }
    if rep, vErr := validator.New().Validate(ctx, project); vErr != nil {
        fmt.Fprintf(in.Stderr, "validator: %v\n", vErr)
        return 2
    } else if rep != nil && !rep.OK() {
        for _, i := range rep.Issues {
            fmt.Fprintln(in.Stderr, i.String())
        }
        return 1
    }
    eng, err := engine.New(ctx, engine.Config{
        CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot, InventoryRepo: in.Inventory,
    })
    if err != nil {
        fmt.Fprintf(in.Stderr, "engine: %v\n", err)
        return 1
    }
    plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
    if err != nil {
        fmt.Fprintf(in.Stderr, "plan: %v\n", err)
        return 1
    }
    est, err := eng.EstimateCost(ctx, plan)
    if err != nil {
        fmt.Fprintf(in.Stderr, "estimate: %v\n", err)
        return 1
    }
    fmt.Fprintf(in.Stdout, "Total: $%.2f/%s (%s)\n\n", est.Total, est.Period, est.Currency)
    for _, t := range est.Targets {
        if t.Subtotal == 0 && len(t.Primitives) == 0 {
            continue
        }
        fmt.Fprintf(in.Stdout, "%s/%s  $%.2f/%s\n", t.Cloud, t.Region, t.Subtotal, est.Period)
        for _, p := range t.Primitives {
            fmt.Fprintf(in.Stdout, "  - %s  $%.4f × %.0f %s = $%.2f\n",
                p.PrimitiveID, p.UnitPrice, p.Units, p.UnitOfMeasure, p.Subtotal)
        }
    }
    if len(est.Warnings) > 0 {
        fmt.Fprintln(in.Stdout, "\nWarnings:")
        for _, w := range est.Warnings {
            fmt.Fprintf(in.Stdout, "  - %s\n", w)
        }
    }
    return 0
}
```

In `cmd/cli/main.go`, register: `root.AddCommand(newCostCommand())`.

- [ ] **Step 2: Test**

Use the full-stack fixture; assert exit 0 + non-zero Total + at least one priced primitive line.

- [ ] **Step 3: Commit**

---

## Task 9: README + CHANGELOG

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

Update Status; add `nimbusfab cost estimate` section and a note about the bundled snapshot.

- [ ] **Step 1: Edit**
- [ ] **Step 2: Final verification**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH go vet ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l .
```

- [ ] **Step 3: Commit**

---

## Final state

`nimbusfab plan` shows a cost summary alongside the parity summary.
`nimbusfab cost estimate --stack <stack>` produces a detailed
per-primitive breakdown. All pricing is from the bundled AWS snapshot;
live-fetch and Azure / GCP snapshots arrive in later phases without
requiring engine or CLI changes.
