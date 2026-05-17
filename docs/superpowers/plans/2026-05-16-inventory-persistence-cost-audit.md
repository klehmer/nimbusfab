# Inventory Persistence (Cost + Audit) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Implement the `CostEstimates` and `AuditLog` repos for both sqlite and postgres. Both backends currently return `ErrNotImplementedYet`. Brings SQLite migration to parity with the Postgres migration (which already has all tables). Unblocks Dashboards Phase 1's cost view and Auth Phase 1's audit-log writes; engine.Plan wiring stays deferred to those consumers.

**Architecture:**
- SQLite migration gains the missing tables (cost_estimates, cost_actuals, audit_log, run_logs, secrets_refs, api_tokens) so the schema matches Postgres's. Existing tables unchanged.
- New `internal/inventory/sqlite/cost_estimates.go` and `internal/inventory/sqlite/audit.go` implement the two repos.
- Mirror for postgres: new `internal/inventory/postgres/cost_estimates.go` and `internal/inventory/postgres/audit.go`.
- Stubs in each backend's `notwired.go` shrink correspondingly.
- Tests for both backends (postgres tests gated on `NIMBUSFAB_TEST_PG_DSN` as established).

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-inventory-persistence/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- `engine.Plan` calling `CostEstimates.BulkInsert` — lands with Dashboards Phase 1 (the consumer).
- Audit-log writes from the web app middleware — lands with Auth Phase 1.
- `RunLogs`, `CostActuals`, `SecretsRefs`, `ApiTokens` repos — those land with their owning consumers. This phase only adds the tables.

---

## Task 1: SQLite migration parity

**Files:**
- Edit: `pkg/inventory/migrations/0001_init.sqlite.sql`
- Edit: `pkg/inventory/migrations_test.go` (assert new tables exist)

- [ ] **Step 1: Add missing tables** to match Postgres's schema. SQLite type mapping: UUID → TEXT, JSONB → TEXT, NUMERIC(20,8) → REAL, BIGSERIAL → INTEGER PRIMARY KEY AUTOINCREMENT.

Tables to add (in dependency order):
- `api_tokens` (depends on users)
- `compositions` (depends on projects) — actually compositions is already in sqlite, double-check
- `run_logs` (depends on runs)
- `cost_estimates` (depends on runs)
- `cost_actuals` (depends on orgs)
- `secrets_refs` (depends on orgs)
- `audit_log` (depends on orgs)

Use TEXT for UUIDs (SQLite has no UUID type); JSON columns stay TEXT; timestamps stay TEXT with the existing ISO-8601 convention.

- [ ] **Step 2: Migration test** asserts each new table exists in a fresh-migrate DB. Use `sqlite_master` query.

- [ ] **Step 3: Build + test + commit** `inventory: SQLite migration adds parity tables (cost / audit / etc.)`

---

## Task 2: SQLite CostEstimates + AuditLog repos

**Files:**
- Create: `internal/inventory/sqlite/cost_estimates.go`
- Create: `internal/inventory/sqlite/audit.go`
- Edit: `internal/inventory/sqlite/sqlite.go` (wire accessors to real repos)
- Edit: `internal/inventory/sqlite/notwired.go` (remove errCostEst, errAudit)
- Edit: `internal/inventory/sqlite/crud_test.go` (add round-trips)

- [ ] **Step 1: Implement**

```go
// cost_estimates.go
type costEstimateRepo struct{ db *sql.DB }

func (r *costEstimateRepo) BulkInsert(ctx context.Context, items []inventory.CostEstimate) error {
    if len(items) == 0 { return nil }
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil { return fmt.Errorf("cost_estimates.BulkInsert begin: %w", err) }
    defer tx.Rollback()
    stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO cost_estimates (id, org_id, run_id, primitive_id, currency, unit_price, units, unit_of_measure, subtotal, pricing_key_json)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `)
    if err != nil { return fmt.Errorf("cost_estimates.BulkInsert prepare: %w", err) }
    defer stmt.Close()
    for _, it := range items {
        id := "cest-" + uuid.NewString()
        if _, err := stmt.ExecContext(ctx, id, it.OrgID, it.RunID, it.PrimitiveID,
            it.Currency, it.UnitPrice, it.Units, it.UnitOfMeasure, it.Subtotal, string(it.PricingKeyJSON)); err != nil {
            return fmt.Errorf("cost_estimates.BulkInsert exec: %w", err)
        }
    }
    return tx.Commit()
}

func (r *costEstimateRepo) ListByRun(ctx context.Context, orgID, runID string) ([]inventory.CostEstimate, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT run_id, org_id, primitive_id, currency, unit_price, units, unit_of_measure, subtotal, COALESCE(pricing_key_json, '')
        FROM cost_estimates WHERE org_id = ? AND run_id = ?
    `, orgID, runID)
    // ... scan into []inventory.CostEstimate
}
```

```go
// audit.go
type auditRepo struct{ db *sql.DB }

func (r *auditRepo) Append(ctx context.Context, e inventory.AuditEntry) error {
    ts := e.Timestamp
    if ts.IsZero() { ts = time.Now().UTC() }
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO audit_log (org_id, actor_user_id, verb, target, payload_json, timestamp)
        VALUES (?, ?, ?, ?, ?, ?)
    `, e.OrgID, nullableStr(e.ActorUserID), e.Verb, e.Target, string(e.PayloadJSON), formatTime(ts))
    if err != nil { return fmt.Errorf("audit_log.Append: %w", err) }
    return nil
}

func (r *auditRepo) Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]inventory.AuditEntry, error) {
    if limit <= 0 { limit = 100 }
    rows, err := r.db.QueryContext(ctx, `
        SELECT org_id, COALESCE(actor_user_id, ''), verb, COALESCE(target, ''), COALESCE(payload_json, ''), timestamp
        FROM audit_log WHERE org_id = ? AND timestamp >= ? AND timestamp <= ?
        ORDER BY timestamp DESC LIMIT ?
    `, orgID, formatTime(since), formatTime(until), limit)
    // ... scan
}
```

Add `nullableStr(s)` helper if not present (mirrors `nullableTime`).

- [ ] **Step 2: Wire accessors**

In `sqlite.go`:
```go
func (r *Repo) CostEstimates() inventory.CostEstimateRepo { return &costEstimateRepo{db: r.db} }
func (r *Repo) AuditLog() inventory.AuditLogRepo          { return &auditRepo{db: r.db} }
```

Remove `errCostEst` and `errAudit` from `notwired.go`.

- [ ] **Step 3: Tests** in `crud_test.go`:
- `TestSQLite_CostEstimates_RoundTrip`: BulkInsert 3 items, ListByRun returns 3, fields match.
- `TestSQLite_AuditLog_RoundTrip`: Append 5 entries with varying timestamps; Query with a since/until window returns the right subset in DESC order; limit caps results.

- [ ] **Step 4: Build + test + commit** `inventory/sqlite: CostEstimates + AuditLog repos`

---

## Task 3: Postgres CostEstimates + AuditLog repos

**Files:**
- Create: `internal/inventory/postgres/cost_estimates.go`
- Create: `internal/inventory/postgres/audit.go`
- Edit: `internal/inventory/postgres/postgres.go` (wire accessors)
- Edit: `internal/inventory/postgres/notwired.go` (remove errCostEst, errAudit)
- Edit: `internal/inventory/postgres/postgres_test.go` (extend round-trip)

- [ ] **Step 1: Implement**

Same shape as SQLite versions with $N placeholders + JSONB ::text/::jsonb casts + sql.NullString → NULLIF dance for the nullable actor_user_id (Postgres UUID column can't accept the empty string). The audit_log id is BIGSERIAL — let Postgres assign it, don't include in INSERT.

- [ ] **Step 2: Wire** — accessors + notwired removals.

- [ ] **Step 3: Tests** — extend the existing CRUDRoundTrip test (still gated on `NIMBUSFAB_TEST_PG_DSN`) to also exercise CostEstimates BulkInsert/ListByRun and AuditLog Append/Query.

- [ ] **Step 4: Build + test + commit** `inventory/postgres: CostEstimates + AuditLog repos`

---

## Task 4: Docs

**Files:**
- Edit: `README.md` (no surface change; skip or just note "audit + cost-estimate persistence now available for both backends")
- Edit: `CHANGELOG.md`

- [ ] **Step 1: CHANGELOG** entry under "Unreleased — Inventory Persistence (Cost + Audit)":
  - SQLite migration parity additions
  - Two repos × two backends now wired
  - Stubs (RunLogs, CostActuals, SecretsRefs) remain for now
  - Consumer wiring (engine.Plan, web-app audit) is the next phase's job

- [ ] **Step 2: Final test + gofmt**

- [ ] **Step 3: Commit** `docs: cost-estimate + audit-log persistence wired for both backends`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/inventory-persistence -m "Merge feat/inventory-persistence: CostEstimates + AuditLog"
git push origin main
git push origin feat/inventory-persistence
```
