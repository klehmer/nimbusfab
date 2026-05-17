# Drift Phase 1 (Drift Overview) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Org-wide drift overview. After Phase 1, `/ui/drift` shows every deployment target's current drift status (clean / drifted / never-checked) with links back to the deployment for re-running drift detection. `GET /api/v1/drift` returns the same data as JSON. Background scheduler (cron-style auto-drift) is Drift Phase 2.

**Architecture:**
- `DriftStatusRepo` gains `ListByOrg(orgID)` — currently only `Get(orgID, dtID)` and `Upsert` exist.
- New API handler `GET /api/v1/drift` returns the records enriched with deployment_target metadata (component name, cloud, region, deployment ID) so the UI can render rows without a separate lookup per row.
- New UI page `/ui/drift` with a table; top-nav link added.

**Out of scope:**
- Background drift scheduler (Drift Phase 2).
- Per-target drift detail page beyond what UI Phase 1's deployment detail already shows.
- Email / Slack notifications on drift detection.
- Drift-history time series.

---

## Task 1: DriftStatusRepo.ListByOrg

**Files:** `pkg/inventory/repo.go`, `pkg/inventory/nullrepo.go`, `internal/inventory/sqlite/drift.go`, `internal/inventory/postgres/drift.go`, `internal/inventory/sqlite/crud_test.go`, `internal/inventory/postgres/postgres_test.go`

```go
type DriftStatusRepo interface {
    Get(ctx context.Context, orgID, dtID string) (*DriftRecord, error)
    Upsert(ctx context.Context, d DriftRecord) error
    ListByOrg(ctx context.Context, orgID string) ([]DriftRecord, error)
}
```

Both backend impls: `SELECT * FROM drift_status WHERE org_id = $1 ORDER BY detected_at DESC`. null repo stub returns ErrInventoryRequired.

Tests seed 3 drift records across 2 orgs; assert ListByOrg returns only the matching org's records, in DESC order.

## Task 2: GET /api/v1/drift handler

`internal/webapi/api/drift.go` + tests.

Shape:
```json
{"data": {"records": [{"deploymentTargetId", "deploymentId", "componentName", "cloud", "region", "hasDrift", "detectedAt"}], "summary": {"total": N, "drifted": M, "clean": N-M}}}
```

Handler walks records, looks up each target (one-shot lookup; N+1 is fine at v1 scale).

## Task 3: /ui/drift page + top-nav link

`drift.html` template + handler in `internal/webapi/ui/pages.go`. Top-nav in `layout.html` gains a "Drift" link.

Table: status badge (✅ clean / ⚠️ drifted / — never-checked), component, cloud/region, detected_at, link to deployment. Summary line at top.

## Task 4: Router mount + tests + docs

`internal/webapi/router.go` adds the two routes. Tests cover both endpoints. CHANGELOG entry.

## Merge

Standard pattern.
