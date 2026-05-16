# Web App HTTP Phase 2 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Browser-triggered deployments with live log streaming. After Phase 2, `POST /api/v1/deployments/{id}/applies` kicks off `engine.Apply` in a goroutine and returns 202 immediately; the caller opens `GET /api/v1/deployments/{id}/events` (SSE) and receives `RunEvent`s live. Same for destroys and drifts. UI Phase 2 will layer Deploy/Destroy/Drift buttons + EventSource JS on top.

**Architecture:**
- New `internal/webapi/runner` package with `RunBroker` — in-process pub/sub keyed by deployment ID. POST handlers create a subscriber-fan-out channel and pass it as `EventSink` into `engine.Apply/Destroy/DetectDrift` (run in a goroutine); SSE handler subscribes to the broker and streams events to one connection.
- Engine API gains `EventSink` fields on `ApplyOpts` / `DestroyOpts` / new `DriftOpts`. Plumbed through to existing provisioner `ApplyInput.EventSink` / `DestroyInput.EventSink` / `DriftInput.EventSink`.
- Router gains `engine.Engine` in `Config`; mounts 3 POST routes and 1 SSE GET route under the existing `/api/v1/*` prefix with the same bearer-auth middleware.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-webapp-http-phase2/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- Plan endpoint (CLI plans; web app applies/destroys/drifts existing deployments).
- Idempotency-Key middleware (separate phase or alongside Auth Phase 1).
- `?wait=true` sync mode.
- Reconnect / `Last-Event-ID` replay from inventory (would need RunLogs repo, currently stubbed).
- UI Phase 2 (browser buttons + JS) — separate phase.
- Persistent event log; events are live-only for Phase 2.

---

## Task 1: Engine plumbs EventSink through Apply/Destroy/Drift

**Files:**
- Edit: `pkg/engine/engine.go` (ApplyOpts / DestroyOpts gain EventSink; new DriftOpts)
- Edit: `pkg/engine/plan.go` (ApplyWithPlan / DestroyWithPlan / DetectDriftWithPlan threads EventSink)

- [ ] **Step 1: Extend opts**

```go
// ApplyOpts gains:
EventSink chan<- provisioner.RunEvent

// DestroyOpts gains:
EventSink chan<- provisioner.RunEvent

// new DriftOpts type:
type DriftOpts struct {
    EventSink chan<- provisioner.RunEvent
}
```

The provisioner.RunEvent reference means engine.go imports provisioner — already does. Aliases stay for backward compat.

- [ ] **Step 2: Plumb through**

In `ApplyWithPlan` / `DestroyWithPlan` / `DetectDriftWithPlan`, set the corresponding `EventSink` field on `provisioner.ApplyInput` / `DestroyInput` / `DriftInput`.

In `Apply(planID, opts)` / `Destroy(deploymentID, opts)` / `DetectDrift(deploymentID)` (the by-ID variants), forward the opts' EventSink to ApplyWithPlan/DestroyWithPlan/DetectDriftWithPlan. `DetectDrift` currently takes no opts; add `opts DriftOpts` argument and update the interface.

- [ ] **Step 3: Tests** — extend `engine/apply_test.go` to assert that an EventSink passed in receives at least one event (use a buffered channel; assert non-empty after Apply returns). Same for Destroy.

- [ ] **Step 4: Build + test + commit** `engine: ApplyOpts/DestroyOpts/DriftOpts gain EventSink; plumb to provisioner`

---

## Task 2: RunBroker pub/sub

**Files:**
- Create: `internal/webapi/runner/broker.go`
- Create: `internal/webapi/runner/broker_test.go`

- [ ] **Step 1: Implement**

```go
package runner

import (
    "sync"

    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// RunBroker fans out RunEvents from one publisher (engine goroutine) to N
// subscribers (SSE clients) keyed by deployment ID. In-memory only; events
// are NOT persisted to inventory in Phase 2 (RunLogs repo is stubbed).
//
// One broker instance per nimbusfab-server process; subscribers come and
// go as SSE clients connect/disconnect.
type RunBroker struct {
    mu       sync.Mutex
    subs     map[string][]chan provisioner.RunEvent // deploymentID → list of subscriber chans
    bufSize  int
}

// NewRunBroker returns a broker; bufSize is the per-subscriber buffer used
// when dispatching events (small enough to apply backpressure on slow
// clients without dropping silently).
func NewRunBroker(bufSize int) *RunBroker {
    if bufSize <= 0 {
        bufSize = 64
    }
    return &RunBroker{subs: map[string][]chan provisioner.RunEvent{}, bufSize: bufSize}
}

// Subscribe returns a channel that receives events for deploymentID until
// the returned unsubscribe func is called. Subscribers see events posted
// AFTER Subscribe returns (no replay).
func (b *RunBroker) Subscribe(deploymentID string) (<-chan provisioner.RunEvent, func()) {
    ch := make(chan provisioner.RunEvent, b.bufSize)
    b.mu.Lock()
    b.subs[deploymentID] = append(b.subs[deploymentID], ch)
    b.mu.Unlock()
    return ch, func() { b.unsubscribe(deploymentID, ch) }
}

// Publisher returns a channel the engine writes RunEvents into for
// deploymentID. When the channel closes, all current subscribers are
// closed (signals "operation done").
func (b *RunBroker) Publisher(deploymentID string) chan<- provisioner.RunEvent {
    pubCh := make(chan provisioner.RunEvent, b.bufSize)
    go func() {
        for evt := range pubCh {
            b.dispatch(deploymentID, evt)
        }
        b.closeSubs(deploymentID)
    }()
    return pubCh
}

func (b *RunBroker) dispatch(deploymentID string, evt provisioner.RunEvent) {
    b.mu.Lock()
    targets := append([]chan provisioner.RunEvent(nil), b.subs[deploymentID]...)
    b.mu.Unlock()
    for _, ch := range targets {
        select {
        case ch <- evt:
        default:
            // slow subscriber: drop. Real impl might detach the sub;
            // Phase 2 keeps it simple.
        }
    }
}

func (b *RunBroker) closeSubs(deploymentID string) {
    b.mu.Lock()
    targets := b.subs[deploymentID]
    delete(b.subs, deploymentID)
    b.mu.Unlock()
    for _, ch := range targets {
        close(ch)
    }
}

func (b *RunBroker) unsubscribe(deploymentID string, ch chan provisioner.RunEvent) {
    b.mu.Lock()
    defer b.mu.Unlock()
    list := b.subs[deploymentID]
    for i, c := range list {
        if c == ch {
            b.subs[deploymentID] = append(list[:i], list[i+1:]...)
            // Do not close ch — publisher's closeSubs will close it on
            // dispatch shutdown; closing twice would panic.
            return
        }
    }
}
```

- [ ] **Step 2: Tests**:
  - `TestBroker_SubscribeReceives`: publish 3 events; subscriber gets all 3 in order.
  - `TestBroker_MultipleSubscribers`: 2 subscribers see same events.
  - `TestBroker_DifferentDeploymentsIsolated`: events for deployment A do not reach subscribers on B.
  - `TestBroker_PublisherCloseSignalsSubscribers`: closing the publisher channel closes subscriber channels.
  - `TestBroker_LateSubscriberMissesEarlyEvents`: events published before Subscribe are not received (no replay).
  - `TestBroker_Unsubscribe`: after unsubscribe, no further events arrive (use a publisher that emits after a small delay).

- [ ] **Step 3: Build + test + commit** `webapi/runner: RunBroker pub/sub for deployment events`

---

## Task 3: POST handlers — applies / destroys / drifts

**Files:**
- Create: `internal/webapi/api/mutations.go`
- Create: `internal/webapi/api/mutations_test.go`

- [ ] **Step 1: Handlers**

```go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/klehmer/nimbusfab/internal/webapi/runner"
    "github.com/klehmer/nimbusfab/pkg/engine"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// Mutations groups POST handlers + their dependencies.
type Mutations struct {
    Engine engine.Engine
    Broker *runner.RunBroker
    OrgID  string
}

type applyBody struct {
    PartialFailure string `json:"partialFailure,omitempty"` // "leave"|"rollback"|"retry-failed"
    AutoApprove    bool   `json:"autoApprove,omitempty"`
}

// PostApply → POST /api/v1/deployments/{id}/applies
func (m *Mutations) PostApply(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var body applyBody
    _ = json.NewDecoder(r.Body).Decode(&body) // empty body is OK
    pub := m.Broker.Publisher(id)
    go func() {
        defer close(pub)
        _, _ = m.Engine.Apply(r.Context(), id, engine.ApplyOpts{
            AutoApprove:    body.AutoApprove,
            PartialFailure: provisioner.PartialFailurePolicy(body.PartialFailure),
            EventSink:      pub,
        })
        // Errors are reflected via the inventory's deployment status; web
        // app re-fetches with GET /api/v1/deployments/{id} after the run.
    }()
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(http.StatusAccepted)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "data": map[string]any{"deploymentId": id, "status": "running"},
    })
}

// PostDestroy → POST /api/v1/deployments/{id}/destroys
// PostDrift → POST /api/v1/deployments/{id}/drifts
// (same shape; different engine call)
```

- [ ] **Step 2: Tests** with `httptest.NewRecorder` against a stub engine that records the EventSink it received:
  - `TestPostApply_Returns202`: returns 202 + JSON envelope with deploymentId.
  - `TestPostApply_StartsGoroutine`: stub engine receives the call (use sync.WaitGroup in stub).
  - `TestPostApply_EventSinkClosed`: after stub engine returns, the publisher channel is closed (subscribers detected via broker).
  - Equivalent tests for PostDestroy / PostDrift.

- [ ] **Step 3: Build + test + commit** `webapi: POST handlers for applies/destroys/drifts (202 async)`

---

## Task 4: SSE handler

**Files:**
- Create: `internal/webapi/api/sse.go`
- Create: `internal/webapi/api/sse_test.go`

- [ ] **Step 1: Handler**

```go
package api

import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/klehmer/nimbusfab/internal/webapi/runner"
)

// SSEEvents → GET /api/v1/deployments/{id}/events
//
// SSE stream of RunEvents for the deployment. Subscribers see events
// published AFTER they connect; no replay. Connection closes when the
// publisher channel closes (operation done) OR the client disconnects.
type SSEEvents struct {
    Broker *runner.RunBroker
}

func (s *SSEEvents) Handle(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    f, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")

    ch, unsub := s.Broker.Subscribe(id)
    defer unsub()

    heartbeat := time.NewTicker(15 * time.Second)
    defer heartbeat.Stop()
    var eventID uint64

    for {
        select {
        case <-r.Context().Done():
            return
        case <-heartbeat.C:
            _, _ = fmt.Fprintf(w, ": ping\n\n")
            f.Flush()
        case evt, ok := <-ch:
            if !ok {
                _, _ = fmt.Fprintf(w, "event: complete\ndata: {}\n\n")
                f.Flush()
                return
            }
            eventID++
            payload, _ := json.Marshal(map[string]any{
                "timestamp":          evt.Timestamp.Format(time.RFC3339),
                "deploymentTargetId": evt.DeploymentTargetID,
                "component":          evt.Component,
                "cloud":              evt.Cloud,
                "region":             evt.Region,
                "kind":               string(evt.Kind),
                "message":            evt.Message,
            })
            _, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", eventID, evt.Kind, payload)
            f.Flush()
        }
    }
}
```

- [ ] **Step 2: Tests** — use `httptest.NewServer` so SSE Flusher works (NewRecorder doesn't implement Flusher with chunked encoding the same way):
  - `TestSSE_StreamsEvents`: open SSE, publish 3 events via broker.Publisher, assert client receives 3 SSE events in order with the right `kind` and `data`.
  - `TestSSE_CompleteOnPublisherClose`: close publisher → client sees `event: complete` and connection ends.
  - `TestSSE_HeartbeatOnIdle`: with a low heartbeat interval (inject via field), idle subscriber sees `: ping` comment lines. (Use a separate test-only constructor that lets us set the heartbeat to 50ms.)
  - `TestSSE_ClientDisconnect`: cancel client context → handler returns cleanly (no goroutine leak; verify via broker unsub).

- [ ] **Step 3: Build + test + commit** `webapi: SSE handler streams RunEvents per deployment`

---

## Task 5: Router mount + engine wiring

**Files:**
- Edit: `internal/webapi/router.go`
- Edit: `internal/webapi/router_test.go`
- Edit: `cmd/server/main.go`

- [ ] **Step 1: Config gains Engine + Broker**

```go
type Config struct {
    Repo     inventory.Repo
    OrgID    string
    APIToken string
    Engine   engine.Engine   // optional; nil = mutating endpoints return 503
}
```

In `New`, if `Engine != nil`, construct a RunBroker and mount mutating + SSE routes:

```go
if cfg.Engine != nil {
    broker := runner.NewRunBroker(64)
    mutations := &api.Mutations{Engine: cfg.Engine, Broker: broker, OrgID: cfg.OrgID}
    sse := &api.SSEEvents{Broker: broker}
    mux.Handle("POST /api/v1/deployments/{id}/applies", apiAuth(http.HandlerFunc(mutations.PostApply)))
    mux.Handle("POST /api/v1/deployments/{id}/destroys", apiAuth(http.HandlerFunc(mutations.PostDestroy)))
    mux.Handle("POST /api/v1/deployments/{id}/drifts", apiAuth(http.HandlerFunc(mutations.PostDrift)))
    mux.Handle("GET /api/v1/deployments/{id}/events", apiAuth(http.HandlerFunc(sse.Handle)))
}
```

If `Engine == nil`, leave those routes unmounted; the existing read-only API still works.

- [ ] **Step 2: cmd/server constructs engine**

```go
// In cmd/server/main.go run():
eng, err := engine.New(ctx, engine.Config{
    CloudAdapters: defaultCloudRegistry(),
    InventoryRepo: repo,
    SecretsBackend: secrets.DefaultBackend(),
    TofuRunner: tofu.NewExecRunner(),
    WorkRoot: envDefault("NIMBUSFAB_WORK_ROOT", filepath.Join(os.TempDir(), "nimbusfab-server")),
})
// then pass to webapi.New
```

Add `defaultCloudRegistry()` helper to cmd/server (mirrors the CLI's). Imports: pkg/components, internal/cloud/{aws,azure,gcp}.

- [ ] **Step 3: Router tests** — POST /api/v1/deployments/anything/applies returns 202 even with a stub engine; nil-Engine config does not mount mutating routes.

- [ ] **Step 4: Smoke test the running binary** — start server with a seeded SQLite DB; POST /api/v1/deployments/{seeded-id}/applies; verify 202 returned; open SSE briefly.

- [ ] **Step 5: Build + test + commit** `webapi: mount POST + SSE routes; cmd/server constructs engine`

---

## Task 6: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: README** brief note: browser-triggered deployments + live log streaming now possible via `POST /api/v1/deployments/{id}/applies` + `GET /api/v1/deployments/{id}/events`.

- [ ] **Step 2: CHANGELOG** entry under "Unreleased — Web App HTTP Phase 2":
  - 3 POST endpoints (applies/destroys/drifts) returning 202 + deployment ID
  - SSE endpoint with heartbeat / no-replay semantics
  - RunBroker in-process pub/sub
  - Engine API EventSink additions
  - Out-of-scope deferrals

- [ ] **Step 3: Final test + gofmt**

- [ ] **Step 4: Commit** `docs: HTTP Phase 2 merged — mutations + SSE`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/webapp-http-phase2 -m "Merge feat/webapp-http-phase2: mutating endpoints + SSE"
git push origin main
git push origin feat/webapp-http-phase2
```
