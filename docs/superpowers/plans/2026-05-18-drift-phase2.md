# Drift Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Background drift detection in `nimbusfab-server`: scheduler with global default + per-stack override, three notification transports (webhook / Slack / email) firing on edge transitions, per-project drift UI.

**Architecture:** New `pkg/drift/scheduler` + `pkg/drift/notify` packages; one schema migration (`0004_drift_interval`); validator Phase 6 (drift); IR `Stack.Drift` field; webapi additions (new project-drift page + global-drift filter). Reuses existing `engine.DetectDrift` from Phase 1.

**Tech Stack:** Go 1.22; `net/smtp` for email; `net/http` for webhooks; `time.Ticker` for scheduling; embed.FS for email template; html/template + vanilla JS for UI.

**Working spec:** `docs/superpowers/specs/2026-05-18-drift-phase2-design.md`

---

## Pre-flight

```bash
export PATH=$HOME/.local/go/bin:$HOME/.local/bin:$PATH
go test ./...                                                 # unit
go test -tags=integration ./cmd/cli/...                       # integration if needed
go build ./...
```

Useful files:
- `pkg/inventory/migrations/0001_init.sqlite.sql:106` — current `drift_status` table.
- `pkg/inventory/repo.go` — `Deployment` struct + `DriftStatusRepo` interface.
- `pkg/engine/inventory.go:60-90` — `persistPlan` (deployments insert site).
- `pkg/engine/plan.go:133` — `DetectDrift` engine entry point.
- `pkg/ir/types.go` — `Stack` struct.
- `internal/dsl/validator/validator.go` — existing phase dispatch.
- `cmd/server/main.go` — server boot + Engine wiring.
- `internal/webapi/ui/pages.go` — existing `Drift` handler (the global one).
- `internal/webapi/ui/templates/drift.html` — global drift template.
- `internal/webapi/ui/templates/project_detail.html` — page-tabs nav.

---

### Task 1: Schema migration `0004_drift_interval.sql`

**Files:**
- Create: `pkg/inventory/migrations/0004_drift_interval.sql` (postgres)
- Create: `pkg/inventory/migrations/0004_drift_interval.sqlite.sql` (sqlite)
- Modify: `pkg/inventory/migrations.go` if it has an embed.FS slice that needs the new entry.
- Test: `internal/inventory/sqlite/migrations_test.go` or similar — add an assertion the column exists.

- [ ] **Step 1: Inspect current migration registration**

```
ls pkg/inventory/migrations/
grep -n "migration\|embed.FS" pkg/inventory/migrations.go
```

- [ ] **Step 2: Write the failing test**

In whichever test file already exercises migrations (likely `internal/inventory/sqlite/migrations_test.go` based on prior phases), append:

```go
func TestMigrations_DriftIntervalColumn(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	// Assert deployments.drift_interval_seconds exists post-migrate.
	rows, err := db.Query("SELECT drift_interval_seconds FROM deployments LIMIT 0")
	if err != nil {
		t.Fatalf("column missing: %v", err)
	}
	rows.Close()
}
```

Match the test file's existing helper names. If there's no equivalent test in postgres yet, add a parallel test gated on `NIMBUSFAB_TEST_PG_DSN`.

- [ ] **Step 3: Run — should FAIL**

```
go test ./internal/inventory/sqlite/ -run TestMigrations_DriftIntervalColumn
```

- [ ] **Step 4: Add the migration files**

Create `pkg/inventory/migrations/0004_drift_interval.sql` (postgres):

```sql
ALTER TABLE deployments ADD COLUMN drift_interval_seconds INTEGER NOT NULL DEFAULT 0;
```

Create `pkg/inventory/migrations/0004_drift_interval.sqlite.sql` (sqlite):

```sql
ALTER TABLE deployments ADD COLUMN drift_interval_seconds INTEGER NOT NULL DEFAULT 0;
```

If `pkg/inventory/migrations.go` has an explicit migration list / embed.FS pattern, add the new files. (Inspect the file first — prior phases used `//go:embed migrations/*.sql` directives.)

- [ ] **Step 5: Run tests + verify**

```
go test ./internal/inventory/...
```

Should pass. The new test confirms the column exists.

- [ ] **Step 6: Commit**

```bash
git add pkg/inventory/migrations/0004_drift_interval.sql pkg/inventory/migrations/0004_drift_interval.sqlite.sql internal/inventory/sqlite/migrations_test.go
git commit -m "inventory: migration 0004 adds deployments.drift_interval_seconds"
```

`git status -sb` after — clean.

---

### Task 2: `inventory.Deployment.DriftIntervalSeconds` + repo updates

**Files:**
- Modify: `pkg/inventory/repo.go` — `Deployment` struct + maybe `DeploymentRepo` interface (`ListAll` method)
- Modify: `internal/inventory/sqlite/deployments.go` + `internal/inventory/postgres/deployments.go`
- Modify: `pkg/inventory/repo_test.go` or equivalent

- [ ] **Step 1: Inspect**

```
grep -n "type Deployment\|DeploymentRepo\|ListByProject\|ListAll" pkg/inventory/repo.go
grep -n "INSERT INTO deployments\|SELECT.*deployments" internal/inventory/sqlite/deployments.go internal/inventory/postgres/deployments.go
```

- [ ] **Step 2: Write failing test**

Append to whatever test file exercises the Deployments repo:

```go
func TestDeployments_DriftIntervalSeconds_Roundtrip(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	d := inventory.Deployment{
		ID: "dep-test", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
		Status: "applied", StartedAt: time.Now().UTC(),
		DriftIntervalSeconds: 14400,
	}
	// Seed prerequisite project + stack rows via existing helpers.
	mustSeedProjectStack(t, repo, "p-1", "s-1")
	if err := repo.Deployments().Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Deployments().Get(ctx, "default", "dep-test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DriftIntervalSeconds != 14400 {
		t.Errorf("DriftIntervalSeconds=%d, want 14400", got.DriftIntervalSeconds)
	}
}

func TestDeployments_ListAll(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	mustSeedProjectStack(t, repo, "p-1", "s-1")
	_ = repo.Deployments().Create(ctx, inventory.Deployment{
		ID: "dep-a", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
		Status: "applied", StartedAt: time.Now().UTC(),
	})
	_ = repo.Deployments().Create(ctx, inventory.Deployment{
		ID: "dep-b", OrgID: "default", ProjectID: "p-1", StackID: "s-1",
		Status: "planned", StartedAt: time.Now().UTC(),
	})
	got, err := repo.Deployments().ListAll(ctx, "default")
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2; got %d", len(got))
	}
}
```

If the existing test helpers have different names (e.g., `setupRepo` instead of `newTestRepo`), match what's there.

- [ ] **Step 3: Run — expect FAIL**

```
go test ./pkg/inventory/... ./internal/inventory/sqlite/...
```

`DriftIntervalSeconds` field missing; `ListAll` method missing.

- [ ] **Step 4: Implement**

In `pkg/inventory/repo.go`:

```go
type Deployment struct {
	// ... existing fields ...
	DriftIntervalSeconds int
}

type DeploymentRepo interface {
	// ... existing methods ...
	// ListAll returns every deployment for the org regardless of project.
	// Used by the drift scheduler.
	ListAll(ctx context.Context, orgID string) ([]Deployment, error)
}
```

In `internal/inventory/sqlite/deployments.go`:
- Update the INSERT statement to include `drift_interval_seconds`.
- Update the SELECT statements (Get, ListByProject) to include the column in the column list AND in `Scan(...)`.
- Add `ListAll`:

```go
func (r *DeploymentRepo) ListAll(ctx context.Context, orgID string) ([]inventory.Deployment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, org_id, project_id, stack_id, status, partial_failure_policy,
		        started_at, finished_at, drift_interval_seconds
		   FROM deployments WHERE org_id = ? ORDER BY started_at DESC`, orgID)
	// ... scan into []Deployment ...
}
```

Same shape in `internal/inventory/postgres/deployments.go` with `$1`-style placeholders + `NULLIF`-style if needed.

- [ ] **Step 5: Tests pass**

```
go test ./pkg/inventory/... ./internal/inventory/...
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add pkg/inventory/repo.go internal/inventory/sqlite/deployments.go internal/inventory/postgres/deployments.go <test files>
git commit -m "inventory: Deployment.DriftIntervalSeconds + DeploymentRepo.ListAll"
```

---

### Task 3: `DriftStatusRepo` new methods

**Files:**
- Modify: `pkg/inventory/repo.go` — extend `DriftStatusRepo` interface
- Modify: `internal/inventory/sqlite/drift.go` + `internal/inventory/postgres/drift.go`
- Test: respective `_test.go` files

- [ ] **Step 1: Inspect**

```
grep -n "DriftStatusRepo\|drift_status" pkg/inventory/repo.go internal/inventory/sqlite/drift.go internal/inventory/postgres/drift.go
```

- [ ] **Step 2: Write failing tests**

Append to the drift repo test file:

```go
func TestDriftStatus_LatestByDeployment(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	// Seed prerequisites: project, stack, deployment, 2 targets.
	mustSeedDeploymentWithTargets(t, repo, "dep-x", "p-1", "s-1",
		[]string{"tgt-a", "tgt-b"})
	// Write two rows per target with different detected_at values.
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	t2 := time.Now().UTC().Add(-1 * time.Hour)
	// older row first
	_ = repo.DriftStatus().Upsert(ctx, inventory.DriftStatus{
		OrgID: "default", DeploymentTargetID: "tgt-a",
		HasDrift: false, DetectedAt: t1, Status: "clean",
	})
	_ = repo.DriftStatus().Upsert(ctx, inventory.DriftStatus{
		OrgID: "default", DeploymentTargetID: "tgt-a",
		HasDrift: true, DetectedAt: t2, Status: "drift_detected",
	})
	_ = repo.DriftStatus().Upsert(ctx, inventory.DriftStatus{
		OrgID: "default", DeploymentTargetID: "tgt-b",
		HasDrift: false, DetectedAt: t2, Status: "clean",
	})
	got, err := repo.DriftStatus().LatestByDeployment(ctx, "default", "dep-x")
	if err != nil {
		t.Fatalf("LatestByDeployment: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 latest rows (one per target); got %d", len(got))
	}
	// tgt-a's latest row should have HasDrift=true (the newer one).
	for _, row := range got {
		if row.DeploymentTargetID == "tgt-a" && !row.HasDrift {
			t.Error("tgt-a latest should be drifted")
		}
	}
}

func TestDriftStatus_ListByProject(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	mustSeedDeploymentWithTargets(t, repo, "dep-x", "p-1", "s-1", []string{"tgt-a"})
	mustSeedDeploymentWithTargets(t, repo, "dep-y", "p-2", "s-2", []string{"tgt-b"})
	_ = repo.DriftStatus().Upsert(ctx, inventory.DriftStatus{
		OrgID: "default", DeploymentTargetID: "tgt-a",
		HasDrift: true, DetectedAt: time.Now().UTC(), Status: "drift_detected",
	})
	_ = repo.DriftStatus().Upsert(ctx, inventory.DriftStatus{
		OrgID: "default", DeploymentTargetID: "tgt-b",
		HasDrift: false, DetectedAt: time.Now().UTC(), Status: "clean",
	})
	got, err := repo.DriftStatus().ListByProject(ctx, "default", "p-1")
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(got) != 1 || got[0].DeploymentTargetID != "tgt-a" {
		t.Errorf("expected only tgt-a row; got %+v", got)
	}
}
```

`mustSeedDeploymentWithTargets` is a helper you may need to write — write it next to existing test helpers in the file, or extend an existing helper.

- [ ] **Step 3: Run — expect FAIL**

```
go test ./internal/inventory/sqlite/ -run TestDriftStatus_LatestByDeployment
```

- [ ] **Step 4: Implement**

In `pkg/inventory/repo.go`:

```go
type DriftStatusRepo interface {
	// ... existing methods ...
	// LatestByDeployment returns the most-recent drift_status row per
	// deployment_target_id for the deployment. Used by the scheduler to
	// determine "is this deployment due for re-check?" and edge detection.
	LatestByDeployment(ctx context.Context, orgID, deploymentID string) ([]DriftStatus, error)
	// ListByProject returns the latest drift_status row per deployment_target_id
	// for every deployment in the project. Used by the per-project drift UI.
	ListByProject(ctx context.Context, orgID, projectID string) ([]DriftStatus, error)
}
```

In sqlite + postgres:

```sql
-- LatestByDeployment
SELECT ds.* FROM drift_status ds
JOIN deployment_targets dt ON dt.id = ds.deployment_target_id
WHERE ds.org_id = ? AND dt.deployment_id = ?
AND ds.id IN (
  SELECT id FROM drift_status ds2 WHERE ds2.deployment_target_id = ds.deployment_target_id
  ORDER BY detected_at DESC LIMIT 1
)
```

Actually that's not quite right (the inner LIMIT 1 doesn't scope per group). Use a window function or a per-group MAX:

```sql
SELECT ds.* FROM drift_status ds
JOIN deployment_targets dt ON dt.id = ds.deployment_target_id
WHERE ds.org_id = ? AND dt.deployment_id = ?
AND (ds.deployment_target_id, ds.detected_at) IN (
  SELECT deployment_target_id, MAX(detected_at) FROM drift_status
  WHERE org_id = ?
  GROUP BY deployment_target_id
)
```

(Sqlite supports the tuple-IN syntax; postgres does too.) `ListByProject` is similar but joins through `deployments` to filter by `project_id`.

- [ ] **Step 5: Tests pass**

```
go test ./internal/inventory/...
```

- [ ] **Step 6: Commit**

```bash
git add pkg/inventory/repo.go internal/inventory/sqlite/drift.go internal/inventory/postgres/drift.go <test files>
git commit -m "inventory: DriftStatusRepo.LatestByDeployment + ListByProject"
```

---

### Task 4: `ir.Stack.Drift` + `ir.DriftConfig`

**Files:**
- Modify: `pkg/ir/types.go`
- Modify: `pkg/ir/types_test.go`

- [ ] **Step 1: Write failing test**

Append to `pkg/ir/types_test.go`:

```go
func TestStackDriftRoundtrip(t *testing.T) {
	in := Stack{Name: "dev",
		Drift: &DriftConfig{Interval: "4h"}}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Stack
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Drift == nil || out.Drift.Interval != "4h" {
		t.Errorf("Drift roundtrip failed: %+v", out)
	}
}
```

- [ ] **Step 2: Run — FAIL**

```
go test ./pkg/ir/ -run TestStackDriftRoundtrip
```

- [ ] **Step 3: Implement**

In `pkg/ir/types.go`, find the `Stack` struct definition and add:

```go
type Stack struct {
	// ... existing fields ...
	Drift *DriftConfig `json:"drift,omitempty" yaml:"drift,omitempty"`
}

// DriftConfig holds per-stack drift-detection scheduling overrides for
// the Phase 2 drift scheduler.
type DriftConfig struct {
	// Interval is a time.ParseDuration-compatible string ("1h", "30m", "4h").
	// Validator phase 6 rejects values < 60s; empty string means "use the
	// server default" (also true when Drift itself is nil).
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
}
```

- [ ] **Step 4: Tests pass**

```
go test ./pkg/ir/...
```

- [ ] **Step 5: Commit**

```bash
git add pkg/ir/types.go pkg/ir/types_test.go
git commit -m "ir: Stack.Drift + DriftConfig for per-stack drift schedule override"
```

---

### Task 5: Validator phase 6 (drift)

**Files:**
- Create: `internal/dsl/validator/phase6_drift.go`
- Create: `internal/dsl/validator/phase6_drift_test.go`
- Modify: `internal/dsl/validator/validator.go` — call phase6Drift after phase5

- [ ] **Step 1: Inspect existing phase dispatch**

```
grep -n "phase5Refs\|phase4\|Validate\|report.Issues" internal/dsl/validator/validator.go | head
```

You'll see the existing flow (validate dispatches each phase, collects issues).

- [ ] **Step 2: Write failing tests**

Create `internal/dsl/validator/phase6_drift_test.go`:

```go
package validator

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPhase6_DriftOK(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "4h"}},
	}}
	rep := &ir.ValidationReport{}
	if err := phase6Drift(proj, rep); err != nil {
		t.Fatalf("phase6: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("expected no issues; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftIntervalInvalid(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "not-a-duration"}},
	}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 1 || rep.Issues[0].Code != "ErrValidatorDriftIntervalInvalid" {
		t.Errorf("expected ErrValidatorDriftIntervalInvalid; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftIntervalTooShort(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "30s"}},
	}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 1 || rep.Issues[0].Code != "ErrValidatorDriftIntervalTooShort" {
		t.Errorf("expected ErrValidatorDriftIntervalTooShort; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftAbsent(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev"},
	}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 0 {
		t.Errorf("absent Drift should be valid; got %+v", rep.Issues)
	}
}
```

- [ ] **Step 3: Run — FAIL**

```
go test ./internal/dsl/validator/ -run TestPhase6
```

- [ ] **Step 4: Implement**

Create `internal/dsl/validator/phase6_drift.go`:

```go
package validator

import (
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

const driftMinimumInterval = 60 * time.Second

// phase6Drift validates per-stack drift schedule overrides.
func phase6Drift(proj *ir.Project, rep *ir.ValidationReport) error {
	for name, stack := range proj.Stacks {
		if stack.Drift == nil || stack.Drift.Interval == "" {
			continue
		}
		d, err := time.ParseDuration(stack.Drift.Interval)
		if err != nil {
			rep.Issues = append(rep.Issues, ir.Issue{
				Code: "ErrValidatorDriftIntervalInvalid",
				Message: fmt.Sprintf("stack %q drift.interval %q does not parse as a duration: %v",
					name, stack.Drift.Interval, err),
			})
			continue
		}
		if d < driftMinimumInterval {
			rep.Issues = append(rep.Issues, ir.Issue{
				Code: "ErrValidatorDriftIntervalTooShort",
				Message: fmt.Sprintf("stack %q drift.interval %q is below the minimum 60s",
					name, stack.Drift.Interval),
			})
		}
	}
	return nil
}
```

In `internal/dsl/validator/validator.go`, add the dispatch call:

```go
if err := phase6Drift(proj, report); err != nil {
	return nil, err
}
```

Insert after the existing `phase5Refs` call.

- [ ] **Step 5: Tests pass**

```
go test ./internal/dsl/validator/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/dsl/validator/phase6_drift.go internal/dsl/validator/phase6_drift_test.go internal/dsl/validator/validator.go
git commit -m "validator: phase 6 enforces drift.interval ≥ 60s and parseable"
```

---

### Task 6: `engine.persistPlan` writes drift_interval_seconds

**Files:**
- Modify: `pkg/engine/inventory.go`
- Modify: `pkg/engine/inventory_test.go` or `plan_test.go`

- [ ] **Step 1: Write failing test**

Find the existing `persistPlan` test in `pkg/engine/`. Add a new one:

```go
func TestPersistPlan_WritesDriftIntervalSeconds(t *testing.T) {
	// Set up engine + in-memory inventory ...
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{
			"dev": {Name: "dev",
				StateBackend: ir.StateBackend{Kind: "local"},
				Drift:        &ir.DriftConfig{Interval: "4h"}},
		},
		Components: []ir.Component{ /* one component, one target */ },
	}
	// Run engine.Plan ...
	deployment, err := repo.Deployments().Get(ctx, "test", planResult.DeploymentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if deployment.DriftIntervalSeconds != 4*3600 {
		t.Errorf("DriftIntervalSeconds=%d want 14400", deployment.DriftIntervalSeconds)
	}
}
```

Mirror the existing test pattern in the file for engine/repo setup.

- [ ] **Step 2: Run — FAIL**

```
go test ./pkg/engine/ -run TestPersistPlan_WritesDriftIntervalSeconds
```

- [ ] **Step 3: Implement**

In `pkg/engine/inventory.go`, find `persistPlan` (the function that calls `e.cfg.InventoryRepo.Deployments().Create`). Before the Create call, parse the stack's drift interval:

```go
var driftSecs int
if stack, ok := project.Stacks[stackName]; ok && stack.Drift != nil && stack.Drift.Interval != "" {
	if d, err := time.ParseDuration(stack.Drift.Interval); err == nil {
		driftSecs = int(d.Seconds())
	}
}
```

(Validator phase 6 already rejected unparseable values; the ignore-on-error here is defensive.)

Update the `inventory.Deployment{...}` literal to set `DriftIntervalSeconds: driftSecs`.

- [ ] **Step 4: Tests pass**

```
go test ./pkg/engine/...
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add pkg/engine/inventory.go <test file>
git commit -m "engine: persistPlan stores stack drift.interval as drift_interval_seconds"
```

---

### Task 7: `pkg/drift/notify` interface + Multi + Nop + DriftEvent

**Files:**
- Create: `pkg/drift/notify/notify.go`
- Create: `pkg/drift/notify/notify_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/drift/notify/notify_test.go`:

```go
package notify

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type recordingNotifier struct {
	calls atomic.Int32
	err   error
}

func (r *recordingNotifier) Notify(ctx context.Context, e DriftEvent) error {
	r.calls.Add(1)
	return r.err
}

func TestMultiNotifier_FanOut(t *testing.T) {
	a := &recordingNotifier{}
	b := &recordingNotifier{}
	m := MultiNotifier{a, b}
	if err := m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Errorf("each notifier should be called once; got a=%d b=%d", a.calls.Load(), b.calls.Load())
	}
}

func TestMultiNotifier_OnePartialFailureDoesNotBlockOthers(t *testing.T) {
	a := &recordingNotifier{err: errors.New("boom")}
	b := &recordingNotifier{}
	m := MultiNotifier{a, b}
	if err := m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"}); err != nil {
		t.Errorf("Multi should swallow per-transport errors; got %v", err)
	}
	if b.calls.Load() != 1 {
		t.Errorf("second notifier should still be called; got %d calls", b.calls.Load())
	}
}

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), DriftEvent{Kind: "x", DetectedAt: time.Now()}); err != nil {
		t.Errorf("NopNotifier returned error: %v", err)
	}
}
```

- [ ] **Step 2: Run — FAIL**

```
go test ./pkg/drift/notify/...
```

- [ ] **Step 3: Implement**

Create `pkg/drift/notify/notify.go`:

```go
// Package notify implements drift-event notifications. Three transports
// implement the Notifier interface — webhook, Slack, email — and a
// MultiNotifier fans events out to all configured transports. Failures of
// one transport do not block others.
package notify

import (
	"context"
	"log/slog"
	"time"
)

// DriftEvent is the canonical payload for a drift transition. Both
// "drift_detected" (clean → drifted) and "drift_resolved" (drifted →
// clean) events share this shape.
type DriftEvent struct {
	Kind          string            // "drift_detected" | "drift_resolved"
	DeploymentID  string
	ProjectID     string
	ProjectName   string
	Stack         string
	DetectedAt    time.Time
	Targets       []DriftEventTarget
	DeploymentURL string            // omitted (empty) when NIMBUSFAB_PUBLIC_URL unset
}

// DriftEventTarget is one target inside a DriftEvent.
type DriftEventTarget struct {
	ComponentName      string
	Type               string
	Cloud, Region      string
	Summary            string  // "+0 ~2 -0"
	DeploymentTargetID string
}

// Notifier is the contract every drift transport implements.
type Notifier interface {
	Notify(ctx context.Context, event DriftEvent) error
}

// MultiNotifier fans an event out to every contained Notifier
// concurrently. Per-transport errors are logged at WARN; the outer
// Notify always returns nil.
type MultiNotifier []Notifier

func (m MultiNotifier) Notify(ctx context.Context, event DriftEvent) error {
	for _, n := range m {
		go func(n Notifier) {
			if err := n.Notify(ctx, event); err != nil {
				slog.Warn("notify transport failed", "err", err, "event", event.Kind)
			}
		}(n)
	}
	return nil
}

// NopNotifier is the zero-config default — does nothing.
type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, DriftEvent) error { return nil }
```

- [ ] **Step 4: Tests pass**

```
go test ./pkg/drift/notify/...
```

Note: the fan-out is goroutine-based, so the test's `atomic.Int32` may not yet show 1 by the time `Notify` returns. Adjust either by adding a `sync.WaitGroup` to `MultiNotifier` (preferred — wait for all goroutines before returning) or sleeping in the test (brittle).

**Preferred fix:** wrap each goroutine in a `WaitGroup`:

```go
func (m MultiNotifier) Notify(ctx context.Context, event DriftEvent) error {
	var wg sync.WaitGroup
	for _, n := range m {
		wg.Add(1)
		go func(n Notifier) {
			defer wg.Done()
			if err := n.Notify(ctx, event); err != nil {
				slog.Warn("notify transport failed", "err", err, "event", event.Kind)
			}
		}(n)
	}
	wg.Wait()
	return nil
}
```

Re-run tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/drift/notify/notify.go pkg/drift/notify/notify_test.go
git commit -m "drift/notify: Notifier interface + MultiNotifier + NopNotifier + DriftEvent"
```

---

### Task 8: `WebhookNotifier`

**Files:**
- Create: `pkg/drift/notify/webhook.go`
- Create: `pkg/drift/notify/webhook_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/drift/notify/webhook_test.go`:

```go
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhook_HappyPath(t *testing.T) {
	var got DriftEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	err := w.Notify(context.Background(), DriftEvent{Kind: "drift_detected", DeploymentID: "dep-1"})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got.Kind != "drift_detected" || got.DeploymentID != "dep-1" {
		t.Errorf("payload not received: %+v", got)
	}
}

func TestWebhook_Retries5xxThenSucceeds(t *testing.T) {
	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempt.Add(1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	if err := w.Notify(context.Background(), DriftEvent{Kind: "x"}); err != nil {
		t.Errorf("expected retry to succeed; got %v", err)
	}
	if attempt.Load() != 2 {
		t.Errorf("expected 2 attempts; got %d", attempt.Load())
	}
}

func TestWebhook_AbortOn4xx(t *testing.T) {
	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt.Add(1)
		w.WriteHeader(400)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	if err := w.Notify(context.Background(), DriftEvent{Kind: "x"}); err == nil {
		t.Error("expected 4xx to return error")
	}
	if attempt.Load() != 1 {
		t.Errorf("4xx should NOT retry; got %d attempts", attempt.Load())
	}
}
```

- [ ] **Step 2: Run — FAIL**

```
go test ./pkg/drift/notify/ -run TestWebhook
```

- [ ] **Step 3: Implement**

Create `pkg/drift/notify/webhook.go`:

```go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookNotifier POSTs DriftEvents as application/json to URL. One retry
// on 5xx or transport error (first failure → wait Backoff, retry once;
// 4xx aborts immediately).
type WebhookNotifier struct {
	URL     string
	Client  *http.Client
	Backoff time.Duration
}

func (w *WebhookNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if w.URL == "" {
		return nil
	}
	client := w.Client
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("webhook %s: %w", w.URL, err)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return fmt.Errorf("webhook %s: %d %s (abort, no retry)", w.URL, resp.StatusCode, resp.Status)
		case resp.StatusCode >= 500:
			if attempt == 2 {
				return fmt.Errorf("webhook %s: %d %s after retry", w.URL, resp.StatusCode, resp.Status)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (w *WebhookNotifier) backoffFor(attempt int) time.Duration {
	if w.Backoff == 0 {
		w.Backoff = time.Second
	}
	if attempt == 1 {
		return w.Backoff
	}
	return w.Backoff * 4
}
```

- [ ] **Step 4: Tests pass**

```
go test ./pkg/drift/notify/...
```

- [ ] **Step 5: Commit**

```bash
git add pkg/drift/notify/webhook.go pkg/drift/notify/webhook_test.go
git commit -m "drift/notify: WebhookNotifier with one-retry-on-5xx and 4xx-abort"
```

---

### Task 9: `SlackNotifier`

**Files:**
- Create: `pkg/drift/notify/slack.go`
- Create: `pkg/drift/notify/slack_test.go`

- [ ] **Step 1: Write failing test**

Create `pkg/drift/notify/slack_test.go`:

```go
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlack_SendsSlackShape(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := &SlackNotifier{URL: srv.URL, Client: srv.Client()}
	err := n.Notify(context.Background(), DriftEvent{
		Kind: "drift_detected", DeploymentID: "dep-1", ProjectName: "demo",
		Targets: []DriftEventTarget{
			{ComponentName: "orders-db", Type: "database", Cloud: "aws", Region: "us-east-1", Summary: "+0 ~2 -0"},
		},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, ok := payload["text"]; !ok {
		t.Errorf("Slack payload missing 'text' field: %+v", payload)
	}
	attachments, _ := payload["attachments"].([]any)
	if len(attachments) == 0 {
		t.Errorf("Slack payload missing attachments: %+v", payload)
	}
}
```

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

Create `pkg/drift/notify/slack.go`:

```go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SlackNotifier POSTs DriftEvents to a Slack incoming-webhook URL,
// formatting the body with text + attachments. Reuses WebhookNotifier's
// retry / abort behavior internally.
type SlackNotifier struct {
	URL     string
	Client  *http.Client
	Backoff time.Duration
}

func (s *SlackNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if s.URL == "" {
		return nil
	}
	body := buildSlackPayload(event)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	jsonBody, _ := json.Marshal(body)
	// Reuse WebhookNotifier's POST + retry by delegating.
	w := &WebhookNotifier{URL: s.URL, Client: client, Backoff: s.Backoff}
	// We can't pass a body directly to WebhookNotifier's Notify(event); easier
	// to issue the request inline.
	for attempt := 1; attempt <= 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("slack %s: %w", s.URL, err)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return fmt.Errorf("slack %s: %d %s", s.URL, resp.StatusCode, resp.Status)
		}
		if attempt == 2 {
			return fmt.Errorf("slack %s: %d %s after retry", s.URL, resp.StatusCode, resp.Status)
		}
		select {
		case <-time.After(w.backoffFor(attempt)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func buildSlackPayload(event DriftEvent) map[string]any {
	color := "warning"
	verb := "detected"
	if event.Kind == "drift_resolved" {
		color = "good"
		verb = "resolved"
	}
	parts := []string{fmt.Sprintf("Drift %s in deployment %s", verb, event.DeploymentID)}
	for _, t := range event.Targets {
		parts = append(parts, fmt.Sprintf("• %s (%s) on %s/%s — %s",
			t.ComponentName, t.Type, t.Cloud, t.Region, t.Summary))
	}
	if event.DeploymentURL != "" {
		parts = append(parts, "<"+event.DeploymentURL+"|View deployment>")
	}
	return map[string]any{
		"text": strings.Join(parts, "\n"),
		"attachments": []map[string]any{
			{
				"color":    color,
				"fallback": fmt.Sprintf("Drift %s: %s", verb, event.DeploymentID),
				"fields": []map[string]any{
					{"title": "Project", "value": event.ProjectName, "short": true},
					{"title": "Stack", "value": event.Stack, "short": true},
				},
			},
		},
	}
}
```

- [ ] **Step 4: Tests pass**

- [ ] **Step 5: Commit**

```bash
git add pkg/drift/notify/slack.go pkg/drift/notify/slack_test.go
git commit -m "drift/notify: SlackNotifier with block-kit shaped payload"
```

---

### Task 10: `SMTPNotifier`

**Files:**
- Create: `pkg/drift/notify/smtp.go`
- Create: `pkg/drift/notify/smtp_test.go`
- Create: `internal/notify/templates/drift_email.txt` (embed.FS asset)

- [ ] **Step 1: Create the email template**

Create `internal/notify/templates/drift_email.txt`:

```
Subject: [nimbusfab] Drift {{.Kind | verbForKind}}: {{.DeploymentID}}

Project: {{.ProjectName}}
Stack: {{.Stack}}
Detected: {{.DetectedAt.Format "2006-01-02 15:04:05 UTC"}}

Affected targets:
{{range .Targets -}}
- {{.ComponentName}} ({{.Type}}) on {{.Cloud}}/{{.Region}} — {{.Summary}}
{{end}}
{{if .DeploymentURL}}
View: {{.DeploymentURL}}
{{end}}
```

- [ ] **Step 2: Write failing test**

Create `pkg/drift/notify/smtp_test.go`:

```go
package notify

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSMTP is a tiny SMTP server good enough to capture HELO/MAIL/RCPT/DATA.
type mockSMTP struct {
	listener net.Listener
	captured strings.Builder
	mu       sync.Mutex
}

func newMockSMTP(t *testing.T) *mockSMTP {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &mockSMTP{listener: l}
	go m.serve()
	return m
}

func (m *mockSMTP) addr() string { return m.listener.Addr().String() }
func (m *mockSMTP) body() string  { m.mu.Lock(); defer m.mu.Unlock(); return m.captured.String() }

func (m *mockSMTP) serve() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			fmt.Fprintf(conn, "220 mock SMTP\r\n")
			buf := make([]byte, 4096)
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				line := string(buf[:n])
				m.mu.Lock()
				m.captured.WriteString(line)
				m.mu.Unlock()
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					fmt.Fprintf(conn, "250 OK\r\n")
				case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
					fmt.Fprintf(conn, "250 OK\r\n")
				case strings.HasPrefix(line, "DATA"):
					fmt.Fprintf(conn, "354 OK\r\n")
				case strings.HasPrefix(line, "QUIT"):
					fmt.Fprintf(conn, "221 OK\r\n")
					return
				case strings.HasSuffix(line, "\r\n.\r\n"):
					fmt.Fprintf(conn, "250 OK\r\n")
				}
			}
		}()
	}
}

func TestSMTP_SendsFormattedEmail(t *testing.T) {
	srv := newMockSMTP(t)
	defer srv.listener.Close()
	host, port, _ := net.SplitHostPort(srv.addr())
	notifier := &SMTPNotifier{
		Host: host, Port: port,
		From: "drift@nimbusfab.example", To: []string{"ops@nimbusfab.example"},
		Timeout: 2 * time.Second,
	}
	err := notifier.Notify(context.Background(), DriftEvent{
		Kind: "drift_detected", DeploymentID: "dep-1",
		ProjectName: "demo", Stack: "dev",
		DetectedAt: time.Now(),
		Targets: []DriftEventTarget{{ComponentName: "orders-db", Type: "database",
			Cloud: "aws", Region: "us-east-1", Summary: "+0 ~2 -0"}},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	body := srv.body()
	if !strings.Contains(body, "orders-db") || !strings.Contains(body, "dep-1") {
		t.Errorf("expected email body to mention orders-db + dep-1; got: %s", body)
	}
}
```

The test uses a homegrown mock SMTP. Add `"fmt"` import.

- [ ] **Step 3: Run — FAIL**

- [ ] **Step 4: Implement**

Create `pkg/drift/notify/smtp.go`:

```go
package notify

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"net/smtp"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/drift_email.txt
var emailTemplateFS embed.FS

// SMTPNotifier sends DriftEvents as plaintext emails via SMTP with PlainAuth.
// Subject + body are rendered from internal/notify/templates/drift_email.txt.
type SMTPNotifier struct {
	Host, Port string
	User, Pass string
	From       string
	To         []string
	Timeout    time.Duration
}

var emailTmpl = func() *template.Template {
	body, _ := emailTemplateFS.ReadFile("templates/drift_email.txt")
	t, _ := template.New("email").Funcs(template.FuncMap{
		"verbForKind": func(k string) string {
			if k == "drift_resolved" {
				return "resolved"
			}
			return "detected"
		},
	}).Parse(string(body))
	return t
}()

func (s *SMTPNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if s.Host == "" || len(s.To) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := emailTmpl.Execute(&buf, event); err != nil {
		return fmt.Errorf("smtp template: %w", err)
	}
	// The template begins with "Subject: ..." so the raw bytes are
	// already in RFC-822 header-then-body shape.
	msg := buf.Bytes()
	// Prepend To/From headers expected by SMTP clients.
	full := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\n", s.From, strings.Join(s.To, ", ")))
	full = append(full, msg...)

	port := s.Port
	if port == "" {
		port = "587"
	}
	addr := s.Host + ":" + port
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	return smtp.SendMail(addr, auth, s.From, s.To, full)
}
```

Note: `net/smtp.SendMail` doesn't take a context. For Phase 2 simplicity we accept this; future enhancement could be a custom Dial with context-cancel support.

The path `templates/drift_email.txt` in the `//go:embed` directive assumes the template is in `pkg/drift/notify/templates/drift_email.txt` (sibling to `smtp.go`). Move the template there if you wrote it to `internal/notify/templates/`. Update accordingly.

- [ ] **Step 5: Tests pass**

```
go test ./pkg/drift/notify/...
```

- [ ] **Step 6: Commit**

```bash
git add pkg/drift/notify/smtp.go pkg/drift/notify/smtp_test.go pkg/drift/notify/templates/drift_email.txt
git commit -m "drift/notify: SMTPNotifier with PlainAuth + plaintext template"
```

---

### Task 11: `FromEnv` constructor

**Files:**
- Create: `pkg/drift/notify/fromenv.go`
- Create: `pkg/drift/notify/fromenv_test.go`

- [ ] **Step 1: Failing test**

Create `pkg/drift/notify/fromenv_test.go`:

```go
package notify

import (
	"context"
	"testing"
)

func TestFromEnv_NoVars_ReturnsNop(t *testing.T) {
	t.Setenv("NIMBUSFAB_DRIFT_WEBHOOK_URL", "")
	t.Setenv("NIMBUSFAB_DRIFT_SLACK_URL", "")
	t.Setenv("NIMBUSFAB_SMTP_HOST", "")
	n := FromEnv()
	if _, ok := n.(NopNotifier); !ok {
		t.Errorf("expected NopNotifier when no env vars set; got %T", n)
	}
}

func TestFromEnv_AllConfigured(t *testing.T) {
	t.Setenv("NIMBUSFAB_DRIFT_WEBHOOK_URL", "http://example.com/hook")
	t.Setenv("NIMBUSFAB_DRIFT_SLACK_URL", "http://hooks.slack.com/x")
	t.Setenv("NIMBUSFAB_SMTP_HOST", "smtp.example.com")
	t.Setenv("NIMBUSFAB_SMTP_FROM", "drift@example.com")
	t.Setenv("NIMBUSFAB_DRIFT_EMAIL_TO", "ops@example.com,sre@example.com")
	n := FromEnv()
	m, ok := n.(MultiNotifier)
	if !ok {
		t.Fatalf("expected MultiNotifier; got %T", n)
	}
	if len(m) != 3 {
		t.Errorf("expected 3 notifiers; got %d", len(m))
	}
	// Smoke: calling Notify should not panic.
	_ = m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"})
}
```

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

Create `pkg/drift/notify/fromenv.go`:

```go
package notify

import (
	"os"
	"strings"
)

// FromEnv constructs a Notifier from environment variables:
//   NIMBUSFAB_DRIFT_WEBHOOK_URL — generic webhook destination.
//   NIMBUSFAB_DRIFT_SLACK_URL   — Slack incoming-webhook URL.
//   NIMBUSFAB_SMTP_HOST/_PORT/_USER/_PASS/_FROM and NIMBUSFAB_DRIFT_EMAIL_TO
//                               — SMTP email destination.
// Returns NopNotifier when nothing is configured.
func FromEnv() Notifier {
	var ns MultiNotifier
	if u := os.Getenv("NIMBUSFAB_DRIFT_WEBHOOK_URL"); u != "" {
		ns = append(ns, &WebhookNotifier{URL: u})
	}
	if u := os.Getenv("NIMBUSFAB_DRIFT_SLACK_URL"); u != "" {
		ns = append(ns, &SlackNotifier{URL: u})
	}
	if h := os.Getenv("NIMBUSFAB_SMTP_HOST"); h != "" {
		to := os.Getenv("NIMBUSFAB_DRIFT_EMAIL_TO")
		var recipients []string
		for _, p := range strings.Split(to, ",") {
			if p = strings.TrimSpace(p); p != "" {
				recipients = append(recipients, p)
			}
		}
		if len(recipients) > 0 {
			ns = append(ns, &SMTPNotifier{
				Host: h, Port: os.Getenv("NIMBUSFAB_SMTP_PORT"),
				User: os.Getenv("NIMBUSFAB_SMTP_USER"),
				Pass: os.Getenv("NIMBUSFAB_SMTP_PASS"),
				From: os.Getenv("NIMBUSFAB_SMTP_FROM"),
				To:   recipients,
			})
		}
	}
	if len(ns) == 0 {
		return NopNotifier{}
	}
	return ns
}
```

- [ ] **Step 4: Tests pass**

- [ ] **Step 5: Commit**

```bash
git add pkg/drift/notify/fromenv.go pkg/drift/notify/fromenv_test.go
git commit -m "drift/notify: FromEnv composes notifiers from env vars (Nop fallback)"
```

---

### Task 12: `Scheduler`

**Files:**
- Create: `pkg/drift/scheduler/scheduler.go`
- Create: `pkg/drift/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/drift/scheduler/scheduler_test.go`:

```go
package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/drift/notify"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type fakeEngine struct {
	calls atomic.Int32
	delay time.Duration
}

func (f *fakeEngine) DetectDrift(ctx context.Context, deploymentID string, opts engine.DriftOpts) (*engine.DriftReport, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return &engine.DriftReport{}, nil
}

// Add the other engine.Engine methods as no-ops if Engine is an interface.

type fakeRepo struct {
	deployments []inventory.Deployment
	latestRows  map[string][]inventory.DriftStatus  // deploymentID → latest rows
	mu          sync.Mutex
}

// Implement enough of inventory.Repo to drive the scheduler:
//   Deployments().ListAll(ctx, orgID) → f.deployments
//   DriftStatus().LatestByDeployment(ctx, orgID, depID) → f.latestRows[depID]
// (Add a minimal repo implementation in the test file.)

func TestScheduler_NoDeployments_NoOp(t *testing.T) {
	eng := &fakeEngine{}
	repo := &fakeRepo{}
	s := &Scheduler{Engine: eng, Repo: repo, Notifier: notify.NopNotifier{},
		GlobalInterval: 100 * time.Millisecond, MaxConcurrent: 1,
		Now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	if eng.calls.Load() != 0 {
		t.Errorf("expected 0 DetectDrift calls; got %d", eng.calls.Load())
	}
}

func TestScheduler_PastDueDeployment_Triggered(t *testing.T) {
	eng := &fakeEngine{}
	repo := &fakeRepo{
		deployments: []inventory.Deployment{
			{ID: "dep-1", OrgID: "default", Status: "applied",
				DriftIntervalSeconds: 0,  // use global
				StartedAt: time.Now().Add(-time.Hour)},
		},
	}
	s := &Scheduler{Engine: eng, Repo: repo, Notifier: notify.NopNotifier{},
		GlobalInterval: 50 * time.Millisecond, MaxConcurrent: 1,
		Now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	if eng.calls.Load() == 0 {
		t.Error("expected DetectDrift to be called at least once")
	}
}

// Additional tests: override beats global, concurrency cap, edge transition fires Notify.
```

The tests need a small `fakeRepo` that implements just enough of `inventory.Repo` for the scheduler. Mock the methods the scheduler actually calls — `Deployments().ListAll`, `DriftStatus().LatestByDeployment`. Other methods can panic if invoked.

Look at how prior phase tests built fakes (e.g., `pkg/provisioner/apply_test.go` uses `tofu.FakeRunner`).

- [ ] **Step 2: Run — FAIL**

```
go test ./pkg/drift/scheduler/...
```

- [ ] **Step 3: Implement**

Create `pkg/drift/scheduler/scheduler.go`:

```go
// Package scheduler runs background drift detection across all active
// deployments at a configurable cadence. Reads per-deployment override
// drift_interval_seconds, falls back to the server-wide default. Fires
// notifications on drift transitions (clean → drifted, drifted → clean).
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/klehmer/nimbusfab/pkg/drift/notify"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type Scheduler struct {
	Engine         engine.Engine
	Repo           inventory.Repo
	Notifier       notify.Notifier
	GlobalInterval time.Duration
	OrgID          string
	MaxConcurrent  int
	Now            func() time.Time
	Logger         *slog.Logger
}

func (s *Scheduler) Run(ctx context.Context) error {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.MaxConcurrent <= 0 {
		s.MaxConcurrent = 4
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	tick := s.tickInterval()
	t := time.NewTicker(tick)
	defer t.Stop()
	s.runTick(ctx)  // run once at boot, then on schedule
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			s.runTick(ctx)
		}
	}
}

func (s *Scheduler) tickInterval() time.Duration {
	const minTick = time.Minute
	if s.GlobalInterval < minTick {
		return s.GlobalInterval
	}
	return minTick
}

func (s *Scheduler) runTick(ctx context.Context) {
	deployments, err := s.Repo.Deployments().ListAll(ctx, s.OrgID)
	if err != nil {
		s.Logger.Warn("drift scheduler: ListAll", "err", err)
		return
	}
	due := s.dueDeployments(ctx, deployments)
	sem := make(chan struct{}, s.MaxConcurrent)
	var wg sync.WaitGroup
	for _, d := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(d inventory.Deployment) {
			defer wg.Done()
			defer func() { <-sem }()
			s.driftOne(ctx, d)
		}(d)
	}
	wg.Wait()
}

func (s *Scheduler) dueDeployments(ctx context.Context, all []inventory.Deployment) []inventory.Deployment {
	var due []inventory.Deployment
	for _, d := range all {
		if !isApplied(d) {
			continue
		}
		eff := s.effectiveInterval(d)
		if eff == 0 {
			continue
		}
		latest, err := s.Repo.DriftStatus().LatestByDeployment(ctx, s.OrgID, d.ID)
		if err != nil {
			s.Logger.Warn("drift scheduler: LatestByDeployment", "deployment", d.ID, "err", err)
			continue
		}
		lastDetectedAt := latestDetectedAt(latest)
		if s.Now().Sub(lastDetectedAt) >= eff {
			due = append(due, d)
		}
	}
	return due
}

func isApplied(d inventory.Deployment) bool {
	switch d.Status {
	case "applied", "drift_clean", "drift_detected", "succeeded":
		return true
	}
	return false
}

func (s *Scheduler) effectiveInterval(d inventory.Deployment) time.Duration {
	if d.DriftIntervalSeconds > 0 {
		return time.Duration(d.DriftIntervalSeconds) * time.Second
	}
	return s.GlobalInterval
}

func latestDetectedAt(rows []inventory.DriftStatus) time.Time {
	var t time.Time
	for _, r := range rows {
		if r.DetectedAt.After(t) {
			t = r.DetectedAt
		}
	}
	return t
}

func (s *Scheduler) driftOne(ctx context.Context, d inventory.Deployment) {
	priorRows, _ := s.Repo.DriftStatus().LatestByDeployment(ctx, s.OrgID, d.ID)
	priorByTarget := map[string]bool{}
	for _, r := range priorRows {
		priorByTarget[r.DeploymentTargetID] = r.HasDrift
	}
	if _, err := s.Engine.DetectDrift(ctx, d.ID, engine.DriftOpts{}); err != nil {
		s.Logger.Warn("drift scheduler: DetectDrift", "deployment", d.ID, "err", err)
		return
	}
	currRows, _ := s.Repo.DriftStatus().LatestByDeployment(ctx, s.OrgID, d.ID)
	events := transitionEvents(priorByTarget, currRows, d)
	for _, ev := range events {
		if err := s.Notifier.Notify(ctx, ev); err != nil {
			s.Logger.Warn("drift scheduler: Notify", "err", err)
		}
	}
}

// transitionEvents diffs prior vs current and returns one DriftEvent per
// detected/resolved transition.
func transitionEvents(priorHasDrift map[string]bool, curr []inventory.DriftStatus, d inventory.Deployment) []notify.DriftEvent {
	var detected, resolved []notify.DriftEventTarget
	for _, r := range curr {
		prior := priorHasDrift[r.DeploymentTargetID]
		if !prior && r.HasDrift {
			detected = append(detected, notify.DriftEventTarget{
				DeploymentTargetID: r.DeploymentTargetID,
				Summary:            r.Summary,
			})
		} else if prior && !r.HasDrift {
			resolved = append(resolved, notify.DriftEventTarget{
				DeploymentTargetID: r.DeploymentTargetID,
				Summary:            r.Summary,
			})
		}
	}
	var out []notify.DriftEvent
	if len(detected) > 0 {
		out = append(out, notify.DriftEvent{
			Kind: "drift_detected", DeploymentID: d.ID, Stack: d.StackID,
			DetectedAt: time.Now().UTC(), Targets: detected,
		})
	}
	if len(resolved) > 0 {
		out = append(out, notify.DriftEvent{
			Kind: "drift_resolved", DeploymentID: d.ID, Stack: d.StackID,
			DetectedAt: time.Now().UTC(), Targets: resolved,
		})
	}
	return out
}
```

Note: `engine.Engine` is an interface; the test's `fakeEngine` must satisfy it. If the interface has many methods, embed `engine.Engine` (nil) and only override `DetectDrift`. Or use an `EngineSubset` defined locally:

```go
type DriftEngine interface {
	DetectDrift(ctx context.Context, deploymentID string, opts engine.DriftOpts) (*engine.DriftReport, error)
}
```

Then `Scheduler.Engine DriftEngine` for narrow contract. This is cleaner for testing — adopt it.

`DriftEventTarget` fields ComponentName, Type, Cloud, Region: the scheduler should populate them from joining `deployment_targets` data. For Phase 2 simplicity, populate only DeploymentTargetID + Summary; downstream UI / notification consumers can join if they want richer fields. (Or add a `DriftStatus.ComponentName, Cloud, Region` view via a join in `LatestByDeployment` — preferable. Adjust the SQL accordingly: `JOIN deployment_targets dt ON dt.id = ds.deployment_target_id` and project `dt.component_name, dt.cloud, dt.region`.)

- [ ] **Step 4: Tests pass**

```
go test ./pkg/drift/scheduler/...
```

- [ ] **Step 5: Commit**

```bash
git add pkg/drift/scheduler/scheduler.go pkg/drift/scheduler/scheduler_test.go
git commit -m "drift/scheduler: ticker loop, per-deployment override, edge-transition notifies"
```

---

### Task 13: `cmd/server/main.go` wires scheduler + notifier

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Inspect**

```
cat cmd/server/main.go | head -100
```

Find where the Engine + Repo are constructed.

- [ ] **Step 2: Add scheduler boot**

After the existing Engine construction and BEFORE `srv.ListenAndServe()`, add:

```go
notif := notify.FromEnv()
if interval := parseDuration(os.Getenv("NIMBUSFAB_DRIFT_INTERVAL")); interval > 0 {
	sched := &scheduler.Scheduler{
		Engine: eng, Repo: repo, Notifier: notif,
		GlobalInterval: interval, OrgID: orgID,
		MaxConcurrent: 4,
	}
	go func() {
		if err := sched.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "drift scheduler exited:", err)
		}
	}()
	fmt.Printf("drift scheduler active (interval=%s, notifier configured: %t)\n", interval, hasNotifier(notif))
}
```

Add helpers:

```go
func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func hasNotifier(n notify.Notifier) bool {
	_, isNop := n.(notify.NopNotifier)
	return !isNop
}
```

Add imports for `errors`, `pkg/drift/notify`, `pkg/drift/scheduler`, `time`.

- [ ] **Step 3: Smoke test**

Run the server with env vars set to a short interval. It should not crash. (Note: full integration verification with a real DetectDrift run is done in Task 14's integration suite, not here.)

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "server: boot drift scheduler when NIMBUSFAB_DRIFT_INTERVAL set"
```

---

### Task 14: UI — per-project drift page + global drift filter + page-tabs

**Files:**
- Create: `internal/webapi/ui/templates/drift_project.html`
- Modify: `internal/webapi/ui/templates/drift.html`
- Modify: `internal/webapi/ui/templates/project_detail.html`
- Modify: `internal/webapi/ui/pages.go` — new `ProjectDrift` handler; enrich the global `Drift` handler with optional `?project=` filter
- Modify: `internal/webapi/router.go` — register `/ui/projects/{id}/drift`
- Modify: `internal/webapi/router_test.go`

- [ ] **Step 1: Survey existing global Drift handler**

```
grep -n "func.*Drift\|/ui/drift" internal/webapi/ui/pages.go internal/webapi/router.go
```

- [ ] **Step 2: Failing tests**

Append to `internal/webapi/router_test.go`:

```go
func TestUI_ProjectDrift_Renders(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()
	// Seed a drift_status row for p-1. (newTestServerWithSeedData seeds p-1.)
	// You may need to add the row inside the seed function or via a follow-up insert.
	resp, body := get(t, srv, "/ui/projects/p-1/drift")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Drift") {
		t.Errorf("body missing 'Drift' heading; body:\n%s", body)
	}
}

func TestUI_GlobalDrift_ProjectFilter(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()
	resp, body := get(t, srv, "/ui/drift?project=p-1")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if !strings.Contains(body, `name="project"`) {
		t.Errorf("expected project filter form; body:\n%s", body)
	}
}
```

If the seed function doesn't write a drift_status row, extend it to include one for p-1.

- [ ] **Step 3: Run — FAIL**

```
go test ./internal/webapi/ -run "TestUI_ProjectDrift|TestUI_GlobalDrift"
```

- [ ] **Step 4: Create the new template**

Create `internal/webapi/ui/templates/drift_project.html`:

```html
{{define "title"}}Drift — {{.ProjectName}}{{end}}
{{define "content"}}
<h1>Drift — {{.ProjectName}}</h1>

<nav class="page-tabs">
  <a href="/ui/projects/{{.ProjectID}}">Overview</a> ·
  <a href="/ui/projects/{{.ProjectID}}/graph">Graph</a> ·
  <a href="/ui/projects/{{.ProjectID}}/drift"><strong>Drift</strong></a>
</nav>

{{if .HasData}}
<p>
  <strong>{{.Summary.Total}}</strong> targets monitored —
  <span class="badge fail">{{.Summary.Drifted}} drifted</span>
  <span class="badge ok">{{.Summary.Clean}} clean</span>
</p>
<table>
  <thead><tr><th>Status</th><th>Component</th><th>Cloud</th><th>Region</th><th>Detected</th><th>Deployment</th></tr></thead>
  <tbody>
  {{range .Records}}
  <tr>
    <td>{{if .HasDrift}}<span class="badge fail">drifted</span>{{else}}<span class="badge ok">clean</span>{{end}}</td>
    <td>{{.ComponentName}}</td>
    <td><span class="badge">{{.Cloud}}</span></td>
    <td><code>{{.Region}}</code></td>
    <td>{{.DetectedAt.Format "2006-01-02 15:04:05 UTC"}}</td>
    <td><a href="/ui/deployments/{{.DeploymentID}}"><code>{{shortID .DeploymentID}}</code></a></td>
  </tr>
  {{end}}
  </tbody>
</table>
{{else}}
<p class="muted">No drift checks recorded for this project yet.</p>
{{end}}
{{end}}
```

- [ ] **Step 5: Add `ProjectDrift` handler in `pages.go`**

```go
func (r *Renderer) ProjectDrift(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	proj, err := r.Repo.Projects().Get(req.Context(), r.OrgID, id)
	if err != nil || proj == nil {
		r.renderError(w, http.StatusNotFound, "project not found: "+id)
		return
	}
	rows, err := r.Repo.DriftStatus().ListByProject(req.Context(), r.OrgID, id)
	if err != nil {
		r.renderError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Build summary + record view models — reuse the existing helper if there
	// is one, or build inline:
	records, summary := buildDriftView(rows)  // assume the helper already exists; if not, mirror Drift()'s logic
	r.render(w, "drift_project.html", r.withUser(req, map[string]any{
		"ProjectID":   proj.ID,
		"ProjectName": proj.Name,
		"HasData":     len(rows) > 0,
		"Records":     records,
		"Summary":     summary,
	}))
}
```

You may need to refactor the existing `Drift` handler to extract a `buildDriftView` helper. Match the existing data shape so the template renders.

- [ ] **Step 6: Enrich the global `Drift` handler**

Update `Drift(w, req)` to:
1. Read `req.URL.Query().Get("project")`.
2. If set and non-empty, call `r.Repo.DriftStatus().ListByProject(ctx, r.OrgID, projectID)`; otherwise the existing all-records query.
3. Pass a `Projects []Project` list to the template (for the filter dropdown) and the `SelectedProject` string.

Update `internal/webapi/ui/templates/drift.html`:
- Add `<form method="GET">` with a `<select name="project" onchange="this.form.submit()">` dropdown above the existing table.
- Add a `<th>Project</th>` column and a `<td><a href="/ui/projects/{{.ProjectID}}">{{.ProjectName}}</a></td>` cell in the loop.

`ListByProject` already exists (Task 3). `Projects().List(orgID)` should already exist (used by `ListProjects` handler — check); if not, add it.

- [ ] **Step 7: Register the route**

In `internal/webapi/router.go`:

```go
mux.Handle("GET /ui/projects/{id}/drift", uiAuth(http.HandlerFunc(renderer.ProjectDrift)))
```

- [ ] **Step 8: Update page-tabs on project_detail.html**

Find the page-tabs row (added by the DAG UI work). Add the Drift entry:

```html
<nav class="page-tabs">
  <a href="/ui/projects/{{.ID}}"><strong>Overview</strong></a> ·
  <a href="/ui/projects/{{.ID}}/graph">Graph</a> ·
  <a href="/ui/projects/{{.ID}}/drift">Drift</a>
</nav>
```

Adjust ID accessor to match what the template uses.

- [ ] **Step 9: Tests pass**

```
go test ./internal/webapi/...
go test ./...
go build ./...
```

- [ ] **Step 10: Commit**

```bash
git add internal/webapi/ui/templates/drift_project.html \
        internal/webapi/ui/templates/drift.html \
        internal/webapi/ui/templates/project_detail.html \
        internal/webapi/ui/pages.go \
        internal/webapi/router.go \
        internal/webapi/router_test.go
git commit -m "webapi: /ui/projects/{id}/drift + project filter on global /ui/drift + page-tabs"
```

---

## Self-Review Checklist

After all 14 tasks:

- [ ] `git log --oneline main..HEAD` shows ~14 commits.
- [ ] `go test ./...` passes.
- [ ] `nimbusfab-server` starts with `NIMBUSFAB_DRIFT_INTERVAL=2s NIMBUSFAB_DRIFT_WEBHOOK_URL=http://127.0.0.1:9999/test` and the scheduler exits cleanly on SIGTERM.
- [ ] An applied deployment seeded into the inventory triggers drift detection within ~2s of server boot.
- [ ] When drift transitions (clean → drifted), a POST hits the webhook URL with the documented JSON shape.
- [ ] `/ui/projects/{id}/drift` renders.
- [ ] `/ui/drift?project=p-1` filters correctly.
- [ ] Validator phase 6 rejects `drift.interval: 30s` and `drift.interval: bogus`.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-drift-phase2.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, two-stage review.

**2. Inline Execution** — `executing-plans`, batched checkpoints.

Which approach?
