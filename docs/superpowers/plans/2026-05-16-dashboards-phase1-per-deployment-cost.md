# Dashboards Phase 1 (Per-Deployment Cost View) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** After Phase 1, a user planning a deployment sees the per-primitive cost estimate persisted in inventory and surfaced as a "Cost estimate" section on `/ui/deployments/{id}` plus a JSON endpoint at `GET /api/v1/deployments/{id}/costs`. Cross-deployment aggregation (org-wide cost dashboard) becomes Dashboards Phase 2.

**Architecture:**
- `CostEstimateRepo` gains a `ListByDeployment(ctx, orgID, deploymentID)` method (JOINs through runs → deployment_targets to filter). Both backends.
- `engine.persistPlan` retains the plan-run IDs it creates per target; after writing run rows, it estimates costs and BulkInserts one row per (run, primitive). Skipped silently in no-inventory mode (CLI) — keeps the dev path unchanged.
- New API handler `GET /api/v1/deployments/{id}/costs` returns `{data: {currency, total, targets: [{deploymentTargetId, cloud, region, total, primitives: [...]}]}}` by aggregating raw rows server-side.
- Deployment detail HTML template gains a "Cost estimate" section with a per-target subtotals table + total. Read-only; no JS.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-dashboards-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- One commit per task.

**Out of scope:**
- Org-wide aggregate dashboard (Dashboards Phase 2).
- Parity overview page (separate phase).
- Drift dashboard (Drift Phase 1).
- Cost actuals (Cost Collector phase).
- Cost-over-time charts (Dashboards Phase 2 or Polish).

---

## Task 1: CostEstimateRepo.ListByDeployment

**Files:**
- Edit: `pkg/inventory/repo.go` (interface addition)
- Edit: `internal/inventory/sqlite/cost_estimates.go`
- Edit: `internal/inventory/postgres/cost_estimates.go`
- Edit: `internal/inventory/sqlite/crud_test.go`
- Edit: `internal/inventory/postgres/postgres_test.go`

- [ ] **Step 1: Interface**

```go
type CostEstimateRepo interface {
    BulkInsert(ctx context.Context, items []CostEstimate) error
    ListByRun(ctx context.Context, orgID, runID string) ([]CostEstimate, error)
    ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]CostEstimate, error)
}
```

- [ ] **Step 2: SQLite impl**

```go
func (r *costEstimateRepo) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]inventory.CostEstimate, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT ce.run_id, ce.org_id, ce.primitive_id, ce.currency,
               ce.unit_price, ce.units, ce.unit_of_measure, ce.subtotal,
               COALESCE(ce.pricing_key_json, '')
        FROM cost_estimates ce
        JOIN runs r ON r.id = ce.run_id
        JOIN deployment_targets dt ON dt.id = r.deployment_target_id
        WHERE ce.org_id = ? AND dt.deployment_id = ?
        ORDER BY ce.id
    `, orgID, deploymentID)
    // scan into []inventory.CostEstimate
}
```

- [ ] **Step 3: Postgres impl** — same SQL with $N placeholders + `::text` cast on `pricing_key_json` for the COALESCE.

- [ ] **Step 4: Tests** — extend both backend tests to seed a deployment with two targets, two runs, four cost estimates; assert `ListByDeployment` returns all four.

- [ ] **Step 5: Build + test + commit** `inventory: CostEstimateRepo.ListByDeployment (JOINs runs→targets)`

---

## Task 2: engine.persistPlan persists cost estimates

**Files:**
- Edit: `pkg/engine/inventory.go`
- Edit: `pkg/engine/plan_test.go` (assert cost rows after Plan)

- [ ] **Step 1: Retain run IDs + persist**

Modify persistPlan to collect each plan-run's ID, then call the existing estimator with the plan + map run IDs back to primitives.

```go
// Existing per-target loop also collects (runID, RawPrimitives).
type planRunInfo struct {
    RunID         string
    Primitives    []ir.ResourcePrimitive
    DeploymentTgt provisioner.TargetPlan
}
var planRuns []planRunInfo

for i := range plan.Targets {
    tp := &plan.Targets[i]
    targetID := "tgt-" + uuid.NewString()
    tp.DeploymentTargetID = targetID
    // ... existing DeploymentTargets.Create
    runID := "run-" + uuid.NewString()
    // ... existing Runs.Create with runID
    planRuns = append(planRuns, planRunInfo{RunID: runID, Primitives: tp.RawPrimitives, DeploymentTgt: *tp})
}

// After all targets persisted, estimate + BulkInsert.
if err := e.persistCostEstimates(ctx, orgID, planRuns); err != nil {
    // Log and continue; cost persistence is non-critical to plan success.
}
```

- [ ] **Step 2: persistCostEstimates helper**

```go
func (e *runtimeEngine) persistCostEstimates(ctx context.Context, orgID string, planRuns []planRunInfo) error {
    cache := pricing.NewCache()
    est := estimator.New(pricing.AsPricingProvider(cache))

    // Build estimator input per target so we get per-target subtotals;
    // BulkInsert one row per primitive, with the right run ID.
    var items []inventory.CostEstimate
    for _, pr := range planRuns {
        adapter, ok := e.cfg.CloudAdapters.Get(pr.DeploymentTgt.Cloud)
        if !ok { continue }
        in := estimator.EstimateInput{Targets: []estimator.TargetInput{{
            DeploymentTargetID: pr.DeploymentTgt.DeploymentTargetID,
            Cloud: pr.DeploymentTgt.Cloud, Region: pr.DeploymentTgt.Region,
            Adapter: adapter, Primitives: pr.Primitives,
        }}}
        out, err := est.Estimate(ctx, in)
        if err != nil { continue }
        for _, t := range out.Targets {
            for _, p := range t.Primitives {
                keyJSON, _ := json.Marshal(p.PricingKey)
                items = append(items, inventory.CostEstimate{
                    RunID: pr.RunID, OrgID: orgID,
                    PrimitiveID: p.PrimitiveID, Currency: out.Currency,
                    UnitPrice: p.UnitPrice, Units: p.Units,
                    UnitOfMeasure: p.UnitOfMeasure, Subtotal: p.Subtotal,
                    PricingKeyJSON: keyJSON,
                })
            }
        }
    }
    return e.cfg.InventoryRepo.CostEstimates().BulkInsert(ctx, items)
}
```

- [ ] **Step 3: Test**

In `plan_test.go`, after `Plan()` returns: open the same sqlite DB and call `ListByDeployment(orgID, deploymentID)` — assert non-zero rows for a project whose components have priced primitives.

- [ ] **Step 4: Build + test + commit** `engine: persistPlan also persists cost estimates`

---

## Task 3: API handler — GET /api/v1/deployments/{id}/costs

**Files:**
- Create: `internal/webapi/api/costs.go`
- Create: `internal/webapi/api/costs_test.go`

- [ ] **Step 1: Handler**

```go
// GetDeploymentCosts → GET /api/v1/deployments/{id}/costs
func (h *Handlers) GetDeploymentCosts(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    ctx := r.Context()
    // Verify the deployment exists + scoped.
    if d, _ := h.Repo.Deployments().Get(ctx, h.OrgID, id); d == nil {
        writeError(w, http.StatusNotFound, "ErrNotFound", "deployment not found: "+id)
        return
    }
    rows, err := h.Repo.CostEstimates().ListByDeployment(ctx, h.OrgID, id)
    if err != nil { writeError(w, 500, "ErrInventory", err.Error()); return }
    targets, _ := h.Repo.DeploymentTargets().ListByDeployment(ctx, h.OrgID, id)
    runs := map[string]string{} // run_id → target_id
    for _, t := range targets {
        for _, run := range h.Repo.Runs().ListByDeploymentTarget(...) {
            runs[run.ID] = t.ID
        }
    }
    writeData(w, aggregateCosts(rows, targets, runs))
}
```

Aggregate shape:
```json
{
  "data": {
    "deploymentId": "...",
    "currency": "USD",
    "total": 57.83,
    "targets": [
      {
        "deploymentTargetId": "...",
        "cloud": "aws",
        "region": "us-east-1",
        "componentName": "web-app",
        "total": 30.37,
        "primitives": [{primitiveId, unitPrice, units, unitOfMeasure, subtotal}]
      }
    ]
  }
}
```

- [ ] **Step 2: Tests** — seed a deployment with cost estimates via the sqlite repo, hit the handler, assert JSON shape.

- [ ] **Step 3: Build + test + commit** `webapi: GET /api/v1/deployments/{id}/costs handler`

---

## Task 4: UI — Cost estimate section on deployment detail

**Files:**
- Edit: `internal/webapi/ui/pages.go` (DeploymentDetail loads cost rows)
- Edit: `internal/webapi/ui/templates/deployment_detail.html` (new section)
- Edit: `internal/webapi/router.go` (mount new API route)
- Edit: `internal/webapi/router_test.go`

- [ ] **Step 1: DeploymentDetail handler enrichment**

Pull cost rows + aggregate into a `[]TargetCostSummary` and pass into the template alongside existing data.

- [ ] **Step 2: Template section**

```html
<h2>Cost estimate</h2>
{{if .CostSummary.HasData}}
<table>
  <thead><tr><th>Component</th><th>Cloud</th><th>Region</th><th>Subtotal</th></tr></thead>
  <tbody>
    {{range .CostSummary.Targets}}
    <tr>
      <td>{{.ComponentName}}</td>
      <td><span class="badge">{{.Cloud}}</span></td>
      <td><code>{{.Region}}</code></td>
      <td>${{printf "%.2f" .Subtotal}} {{.Currency}}/month</td>
    </tr>
    {{end}}
  </tbody>
  <tfoot>
    <tr><th colspan="3">Total</th><th>${{printf "%.2f" .CostSummary.Total}} {{.CostSummary.Currency}}/month</th></tr>
  </tfoot>
</table>
{{else}}
<p class="muted">No cost estimate recorded yet.</p>
{{end}}
```

- [ ] **Step 3: Router mount**

```go
mux.Handle("GET /api/v1/deployments/{id}/costs", apiAuth(http.HandlerFunc(apiHandlers.GetDeploymentCosts)))
```

- [ ] **Step 4: Tests** — UI test confirms the new section renders; router test confirms /api/v1/.../costs serves JSON.

- [ ] **Step 5: Build + test + commit** `ui+webapi: deployment cost estimate section + /api/v1/deployments/{id}/costs`

---

## Task 5: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: CHANGELOG** entry under "Unreleased — Dashboards Phase 1 (Per-Deployment Cost View)":
  - engine.Plan now persists cost estimates
  - ListByDeployment repo method
  - API + UI section
  - Out of scope: org-wide aggregate, parity overview, drift dashboard

- [ ] **Step 2: Final test + gofmt + commit**

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/dashboards-phase1 -m "Merge feat/dashboards-phase1: per-deployment cost view"
git push origin main
git push origin feat/dashboards-phase1
```
