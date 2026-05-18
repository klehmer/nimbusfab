# Drift Phase 2 — Background Cron + Notifications + Per-Project Drift UI

**Status:** Subsystem spec. Drift Phase 1 (merged 2026-05-16) shipped on-demand drift detection: `nimbusfab drift <id>` CLI + browser-triggered drift via the mutating endpoint with SSE. Phase 2 adds a background scheduler in `nimbusfab-server` that periodically scans active deployments, three notification transports (webhook, Slack, email) firing on edge transitions, and a per-project drift UI.

**Date:** 2026-05-18

**Depends on:**
- `docs/superpowers/specs/2026-05-16-drift-phase1-overview.md` — on-demand drift, the `drift_status` table, the `DriftStatus` repo, `/ui/drift` flat list.
- `docs/superpowers/specs/2026-05-17-cross-component-planning-design.md` — `provisioner.DetectDrift` (toposort, real-or-skip vars) is the engine call the scheduler invokes.
- `docs/superpowers/specs/2026-05-16-webapp-design.md` — server-rendered HTML pattern; vanilla JS + minimal CSS; no SPA framework.

**Depended on by:**
- Drift Phase 3 (v1.3+, not specced) — daily-digest emails for persistent drift, drift-event log retention controls.
- Notifications subsystem reuse — webhook/Slack/email infrastructure may be lifted to a generic notifier for apply-failure / cost-threshold alerts post-v1.2.

---

## Context

User-test session #1 confirmed drift detection works on-demand but no one will remember to run it. Phase 2 turns drift into an always-on signal: the server enumerates active deployments on a schedule, runs drift, and notifies on transitions (clean→drifted, drifted→clean). Spam-avoidance is built-in — persistent drift doesn't re-fire.

## Design principles

1. **One scheduler in-process.** No external cron required. Self-hosted nimbusfab-server starts the scheduler at boot, stops it on shutdown. External cron remains usable via the existing `/api/v1/deployments/{id}/drifts` endpoint for users who prefer it.
2. **Server-wide default + per-stack override.** A single `NIMBUSFAB_DRIFT_INTERVAL` env var covers the common case. Stacks that need a different cadence (e.g., expensive multi-cloud DB drift every 6h, cheap network drift every 30m) opt-in via YAML.
3. **Edge transitions, not state.** Notifications fire when `has_drift` *changes*. Persistent drift is silent until resolved.
4. **Three transports, one event.** Webhook / Slack / email are independent. Failure of one doesn't block others. Notifier errors never block detection.
5. **No new auth surface.** Notification destinations are server-level env vars. Per-deployment notification routing is deferred to v1.3+.

## Architecture

### Scheduler

`pkg/drift/scheduler` — new package. One exported type:

```go
type Scheduler struct {
    Engine        engine.Engine
    Repo          inventory.Repo
    Notifier      notify.Notifier
    GlobalInterval time.Duration
    OrgID         string
    MaxConcurrent int
    Now           func() time.Time   // for tests
    Logger        *slog.Logger
}

func (s *Scheduler) Run(ctx context.Context) error
```

`Run` blocks until ctx is cancelled. Internally:
1. Tick on `time.NewTicker(min(globalInterval, 60s))` (clamp to 60s minimum tick rate; longer intervals just skip more ticks).
2. Each tick: `repo.Deployments().ListAll(orgID)` (new repo method, filter `Status` to "applied" / "drift_clean" / "drift_detected" — anything post-apply).
3. For each deployment, compute `effectiveInterval`: `drift_interval_seconds` from the row if non-zero, else `s.GlobalInterval`.
4. Compute `lastDriftAt` from the latest `drift_status` row for any of the deployment's targets (max of `detected_at`).
5. If `s.Now().Sub(lastDriftAt) >= effectiveInterval`, queue this deployment for drift.
6. Run queued drifts in a goroutine pool of size `s.MaxConcurrent` (default 4). Each goroutine calls `engine.DetectDrift(ctx, deploymentID, opts)`.
7. After each `DetectDrift` returns: read the prior drift_status row (the one PRE-tick), compare `has_drift` to the new one — if it flipped, fire notifications via `s.Notifier.Notify`.

### Notifier

`pkg/drift/notify` — new package. One interface:

```go
type Notifier interface {
    Notify(ctx context.Context, event DriftEvent) error
}

type DriftEvent struct {
    Kind          string   // "drift_detected" or "drift_resolved"
    DeploymentID  string
    ProjectID     string
    ProjectName   string
    Stack         string
    DetectedAt    time.Time
    Targets       []DriftEventTarget
    DeploymentURL string   // empty if NIMBUSFAB_PUBLIC_URL not set
}

type DriftEventTarget struct {
    ComponentName      string
    Type               string
    Cloud, Region      string
    Summary            string  // "+0 ~2 -0" formatted adds/changes/destroys
    DeploymentTargetID string
}
```

Three concrete implementations:

**`WebhookNotifier`** — sends `application/json` POST with the `DriftEvent` body. One retry on 5xx / transport error: backoff 1s → 4s. 4xx aborts. Configured by `NIMBUSFAB_DRIFT_WEBHOOK_URL`.

**`SlackNotifier`** — wraps `WebhookNotifier`-style transport but formats the body as Slack's `{"text": ..., "attachments": [...]}` shape. `color: "warning"` for detected, `"good"` for resolved. Configured by `NIMBUSFAB_DRIFT_SLACK_URL`.

**`SMTPNotifier`** — `net/smtp` with PlainAuth. Config: `NIMBUSFAB_SMTP_HOST`, `_PORT` (default 587), `_USER`, `_PASS`, `_FROM`, `NIMBUSFAB_DRIFT_EMAIL_TO` (comma-separated). Body rendered from `internal/notify/templates/drift_email.txt` (text/template; embed.FS).

**`MultiNotifier`** — composite that fans out to all configured transports concurrently. Logs per-transport errors at WARN; the outer `Notify` returns nil even when one transport fails (drift detection never blocks on notification).

### Schema change

```sql
-- 0004_drift_interval.sql (both sqlite + postgres)
ALTER TABLE deployments ADD COLUMN drift_interval_seconds INTEGER NOT NULL DEFAULT 0;
```

`0` is the sentinel for "use server default."

### IR + Validator changes

**`pkg/ir/types.go`** — `Stack` struct gains:
```go
Drift *DriftConfig `json:"drift,omitempty" yaml:"drift,omitempty"`

type DriftConfig struct {
    Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
}
```

`Interval` is a `time.ParseDuration`-compatible string. Stored as string in the IR for round-trip fidelity; parsed at validate / persistence time.

**`internal/dsl/validator`** — new Phase 6 (`phase6_drift.go`) checks:
- `drift.interval` parses as a `time.Duration` ≥ 60 seconds.
- Error codes: `ErrValidatorDriftIntervalInvalid`, `ErrValidatorDriftIntervalTooShort`.

Phase 6 is light (one schema check per stack) so it doesn't justify a separate spec.

### Persistence wiring

`pkg/engine/inventory.go` `persistPlan` reads `stack.Drift.Interval`, parses to a Duration, writes seconds value into `inventory.Deployment.DriftIntervalSeconds`. Existing `Deployment` Go struct gains the `DriftIntervalSeconds int` field; both sqlite + postgres `Create` paths persist it.

`DeploymentRepo.ListAll(ctx, orgID)` — new method (existing `Get` and `ListByProject` are per-ID / per-project; the scheduler needs to enumerate all). Returns `[]Deployment`. SQL: `SELECT … FROM deployments WHERE org_id = ?`. Existing implementation pattern from `ListByProject` carries over.

`DriftStatusRepo.LatestByDeployment(ctx, orgID, deploymentID)` — new method. Returns the most recent `drift_status` row per `deployment_target_id` for the given deployment (so the scheduler can decide whether each target is past due AND detect edge transitions). Alternatively `LatestByDeploymentTarget(ctx, orgID, dtID)`. The simpler API is per-deployment, returning a slice.

`DriftStatusRepo.ListByProject(ctx, orgID, projectID)` — new method. Joins `drift_status → deployment_targets → deployments` to filter by project. Used by the new per-project drift UI page.

### Webapi integration

**`cmd/server/main.go`** — boots the scheduler if `NIMBUSFAB_DRIFT_INTERVAL` is set to a non-zero duration:

```go
if interval := envDuration("NIMBUSFAB_DRIFT_INTERVAL", 0); interval > 0 {
    sched := &scheduler.Scheduler{
        Engine: eng, Repo: repo, Notifier: notif,
        GlobalInterval: interval, OrgID: orgID,
        MaxConcurrent: 4, Now: time.Now, Logger: logger,
    }
    go sched.Run(ctx)
}
```

The `notif` value comes from a new `notify.FromEnv()` constructor that reads all the env vars and composes a `MultiNotifier` (or returns a `NopNotifier` if nothing is configured).

**`internal/webapi/ui/templates/drift_project.html`** — new template:
- Page-tabs nav: Overview · Graph · Drift (active).
- Same table shape as `drift.html`, scoped to one project's deployments.
- "Project: {{ .ProjectName }}" subtitle.

**`internal/webapi/ui/templates/drift.html`** — enriched:
- New `Project` column linking to `/ui/projects/{id}`.
- Filter form above the table: `<select name="project" onchange="this.form.submit()">` with `All` + one option per project. Reads from a new repo method `Projects().List(orgID)` (existing).
- When `?project=<id>` is present, handler calls `ListByProject` and renders the filtered set + sets the dropdown's selected option.

**Page-tabs on `project_detail.html`** — current row is `Overview · Graph`; add `· Drift` (linking to `/ui/projects/{id}/drift`).

**Routes registered in `internal/webapi/router.go`:**
```go
mux.Handle("GET /ui/projects/{id}/drift", uiAuth(renderer.ProjectDrift))
```

## Components

### New files

- `pkg/drift/scheduler/scheduler.go` + `_test.go`
- `pkg/drift/notify/notify.go` (interface + `MultiNotifier` + `NopNotifier`)
- `pkg/drift/notify/webhook.go` + `_test.go`
- `pkg/drift/notify/slack.go` + `_test.go`
- `pkg/drift/notify/smtp.go` + `_test.go`
- `pkg/drift/notify/fromenv.go` (constructs `Notifier` from env vars)
- `internal/notify/templates/drift_email.txt` (embed.FS — yes the package layout has both `pkg/drift/notify` and `internal/notify/templates`; the template is internal-only)
- `internal/dsl/validator/phase6_drift.go` + `phase6_drift_test.go`
- `pkg/inventory/migrations/0004_drift_interval.sql` (and the `.sqlite.sql` variant)
- `internal/webapi/ui/templates/drift_project.html`

### Modified files

- `pkg/ir/types.go` — Stack.Drift + DriftConfig
- `pkg/inventory/repo.go` — `Deployment.DriftIntervalSeconds`; new method signatures `Deployments().ListAll`, `DriftStatus().LatestByDeployment`, `DriftStatus().ListByProject`
- `internal/inventory/sqlite/{deployments,drift}.go` — implement new methods; column added in INSERT/SELECT
- `internal/inventory/postgres/{deployments,drift}.go` — same
- `pkg/engine/inventory.go` — write drift_interval_seconds
- `internal/dsl/validator/validator.go` — call phase6Drift
- `cmd/server/main.go` — boot scheduler + notifier
- `internal/webapi/ui/pages.go` — `ProjectDrift` handler + global Drift handler enrichment (project column + filter)
- `internal/webapi/router.go` — route registration
- `internal/webapi/ui/templates/drift.html` — column + filter
- `internal/webapi/ui/templates/project_detail.html` — page-tabs add Drift link

## Data flow

```
                ┌─────────────────────────────┐
                │  nimbusfab-server boots     │
                │  NIMBUSFAB_DRIFT_INTERVAL=1h│
                └──────────────┬──────────────┘
                               ▼
                ┌─────────────────────────────┐
                │  scheduler.Scheduler.Run    │
                │  (ticker, 60s clamp)        │
                └──────────────┬──────────────┘
                               ▼
       every tick:   ┌─────────────────────────────┐
                     │  Deployments().ListAll      │
                     │  filter to applied / drift- │
                     │  detected status            │
                     └──────────────┬──────────────┘
                                    ▼
                     ┌─────────────────────────────┐
                     │  per deployment:            │
                     │   effective = override ?? default
                     │   latest = drift_status     │
                     │   if now - latest >= eff:   │
                     │     queue                   │
                     └──────────────┬──────────────┘
                                    ▼
              ┌───────────────────────────────────────┐
              │ goroutine pool (4 concurrent)         │
              │   engine.DetectDrift(deploymentID)    │
              │     → provisioner.DetectDrift         │
              │     → writes drift_status rows        │
              └──────────────┬────────────────────────┘
                             ▼
              ┌───────────────────────────────────────┐
              │  edge detection:                      │
              │   read prior drift_status (LatestByX) │
              │   if has_drift flipped → fire notify  │
              └──────────────┬────────────────────────┘
                             ▼
              ┌──────────────────────────────────────────────┐
              │  MultiNotifier.Notify(DriftEvent)             │
              │   ├── WebhookNotifier (POST JSON)             │
              │   ├── SlackNotifier   (POST Slack block-kit)  │
              │   └── SMTPNotifier    (PlainAuth + template)  │
              │   per-transport failures logged, never block  │
              └──────────────────────────────────────────────┘
```

## Error handling

| Layer | Condition | Behavior |
|-------|-----------|----------|
| Scheduler | `ListAll` errors | Log; skip this tick; ticker continues. |
| Scheduler | Deployment with no applied state | Skipped — no `last_apply_status` post-apply means nothing to drift against. |
| Scheduler | `engine.DetectDrift` returns error | Log; `drift_status` row written with status `failed`; **no** notification (only `has_drift` transitions fire). |
| Scheduler | Server shutdown during drift detection | Context cancellation propagates; in-flight `DetectDrift` aborts. |
| Notifier | Transport returns error | Log at WARN; aggregate `MultiNotifier` still returns nil. |
| Notifier | All transports unconfigured | `NopNotifier` returned by `FromEnv`; `Notify` is a no-op. |
| Validator | Stack drift.interval not parseable | `ErrValidatorDriftIntervalInvalid`. |
| Validator | Stack drift.interval < 60s | `ErrValidatorDriftIntervalTooShort`. |

## Webhook payload schema

```json
{
  "event": "drift_detected",
  "deploymentId": "dep-abc123",
  "projectId": "proj-xyz",
  "projectName": "demo",
  "stack": "dev",
  "detectedAt": "2026-05-18T15:34:12Z",
  "targets": [
    {
      "component": "orders-db",
      "type": "database",
      "cloud": "aws",
      "region": "us-east-1",
      "summary": "+0 ~2 -0",
      "deploymentTargetId": "tgt-abc"
    }
  ],
  "deploymentUrl": "https://nimbusfab.example.com/ui/deployments/dep-abc123"
}
```

`deploymentUrl` is omitted when `NIMBUSFAB_PUBLIC_URL` is empty.

## Testing strategy

### Unit

- **Scheduler** with injected fake clock + fake `inventory.Repo`:
  - First tick: no deployments → no-op.
  - One past-due deployment → DetectDrift invoked once.
  - Per-deployment override beats global.
  - Concurrency cap: 5 due deployments + cap=2 → pool serializes.
  - Context cancel mid-tick → clean exit.
  - DetectDrift error: log + status="failed" row, no notify.
- **Notifiers** with `httptest.Server` (webhook + Slack) and a mock SMTP via `net/smtp/smtptest`-style harness:
  - Webhook: 200, 500 retry, 4xx abort, network error.
  - Slack: payload contains correct `text` + `color`.
  - SMTP: subject formatted, body rendered from template.
  - Multi: one transport fails, others still called.
- **Edge transition** standalone helper `transitioned(prev []DriftStatus, curr []DriftStatus) []DriftEvent`:
  - clean → drifted = drift_detected event.
  - drifted → clean = drift_resolved event.
  - drifted → drifted = no event.
  - clean → clean = no event.
  - new target (no prev row) + drifted = drift_detected.
- **Validator phase 6**: too-short, invalid, OK.
- **Inventory repo**: ListAll round-trip; ListByProject round-trip; LatestByDeployment correctness with multiple rows per target.

### Integration

- `nimbusfab-server` with `NIMBUSFAB_DRIFT_INTERVAL=2s` and a FakeRunner-seeded deployment. Run for ~6s, assert `drift_status` rows appear and Notify is invoked (with a test webhook receiver in the test process).

## Non-goals (deferred to v1.3+)

- **Per-deployment notification routing.** All drift events go to the same configured destinations. Per-deployment / per-component routing requires a notification-targets table + UI for editing — a separate spec.
- **Daily digest for persistent drift.** A deployment that's drifted for a week generates no further events under Phase 2. Future enhancement.
- **Drift remediation (auto-apply).** Phase 2 detects + notifies only. Auto-remediation is a substantial product decision (when is it safe? who approves?) and gets its own spec.
- **OIDC/SSO for SMTP auth.** `NIMBUSFAB_SMTP_PASS` plain string for v1.2. SMTP OAuth is a deferred enhancement.
- **HTML email.** Plaintext only in v1.2.
- **Webhook signing.** No HMAC signature on outbound POST. Add later if users request.
- **Per-tenant scheduler isolation.** Single-org-per-server (current v1 deployment model) means one scheduler suffices. Multi-org rate-limiting is v2+.

## Implementation phasing

Single phase: **Drift Phase 2.** ~14 implementation tasks.

1. Migration `0004_drift_interval.sql` (both backends) + tests asserting the column exists post-migrate.
2. `inventory.Deployment.DriftIntervalSeconds` field + repo `Create`/`Get`/`ListByProject`/`ListAll` updates (both backends) + tests.
3. `inventory.DriftStatusRepo.LatestByDeployment` + `ListByProject` + tests.
4. `ir.Stack.Drift` + `ir.DriftConfig`; YAML round-trip test.
5. `validator/phase6_drift.go` + tests (too-short, invalid, OK).
6. `engine.persistPlan` writes `drift_interval_seconds`; roundtrip test.
7. `pkg/drift/notify` package: `Notifier` interface + `MultiNotifier` + `NopNotifier` + `DriftEvent` types + tests.
8. `pkg/drift/notify/webhook.go` + retry/backoff + httptest-server tests.
9. `pkg/drift/notify/slack.go` (wraps webhook with Slack body shape) + tests.
10. `pkg/drift/notify/smtp.go` + email template + tests with a mock SMTP server.
11. `pkg/drift/notify/fromenv.go` constructor + tests.
12. `pkg/drift/scheduler.Scheduler` + fake-clock + fake-repo tests covering 5 cases (no deployments, one past-due, override beats default, concurrency cap, ctx cancel).
13. `cmd/server/main.go` wires scheduler + notifier; new env vars surfaced. Integration test with FakeRunner-seeded deployment + 2s interval.
14. UI changes: new `drift_project.html` template + `ProjectDrift` handler + project-filter on `/ui/drift` + page-tabs Drift entry on project-detail. Handler tests assert filtering + rendering.

## Open questions

None blocking. Possible v1.3 surfaces noted:
- Should `drift_resolved` events count "all targets clean" as one event, or one per resolved target? Current spec: one event with the resolved targets in `targets[]`. Symmetric with `drift_detected`. Worth re-checking once we see real telemetry.
- Should we record a per-deployment `drift_paused: bool` field for operators to mute drift on known-broken deployments? Easy add later.
