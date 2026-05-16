# Provisioner Phase 2 — Apply, Destroy, Orchestration, State Bridge, Drift, Refs

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land working `nimbusfab apply`, `nimbusfab destroy`, and `nimbusfab drift` commands that operate on multi-target deployments with chosen partial-failure policy and cross-component output refs. Phase 1's single-target `nimbusfab plan` becomes a parallel multi-target plan. State produced by `tofu apply` is parsed, captured as outputs, and made available to dependent components via `data.terraform_remote_state` workspace blocks. Inventory persistence is still out of scope (Phase 2 stays in-memory / per-process; the inventory phase comes later).

**Architecture:** Three new internal layers under `pkg/provisioner`. (1) **Orchestrator** (`orchestrator.go`) builds a component DAG from `ir.Project.Components[].Refs`, fans targets out across a global / per-cloud / per-credential semaphore via `golang.org/x/sync/errgroup`, and applies the chosen `PartialFailurePolicy` when targets fail. (2) **Refs resolver** (`refs.go`) walks `ir.LazyRef` markers in target specs and assembles the `cloud.ResolvedRefs` map the adapter sees, plus injects `data.terraform_remote_state` blocks into downstream workspaces so the runtime values resolve at Tofu time. (3) **State bridge** (`internal/state/bridge`) parses `tofu show -json` post-apply state into a `StateSnapshot` and emits per-resource records the eventual inventory persistence layer (or in-memory caller, for Phase 2) consumes. Drift detection uses `tofu plan -refresh-only` and produces a `DriftReport` consumable by the new `nimbusfab drift` CLI surface.

**Tech Stack:**
- Go 1.22 (existing)
- `github.com/google/uuid` (existing)
- `golang.org/x/sync@v0.7.0` (NEW; for `errgroup` — was removed by tidy at end of Phase 1, re-added here when used)
- All other deps already in `go.mod`

**Conventions used throughout this plan:**
- All file paths are relative to the repo root `/home/kurt/git/nimbusfab/`.
- Run all `go` commands from the repo root, with `PATH=$HOME/.local/go/bin:$PATH`.
- Each Task ends with a commit; commit messages follow `<area>: <imperative>` style.
- Tests live alongside source.
- Use `go test ./...` from the repo root after each task.
- The `tofu` binary is NOT required to run unit tests — FakeRunner handles everything.

**Out of scope for Phase 2 (deferred):**
- Inventory persistence: `deployments`, `deployment_targets`, `runs`, `run_logs`, `tofu_resources`, `drift_status` tables are still DDL-only stubs. Phase 2 keeps run state per-process (returned in `ApplyResult`/`DriftReport`); the inventory phase wires DB writes against these results.
- Web SSE streaming. Phase 2 produces `RunEvent`s but doesn't ship a server consumer; the web app phase plugs in.
- Workspace caching across runs (`.terraform/` reuse, workspace_hash) — Phase 2 always re-`tofu init`s. The optimization lands when measurement shows it matters.
- AWS adapter expansion (subnets, route tables, database, compute, storage) — Phase 3.
- Azure / GCP adapters — Phases 4–5.
- `nimbusfab state {show,rm,mv,unlock}` CLI commands — separate phase. The Runner interface already exposes these.
- gRPC plugin transport — v2 spec.

---

## Task 1: Re-add errgroup dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dep**

```bash
PATH=$HOME/.local/go/bin:$PATH go get golang.org/x/sync@v0.7.0
```

(It's listed as `// indirect` after `go get` because no `.go` file imports it yet — Task 4 changes that.)

- [ ] **Step 2: Verify build**

```bash
PATH=$HOME/.local/go/bin:$PATH go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: re-add x/sync/errgroup for Phase 2 orchestrator"
```

---

## Task 2: Extend provisioner public types for Apply/Destroy/Drift

**Files:**
- Modify: `pkg/provisioner/types.go`
- Create: `pkg/provisioner/types_apply_test.go`

Phase 1 left `ApplyInput`, `ApplyResult`, `TargetApplyResult`, `DestroyInput` as reserved-shape stubs in `pkg/provisioner/provisioner.go`. Promote them to `types.go` alongside `PlanInput`/`PlanResult` and add the Phase-2 fields that turned out to be needed: `Outputs` capture, `StateSnapshot` slot, `RunEvent` shape, drift report types.

- [ ] **Step 1: Move + extend types**

In `pkg/provisioner/types.go`, ADD (after the existing `Diagnostic` type):

```go
// RunStatus discriminates per-target lifecycle outcomes.
type RunStatus string

const (
    RunStatusQueued    RunStatus = "queued"
    RunStatusRunning   RunStatus = "running"
    RunStatusSucceeded RunStatus = "succeeded"
    RunStatusFailed    RunStatus = "failed"
    RunStatusSkipped   RunStatus = "skipped"
    RunStatusReverted  RunStatus = "reverted" // rollback policy destroyed it
)

// ApplyStatus discriminates the overall outcome of an Apply call.
type ApplyStatus string

const (
    ApplySucceeded       ApplyStatus = "succeeded"
    ApplyPartialFailure  ApplyStatus = "partial_failure"
    ApplyFailed          ApplyStatus = "failed"
    ApplyRollbackFailed  ApplyStatus = "rollback_failed"
)

// ApplyInput is what the engine hands the provisioner for Apply.
type ApplyInput struct {
    PlanResult            *PlanResult
    OrgID                 string
    PartialFailure        PartialFailurePolicy
    AutoApprove           bool
    AllowParityViolations bool
    MaxRetries            int // for retry-failed policy; default 1
    MaxConcurrentTargets  int // global parallelism cap; <=0 means runtime.NumCPU()
    MaxConcurrentPerCloud int // per-cloud cap; <=0 means 8
    EventSink             chan<- RunEvent // optional; buffered or nil
}

// ApplyResult mirrors the spec shape.
type ApplyResult struct {
    DeploymentID  string
    Status        ApplyStatus
    TargetResults []TargetApplyResult
    GeneratedAt   time.Time
}

// TargetApplyResult is one target's outcome.
type TargetApplyResult struct {
    DeploymentTargetID string
    Component          string
    Cloud              string
    Region             string
    RunID              string
    Status             RunStatus
    Outputs            map[string]any
    State              *StateSnapshot
    Error              error
    StartedAt          time.Time
    FinishedAt         time.Time
}

// DestroyInput targets an existing deployment for teardown.
type DestroyInput struct {
    PlanResult            *PlanResult           // optional; if provided, scopes to its targets
    DeploymentID          string                // required when PlanResult is nil
    Stack                 string                // required when PlanResult is nil
    Project               *ir.Project           // required when PlanResult is nil
    OrgID                 string
    PartialFailure        PartialFailurePolicy
    AutoApprove           bool
    MaxConcurrentTargets  int
    MaxConcurrentPerCloud int
    EventSink             chan<- RunEvent
}

// RunEvent is one streamed update produced by per-target work. Apply/Destroy
// forward these to the optional EventSink channel.
type RunEvent struct {
    Timestamp          time.Time
    DeploymentTargetID string
    Component          string
    Cloud              string
    Region             string
    Kind               RunEventKind
    Message            string
    // Raw carries adapter / runner detail; opaque to upstream consumers.
    Raw map[string]any
}

// RunEventKind classifies a RunEvent.
type RunEventKind string

const (
    RunEventStart      RunEventKind = "start"
    RunEventLog        RunEventKind = "log"
    RunEventDiagnostic RunEventKind = "diagnostic"
    RunEventProgress   RunEventKind = "progress"
    RunEventSuccess    RunEventKind = "success"
    RunEventFailure    RunEventKind = "failure"
    RunEventSkip       RunEventKind = "skip"
    RunEventTerminal   RunEventKind = "terminal"
)

// StateSnapshot captures a parsed `tofu show -json` state.
type StateSnapshot struct {
    DeploymentTargetID string
    TofuVersion        string
    SerialNumber       int64
    Resources          []StateResource
    Outputs            map[string]any
    CapturedAt         time.Time
}

// StateResource is one entry from a parsed Tofu state.
type StateResource struct {
    Address         string // "aws_vpc.web"
    Type            string // "aws_vpc"
    Name            string // "web"
    CloudResourceID string // ARN / self-link / Azure ID
    AttributesHash  string // sha256 of canonicalized attributes
    Attributes      map[string]any
}

// DriftInput is what DetectDrift takes.
type DriftInput struct {
    PlanResult            *PlanResult
    DeploymentID          string
    Stack                 string
    Project               *ir.Project
    OrgID                 string
    MaxConcurrentTargets  int
    MaxConcurrentPerCloud int
    EventSink             chan<- RunEvent
}

// DriftReport is what DetectDrift returns.
type DriftReport struct {
    DeploymentID  string
    TargetReports []TargetDriftReport
    GeneratedAt   time.Time
}

// TargetDriftReport is per-target drift.
type TargetDriftReport struct {
    DeploymentTargetID string
    Component          string
    Cloud              string
    Region             string
    HasDrift           bool
    Drifted            []DriftedResource
    Gone               []DriftedResource
    Discovered         []DriftedResource
    Error              error
}

// DriftedResource describes one drifted Tofu address.
type DriftedResource struct {
    Address          string
    Kind             string // "drift" | "gone" | "discovered"
    AttributesBefore map[string]any
    AttributesAfter  map[string]any
    DiffSummary      string
}
```

- [ ] **Step 2: Remove now-duplicate stubs from `provisioner.go`**

Delete the existing `ApplyInput`, `ApplyResult`, `TargetApplyResult`, `DestroyInput` blocks from `pkg/provisioner/provisioner.go` (the ones added in Phase 1 as Phase-2 reserved shapes — the real ones now live in `types.go`).

- [ ] **Step 3: Verify build**

```bash
PATH=$HOME/.local/go/bin:$PATH go build ./...
```

Expected: the Apply/Destroy stub methods in `provisioner.go` still return `ErrNotImplementedYet` and reference the moved types — clean build.

- [ ] **Step 4: Test the new types (zero-value sanity)**

Create `pkg/provisioner/types_apply_test.go`:

```go
package provisioner

import "testing"

func TestRunStatus_Constants(t *testing.T) {
    if string(RunStatusSucceeded) != "succeeded" {
        t.Errorf("RunStatusSucceeded = %q", RunStatusSucceeded)
    }
    if string(RunStatusReverted) != "reverted" {
        t.Errorf("RunStatusReverted = %q", RunStatusReverted)
    }
}

func TestApplyStatus_Constants(t *testing.T) {
    if string(ApplyPartialFailure) != "partial_failure" {
        t.Errorf("ApplyPartialFailure = %q", ApplyPartialFailure)
    }
    if string(ApplyRollbackFailed) != "rollback_failed" {
        t.Errorf("ApplyRollbackFailed = %q", ApplyRollbackFailed)
    }
}

func TestStateSnapshot_ZeroValue(t *testing.T) {
    var s StateSnapshot
    if s.Resources != nil {
        t.Error("zero StateSnapshot should have nil Resources")
    }
}
```

- [ ] **Step 5: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run "TestRunStatus|TestApplyStatus|TestStateSnapshot"
git add pkg/provisioner/types.go pkg/provisioner/types_apply_test.go pkg/provisioner/provisioner.go
git commit -m "provisioner: promote Apply/Destroy/Drift types to types.go"
```

---

## Task 3: State bridge — parse `tofu show -json` into StateSnapshot

**Files:**
- Create: `internal/state/bridge/bridge.go`
- Create: `internal/state/bridge/bridge_test.go`
- Create: `internal/state/bridge/testdata/state_one_vpc.json`
- Create: `internal/state/bridge/testdata/state_empty.json`

The bridge is what turns `tofu show -json`'s output into a typed `StateSnapshot`. It does NOT write to inventory in Phase 2 — that's the inventory phase. Apply/Destroy/Drift call the bridge and embed the snapshot in their result so callers can inspect.

- [ ] **Step 1: Write failing tests**

Create `internal/state/bridge/testdata/state_one_vpc.json`:

```json
{
  "format_version": "1.0",
  "terraform_version": "1.7.0",
  "serial": 4,
  "values": {
    "outputs": {
      "vpc_id": { "value": "vpc-0abc123", "type": "string" },
      "subnet_ids": { "value": ["subnet-1", "subnet-2"], "type": ["list", "string"] }
    },
    "root_module": {
      "resources": [
        {
          "address": "aws_vpc.web",
          "type": "aws_vpc",
          "name": "web",
          "values": {
            "id": "vpc-0abc123",
            "arn": "arn:aws:ec2:us-east-1:111:vpc/vpc-0abc123",
            "cidr_block": "10.0.0.0/16"
          }
        }
      ]
    }
  }
}
```

Create `internal/state/bridge/testdata/state_empty.json`:

```json
{
  "format_version": "1.0",
  "terraform_version": "1.7.0",
  "values": {"outputs": {}, "root_module": {"resources": []}}
}
```

Create `internal/state/bridge/bridge_test.go`:

```go
package bridge_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/klehmer/nimbusfab/internal/state/bridge"
)

func TestParse_OneVPC(t *testing.T) {
    raw, err := os.ReadFile(filepath.Join("testdata", "state_one_vpc.json"))
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    snap, err := bridge.Parse(raw)
    if err != nil {
        t.Fatalf("Parse: %v", err)
    }
    if snap.TofuVersion != "1.7.0" {
        t.Errorf("TofuVersion = %q", snap.TofuVersion)
    }
    if snap.SerialNumber != 4 {
        t.Errorf("SerialNumber = %d", snap.SerialNumber)
    }
    if len(snap.Resources) != 1 {
        t.Fatalf("Resources len = %d", len(snap.Resources))
    }
    r := snap.Resources[0]
    if r.Address != "aws_vpc.web" || r.Type != "aws_vpc" || r.Name != "web" {
        t.Errorf("resource shape: %+v", r)
    }
    if r.CloudResourceID == "" {
        t.Error("CloudResourceID empty; expected arn or id fallback")
    }
    if r.AttributesHash == "" {
        t.Error("AttributesHash empty")
    }
    if snap.Outputs["vpc_id"] != "vpc-0abc123" {
        t.Errorf("Outputs[vpc_id] = %v", snap.Outputs["vpc_id"])
    }
}

func TestParse_EmptyState(t *testing.T) {
    raw, err := os.ReadFile(filepath.Join("testdata", "state_empty.json"))
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    snap, err := bridge.Parse(raw)
    if err != nil {
        t.Fatalf("Parse: %v", err)
    }
    if len(snap.Resources) != 0 || len(snap.Outputs) != 0 {
        t.Errorf("expected empty snapshot, got %+v", snap)
    }
}

func TestParse_DeterministicAttributesHash(t *testing.T) {
    raw, _ := os.ReadFile(filepath.Join("testdata", "state_one_vpc.json"))
    a, _ := bridge.Parse(raw)
    b, _ := bridge.Parse(raw)
    if a.Resources[0].AttributesHash != b.Resources[0].AttributesHash {
        t.Errorf("attribute hash not deterministic: %q vs %q",
            a.Resources[0].AttributesHash, b.Resources[0].AttributesHash)
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/state/bridge/ -v
```

Expected: FAIL — package missing.

- [ ] **Step 3: Implement `bridge.go`**

Create `internal/state/bridge/bridge.go`:

```go
// Package bridge parses `tofu show -json` output into the provisioner's
// StateSnapshot type. It does NOT touch the inventory database — that lives
// in the inventory persistence phase. Bridge is pure: given JSON bytes,
// return a typed snapshot.
package bridge

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "sort"
    "time"

    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// Parse turns `tofu show -json` raw bytes into a StateSnapshot.
// DeploymentTargetID is left empty — the caller fills it from context.
func Parse(raw []byte) (*provisioner.StateSnapshot, error) {
    var doc struct {
        FormatVersion    string `json:"format_version"`
        TerraformVersion string `json:"terraform_version"`
        Serial           int64  `json:"serial"`
        Values           struct {
            Outputs    map[string]struct {
                Value any `json:"value"`
                Type  any `json:"type"`
            } `json:"outputs"`
            RootModule struct {
                Resources []struct {
                    Address string         `json:"address"`
                    Type    string         `json:"type"`
                    Name    string         `json:"name"`
                    Values  map[string]any `json:"values"`
                } `json:"resources"`
            } `json:"root_module"`
        } `json:"values"`
    }
    if err := json.Unmarshal(raw, &doc); err != nil {
        return nil, fmt.Errorf("bridge.Parse: %w", err)
    }
    snap := &provisioner.StateSnapshot{
        TofuVersion:  doc.TerraformVersion,
        SerialNumber: doc.Serial,
        Outputs:      map[string]any{},
        CapturedAt:   time.Now().UTC(),
    }
    for k, v := range doc.Values.Outputs {
        snap.Outputs[k] = v.Value
    }
    for _, r := range doc.Values.RootModule.Resources {
        snap.Resources = append(snap.Resources, provisioner.StateResource{
            Address:         r.Address,
            Type:            r.Type,
            Name:            r.Name,
            CloudResourceID: cloudResourceID(r.Values),
            AttributesHash:  hashAttributes(r.Values),
            Attributes:      r.Values,
        })
    }
    sort.Slice(snap.Resources, func(i, j int) bool {
        return snap.Resources[i].Address < snap.Resources[j].Address
    })
    return snap, nil
}

// cloudResourceID picks the cloud-native primary identifier from the resource
// attributes map. Preference: arn > id > self_link > name fallback.
func cloudResourceID(attrs map[string]any) string {
    for _, key := range []string{"arn", "id", "self_link"} {
        if v, ok := attrs[key].(string); ok && v != "" {
            return v
        }
    }
    return ""
}

// hashAttributes returns the sha256 of the canonical-JSON form of attrs.
// Used to detect attribute changes across `tofu apply` runs.
func hashAttributes(attrs map[string]any) string {
    b := canonicalJSON(attrs)
    sum := sha256.Sum256(b)
    return hex.EncodeToString(sum[:])
}

// canonicalJSON is a local copy of provisioner's canonical-JSON serializer so
// bridge doesn't import provisioner-internal helpers. Keep the two algorithms
// in sync.
func canonicalJSON(v any) []byte {
    return canonical(v)
}

func canonical(v any) []byte {
    switch t := v.(type) {
    case map[string]any:
        keys := make([]string, 0, len(t))
        for k := range t {
            keys = append(keys, k)
        }
        sort.Strings(keys)
        buf := []byte{'{'}
        for i, k := range keys {
            if i > 0 {
                buf = append(buf, ',')
            }
            kb, _ := json.Marshal(k)
            buf = append(buf, kb...)
            buf = append(buf, ':')
            buf = append(buf, canonical(t[k])...)
        }
        buf = append(buf, '}')
        return buf
    case []any:
        buf := []byte{'['}
        for i, x := range t {
            if i > 0 {
                buf = append(buf, ',')
            }
            buf = append(buf, canonical(x)...)
        }
        buf = append(buf, ']')
        return buf
    default:
        b, _ := json.Marshal(t)
        return b
    }
}
```

- [ ] **Step 4: Run, verify pass + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/state/bridge/ -v
git add internal/state/bridge/
git commit -m "bridge: parse tofu show -json into StateSnapshot with deterministic attribute hash"
```

---

## Task 4: Orchestrator — component DAG + parallel target fan-out

**Files:**
- Create: `pkg/provisioner/orchestrator.go`
- Create: `pkg/provisioner/orchestrator_test.go`

The orchestrator topologically sorts components by `Refs[].Component`, then per component fans targets out in parallel (or serial if `comp.Policy.Serial`). It enforces three semaphores in series — global → cloud → credential — to bound concurrency. Per-target work is supplied by the caller as a function; the orchestrator owns scheduling, error aggregation, partial-failure policy hand-off, and event emission.

- [ ] **Step 1: Write failing test for topo sort**

Create `pkg/provisioner/orchestrator_test.go`:

```go
package provisioner

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestTopoSort_LinearChain(t *testing.T) {
    comps := []ir.Component{
        {Name: "c", Refs: []ir.ComponentRef{{Component: "b"}}},
        {Name: "a"},
        {Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
    }
    order, err := topoSort(comps)
    if err != nil {
        t.Fatalf("topoSort: %v", err)
    }
    if len(order) != 3 || order[0].Name != "a" || order[1].Name != "b" || order[2].Name != "c" {
        t.Errorf("order = %v", names(order))
    }
}

func TestTopoSort_Diamond(t *testing.T) {
    // a -> {b, d}, both -> c
    comps := []ir.Component{
        {Name: "a"},
        {Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
        {Name: "d", Refs: []ir.ComponentRef{{Component: "a"}}},
        {Name: "c", Refs: []ir.ComponentRef{{Component: "b"}, {Component: "d"}}},
    }
    order, err := topoSort(comps)
    if err != nil {
        t.Fatalf("topoSort: %v", err)
    }
    pos := positions(order)
    if pos["a"] >= pos["b"] || pos["a"] >= pos["d"] {
        t.Errorf("a must come before b and d: %v", names(order))
    }
    if pos["b"] >= pos["c"] || pos["d"] >= pos["c"] {
        t.Errorf("b and d must come before c: %v", names(order))
    }
}

func TestTopoSort_Cycle(t *testing.T) {
    comps := []ir.Component{
        {Name: "a", Refs: []ir.ComponentRef{{Component: "b"}}},
        {Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
    }
    if _, err := topoSort(comps); err == nil {
        t.Fatal("topoSort: nil err for cycle")
    }
}

func TestTopoSort_UnknownRef(t *testing.T) {
    comps := []ir.Component{
        {Name: "a", Refs: []ir.ComponentRef{{Component: "ghost"}}},
    }
    if _, err := topoSort(comps); err == nil {
        t.Fatal("topoSort: nil err for unknown ref")
    }
}

func names(cs []ir.Component) []string {
    out := make([]string, len(cs))
    for i, c := range cs {
        out[i] = c.Name
    }
    return out
}

func positions(cs []ir.Component) map[string]int {
    out := map[string]int{}
    for i, c := range cs {
        out[c.Name] = i
    }
    return out
}
```

- [ ] **Step 2: Verify failure**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestTopoSort
```

Expected: FAIL — `topoSort` undefined.

- [ ] **Step 3: Implement orchestrator.go**

Create `pkg/provisioner/orchestrator.go`:

```go
package provisioner

import (
    "context"
    "fmt"
    "runtime"
    "sort"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

// topoSort returns components in dependency order: if B references A, A appears
// before B. Cycles return an error.
func topoSort(components []ir.Component) ([]ir.Component, error) {
    byName := make(map[string]ir.Component, len(components))
    for _, c := range components {
        if _, dup := byName[c.Name]; dup {
            return nil, fmt.Errorf("topoSort: duplicate component %q", c.Name)
        }
        byName[c.Name] = c
    }
    indeg := make(map[string]int, len(components))
    children := make(map[string][]string, len(components))
    for _, c := range components {
        indeg[c.Name] += 0
        for _, ref := range c.Refs {
            if _, ok := byName[ref.Component]; !ok {
                return nil, fmt.Errorf("topoSort: component %q references unknown %q", c.Name, ref.Component)
            }
            indeg[c.Name]++
            children[ref.Component] = append(children[ref.Component], c.Name)
        }
    }
    queue := []string{}
    for name, d := range indeg {
        if d == 0 {
            queue = append(queue, name)
        }
    }
    sort.Strings(queue) // deterministic order among independent components
    var out []ir.Component
    for len(queue) > 0 {
        head := queue[0]
        queue = queue[1:]
        out = append(out, byName[head])
        kids := append([]string{}, children[head]...)
        sort.Strings(kids)
        for _, k := range kids {
            indeg[k]--
            if indeg[k] == 0 {
                queue = append(queue, k)
            }
        }
    }
    if len(out) != len(components) {
        return nil, fmt.Errorf("topoSort: cycle detected (%d of %d components resolved)", len(out), len(components))
    }
    return out, nil
}

// targetWorker is the per-target function the orchestrator invokes. The
// orchestrator owns scheduling, error aggregation, and cancellation; the
// worker owns the work itself.
type targetWorker func(ctx context.Context, component ir.Component, target ir.DeploymentTarget) TargetApplyResult

// concurrencyCaps captures global / per-cloud / per-credential limits.
type concurrencyCaps struct {
    Global         int
    PerCloud       int
    PerCredential  int
}

func resolveCaps(in concurrencyCaps) concurrencyCaps {
    out := in
    if out.Global <= 0 {
        out.Global = runtime.NumCPU()
    }
    if out.PerCloud <= 0 {
        out.PerCloud = 8
    }
    if out.PerCredential <= 0 {
        out.PerCredential = 4
    }
    return out
}

// semaphores holds the three named semaphores. Acquisition order is always
// global -> cloud -> credential to avoid deadlock.
type semaphores struct {
    global     chan struct{}
    perCloud   map[string]chan struct{}
    perCred    map[string]chan struct{}
    capPerCloud, capPerCred int
    mu         sync.Mutex
}

func newSemaphores(caps concurrencyCaps) *semaphores {
    return &semaphores{
        global:      make(chan struct{}, caps.Global),
        perCloud:    map[string]chan struct{}{},
        perCred:     map[string]chan struct{}{},
        capPerCloud: caps.PerCloud,
        capPerCred:  caps.PerCredential,
    }
}

func (s *semaphores) acquire(ctx context.Context, cloud, cred string) (release func(), err error) {
    select {
    case s.global <- struct{}{}:
    case <-ctx.Done():
        return nil, ctx.Err()
    }
    s.mu.Lock()
    cs, ok := s.perCloud[cloud]
    if !ok {
        cs = make(chan struct{}, s.capPerCloud)
        s.perCloud[cloud] = cs
    }
    crs, ok := s.perCred[cred]
    if !ok {
        crs = make(chan struct{}, s.capPerCred)
        s.perCred[cred] = crs
    }
    s.mu.Unlock()
    select {
    case cs <- struct{}{}:
    case <-ctx.Done():
        <-s.global
        return nil, ctx.Err()
    }
    select {
    case crs <- struct{}{}:
    case <-ctx.Done():
        <-cs
        <-s.global
        return nil, ctx.Err()
    }
    return func() {
        <-crs
        <-cs
        <-s.global
    }, nil
}

// runComponent runs one component's targets according to its policy and the
// concurrency caps. Returns one TargetApplyResult per target.
func runComponent(ctx context.Context, comp ir.Component, sems *semaphores, work targetWorker) []TargetApplyResult {
    results := make([]TargetApplyResult, len(comp.Targets))
    if comp.Policy.Serial {
        for i, t := range comp.Targets {
            results[i] = runOne(ctx, comp, t, sems, work)
        }
        return results
    }
    var wg sync.WaitGroup
    for i, t := range comp.Targets {
        wg.Add(1)
        go func(i int, t ir.DeploymentTarget) {
            defer wg.Done()
            results[i] = runOne(ctx, comp, t, sems, work)
        }(i, t)
    }
    wg.Wait()
    return results
}

func runOne(ctx context.Context, comp ir.Component, t ir.DeploymentTarget, sems *semaphores, work targetWorker) TargetApplyResult {
    rel, err := sems.acquire(ctx, t.Cloud, t.CredentialRef)
    if err != nil {
        return TargetApplyResult{
            Component:  comp.Name,
            Cloud:      t.Cloud,
            Region:     t.Region,
            Status:     RunStatusFailed,
            Error:      err,
            StartedAt:  time.Now().UTC(),
            FinishedAt: time.Now().UTC(),
        }
    }
    defer rel()
    return work(ctx, comp, t)
}

// runDAG runs the full project's components in topological order. After each
// component, if any of its targets failed, the partial-failure policy decides
// what happens next.
func runDAG(ctx context.Context, components []ir.Component, sems *semaphores, work targetWorker, policy PartialFailurePolicy, eg *errgroup.Group) ([]TargetApplyResult, error) {
    ordered, err := topoSort(components)
    if err != nil {
        return nil, err
    }
    var all []TargetApplyResult
    failedComponents := map[string]bool{}
    for _, comp := range ordered {
        // If any upstream ref failed, skip this component (chained failure).
        if hasFailedUpstream(comp, failedComponents) {
            for _, t := range comp.Targets {
                all = append(all, TargetApplyResult{
                    Component: comp.Name,
                    Cloud:     t.Cloud,
                    Region:    t.Region,
                    Status:    RunStatusSkipped,
                    Error:     fmt.Errorf("upstream component failed"),
                    StartedAt: time.Now().UTC(),
                    FinishedAt: time.Now().UTC(),
                })
            }
            failedComponents[comp.Name] = true
            continue
        }
        compResults := runComponent(ctx, comp, sems, work)
        all = append(all, compResults...)
        if anyFailed(compResults) {
            failedComponents[comp.Name] = true
            if policy == PartialFailureRollback {
                // Caller handles rollback at the apply level; orchestrator
                // signals by halting forward progress.
                return all, ErrPartialFailure
            }
            // leave / retry-failed both continue; the apply path inspects
            // results and retries / records as appropriate.
        }
    }
    return all, nil
}

func hasFailedUpstream(comp ir.Component, failed map[string]bool) bool {
    for _, ref := range comp.Refs {
        if failed[ref.Component] {
            return true
        }
    }
    return false
}

func anyFailed(rs []TargetApplyResult) bool {
    for _, r := range rs {
        if r.Status == RunStatusFailed {
            return true
        }
    }
    return false
}

// ErrPartialFailure is returned by runDAG when policy=rollback and at least
// one target failed; the apply path catches it and triggers rollback.
var ErrPartialFailure = fmt.Errorf("provisioner: partial failure with rollback policy")
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestTopoSort
PATH=$HOME/.local/go/bin:$PATH go vet ./pkg/provisioner/
git add pkg/provisioner/orchestrator.go pkg/provisioner/orchestrator_test.go
git commit -m "provisioner: DAG topological sort + per-component parallel/serial fan-out + three-semaphore caps"
```

---

## Task 5: Apply implementation (single-target happy path, no policy yet)

**Files:**
- Create: `pkg/provisioner/apply.go`
- Create: `pkg/provisioner/apply_test.go`
- Modify: `pkg/provisioner/provisioner.go` (delete the Apply stub)

Phase-5a: get a working `Apply` for the simple case (single target, no failures). Tasks 6–7 add partial-failure / rollback / retry.

- [ ] **Step 1: Write failing test**

Create `pkg/provisioner/apply_test.go`:

```go
package provisioner_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestApply_SingleTargetHappyPath(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    fakeRunner := tofu.NewFakeRunner()
    fakeRunner.PlanFileContents = []byte("FAKE-PLAN")
    fakeRunner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0","serial":1,"values":{"outputs":{"vpc_id":{"value":"vpc-0xyz","type":"string"}},"root_module":{"resources":[{"address":"aws_vpc.web","type":"aws_vpc","name":"web","values":{"id":"vpc-0xyz","cidr_block":"10.0.0.0/16"}}]}}}`)
    fakeRunner.OutputReturn = map[string]any{"vpc_id": "vpc-0xyz"}

    p, err := provisioner.New(provisioner.Config{
        WorkRoot: t.TempDir(),
        Adapters: reg,
        Runner:   fakeRunner,
    })
    if err != nil {
        t.Fatalf("New: %v", err)
    }

    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1,
        Name:       "x",
        Stacks:     map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "web", Type: "network",
            Spec:    map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }

    planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-t",
    })
    if err != nil {
        t.Fatalf("Plan: %v", err)
    }

    applyRes, err := p.Apply(context.Background(), provisioner.ApplyInput{
        PlanResult: planRes,
        OrgID:      "local",
    })
    if err != nil {
        t.Fatalf("Apply: %v", err)
    }
    if applyRes.Status != provisioner.ApplySucceeded {
        t.Errorf("Status = %q, want succeeded", applyRes.Status)
    }
    if len(applyRes.TargetResults) != 1 {
        t.Fatalf("TargetResults len = %d", len(applyRes.TargetResults))
    }
    tr := applyRes.TargetResults[0]
    if tr.Status != provisioner.RunStatusSucceeded {
        t.Errorf("target status = %q", tr.Status)
    }
    if tr.Outputs["vpc_id"] != "vpc-0xyz" {
        t.Errorf("outputs not captured: %v", tr.Outputs)
    }
    if tr.State == nil || len(tr.State.Resources) != 1 {
        t.Errorf("state not captured: %+v", tr.State)
    }
    if len(fakeRunner.ApplyCalls) != 1 {
        t.Errorf("Apply runner calls = %d, want 1", len(fakeRunner.ApplyCalls))
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -run TestApply_SingleTargetHappyPath -v
```

Expected: FAIL — Apply returns ErrNotImplementedYet.

- [ ] **Step 3: Implement apply.go**

Create `pkg/provisioner/apply.go`:

```go
package provisioner

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/klehmer/nimbusfab/internal/state/bridge"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
    if in.PlanResult == nil {
        return nil, fmt.Errorf("provisioner.Apply: PlanResult required")
    }
    if in.PartialFailure == "" {
        in.PartialFailure = PartialFailureLeave
    }
    if in.MaxRetries <= 0 {
        in.MaxRetries = 1
    }

    sems := newSemaphores(resolveCaps(concurrencyCaps{
        Global:        in.MaxConcurrentTargets,
        PerCloud:      in.MaxConcurrentPerCloud,
        PerCredential: 0,
    }))

    work := rp.applyWorker(in)
    plan := in.PlanResult

    targetsByID := map[string]TargetPlan{}
    componentsOrdered := []ir.Component{} // reconstruct from PlanResult ordering

    // Phase 2 keeps it simple: we already have a flat list of target plans in
    // PlanResult.Targets; group by component name preserving order.
    seenComponents := map[string]bool{}
    for _, tp := range plan.Targets {
        targetsByID[tp.DeploymentTargetID] = tp
        if !seenComponents[tp.Component] {
            seenComponents[tp.Component] = true
            componentsOrdered = append(componentsOrdered, ir.Component{
                Name: tp.Component,
            })
        }
    }
    for i, c := range componentsOrdered {
        for _, tp := range plan.Targets {
            if tp.Component == c.Name {
                componentsOrdered[i].Targets = append(componentsOrdered[i].Targets, ir.DeploymentTarget{
                    Cloud:  tp.Cloud,
                    Region: tp.Region,
                })
            }
        }
    }

    var results []TargetApplyResult
    for _, comp := range componentsOrdered {
        cResults := runComponent(ctx, comp, sems, work)
        results = append(results, cResults...)
    }

    return &ApplyResult{
        DeploymentID:  plan.DeploymentID,
        Status:        summarizeApplyStatus(results),
        TargetResults: results,
        GeneratedAt:   time.Now().UTC(),
    }, nil
}

func (rp *runtimeProvisioner) applyWorker(in ApplyInput) targetWorker {
    plan := in.PlanResult
    return func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
        startedAt := time.Now().UTC()
        tp := findTargetPlan(plan, comp.Name, t.Cloud, t.Region)
        if tp == nil {
            return TargetApplyResult{
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                Status: RunStatusFailed,
                Error:  fmt.Errorf("apply: no plan for %s/%s/%s", comp.Name, t.Cloud, t.Region),
                StartedAt: startedAt, FinishedAt: time.Now().UTC(),
            }
        }
        emit(in.EventSink, RunEvent{
            Timestamp:          time.Now().UTC(),
            DeploymentTargetID: tp.DeploymentTargetID,
            Component:          comp.Name, Cloud: t.Cloud, Region: t.Region,
            Kind: RunEventStart, Message: "apply starting",
        })
        ws := tofu.Workspace{Dir: tp.WorkspaceDir}
        if err := rp.cfg.Runner.Apply(ctx, ws, tp.PlanFile, tofu.ApplyOpts{AutoApprove: in.AutoApprove}); err != nil {
            emit(in.EventSink, RunEvent{
                Timestamp: time.Now().UTC(), DeploymentTargetID: tp.DeploymentTargetID,
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                Kind: RunEventFailure, Message: err.Error(),
            })
            return TargetApplyResult{
                DeploymentTargetID: tp.DeploymentTargetID,
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                RunID:     "run-" + uuid.NewString(),
                Status:    RunStatusFailed, Error: fmt.Errorf("tofu apply: %w", err),
                StartedAt: startedAt, FinishedAt: time.Now().UTC(),
            }
        }
        // Capture state.
        stateBytes, err := rp.cfg.Runner.StateShow(ctx, ws)
        var snap *StateSnapshot
        if err == nil {
            snap, _ = bridge.Parse(stateBytes)
            if snap != nil {
                snap.DeploymentTargetID = tp.DeploymentTargetID
            }
        }
        // Capture outputs.
        outputs, _ := rp.cfg.Runner.Output(ctx, ws)
        emit(in.EventSink, RunEvent{
            Timestamp: time.Now().UTC(), DeploymentTargetID: tp.DeploymentTargetID,
            Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
            Kind: RunEventSuccess, Message: "apply complete",
        })
        return TargetApplyResult{
            DeploymentTargetID: tp.DeploymentTargetID,
            Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
            RunID:     "run-" + uuid.NewString(),
            Status:    RunStatusSucceeded,
            Outputs:   outputs,
            State:     snap,
            StartedAt: startedAt, FinishedAt: time.Now().UTC(),
        }
    }
}

func findTargetPlan(plan *PlanResult, component, cloud, region string) *TargetPlan {
    for i := range plan.Targets {
        tp := &plan.Targets[i]
        if tp.Component == component && tp.Cloud == cloud && tp.Region == region {
            return tp
        }
    }
    return nil
}

func summarizeApplyStatus(results []TargetApplyResult) ApplyStatus {
    var succeeded, failed, skipped int
    for _, r := range results {
        switch r.Status {
        case RunStatusSucceeded:
            succeeded++
        case RunStatusFailed:
            failed++
        case RunStatusSkipped:
            skipped++
        }
    }
    switch {
    case failed == 0 && skipped == 0:
        return ApplySucceeded
    case succeeded == 0:
        return ApplyFailed
    default:
        return ApplyPartialFailure
    }
}

func emit(ch chan<- RunEvent, e RunEvent) {
    if ch == nil {
        return
    }
    select {
    case ch <- e:
    default:
        // Sink is full; drop to avoid blocking apply.
    }
}
```

- [ ] **Step 4: Delete stub from provisioner.go**

Remove the `(*runtimeProvisioner).Apply` method that returns `ErrNotImplementedYet` (now replaced by `apply.go`'s real implementation).

- [ ] **Step 5: Run, verify pass + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestApply_SingleTargetHappyPath
git add pkg/provisioner/apply.go pkg/provisioner/apply_test.go pkg/provisioner/provisioner.go
git commit -m "provisioner: Apply happy path — tofu apply + state capture + outputs"
```

---

## Task 6: Multi-target Apply + partial-failure policies

**Files:**
- Modify: `pkg/provisioner/apply.go` (add retry-failed + rollback)
- Create: `pkg/provisioner/apply_policies_test.go`

- [ ] **Step 1: Write failing tests for each policy**

Create `pkg/provisioner/apply_policies_test.go`:

```go
package provisioner_test

import (
    "context"
    "errors"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func twoTargetProject() *ir.Project {
    return &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1,
        Name:       "x",
        Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "web", Type: "network",
            Spec: map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{
                {Cloud: "aws", Region: "us-east-1"},
                {Cloud: "aws", Region: "eu-west-1"},
            },
        }},
    }
}

// flakyRunner fails the first N apply calls then succeeds.
type flakyRunner struct {
    *tofu.FakeRunner
    failCount int
    callCount int
}

func newFlakyRunner(failCount int) *flakyRunner {
    return &flakyRunner{FakeRunner: tofu.NewFakeRunner(), failCount: failCount}
}

func (f *flakyRunner) Apply(ctx context.Context, ws tofu.Workspace, planFile string, opts tofu.ApplyOpts) error {
    f.callCount++
    if f.callCount <= f.failCount {
        return errors.New("scripted apply failure")
    }
    return f.FakeRunner.Apply(ctx, ws, planFile, opts)
}

func TestApply_LeavePolicy_PartialFailureRecorded(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := newFlakyRunner(1) // fail first apply call, succeed the second
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-l",
    })
    if err != nil { t.Fatalf("Plan: %v", err) }

    res, err := p.Apply(context.Background(), provisioner.ApplyInput{
        PlanResult: planRes, OrgID: "local",
        PartialFailure: provisioner.PartialFailureLeave,
    })
    if err != nil { t.Fatalf("Apply: %v", err) }
    if res.Status != provisioner.ApplyPartialFailure {
        t.Errorf("Status = %q, want partial_failure", res.Status)
    }
    var succeeded, failed int
    for _, r := range res.TargetResults {
        switch r.Status {
        case provisioner.RunStatusSucceeded: succeeded++
        case provisioner.RunStatusFailed:    failed++
        }
    }
    if succeeded != 1 || failed != 1 {
        t.Errorf("succeeded=%d failed=%d, want 1 each", succeeded, failed)
    }
}

func TestApply_RetryFailedPolicy(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := newFlakyRunner(1)
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-r",
    })
    res, err := p.Apply(context.Background(), provisioner.ApplyInput{
        PlanResult:    planRes,
        OrgID:         "local",
        PartialFailure: provisioner.PartialFailureRetryFailed,
        MaxRetries:    2,
    })
    if err != nil { t.Fatalf("Apply: %v", err) }
    if res.Status != provisioner.ApplySucceeded {
        t.Errorf("Status = %q, want succeeded after retry", res.Status)
    }
}

// alwaysFailingRunner fails every apply.
type alwaysFailingRunner struct{ *tofu.FakeRunner }

func newAlwaysFailingRunner() *alwaysFailingRunner {
    return &alwaysFailingRunner{FakeRunner: tofu.NewFakeRunner()}
}
func (a *alwaysFailingRunner) Apply(ctx context.Context, ws tofu.Workspace, planFile string, opts tofu.ApplyOpts) error {
    return errors.New("scripted permanent failure")
}

func TestApply_RollbackPolicy_DestroysSucceeded(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := newFlakyRunner(1) // first apply fails, second succeeds
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-rb",
    })
    res, _ := p.Apply(context.Background(), provisioner.ApplyInput{
        PlanResult:     planRes,
        OrgID:          "local",
        PartialFailure: provisioner.PartialFailureRollback,
    })
    if res.Status != provisioner.ApplyFailed {
        t.Errorf("Status = %q, want failed (rolled back)", res.Status)
    }
    // The succeeded target should now be marked Reverted.
    var reverted int
    for _, r := range res.TargetResults {
        if r.Status == provisioner.RunStatusReverted {
            reverted++
        }
    }
    if reverted != 1 {
        t.Errorf("reverted count = %d, want 1", reverted)
    }
    if len(runner.DestroyCalls) != 1 {
        t.Errorf("Destroy calls = %d, want 1 (the succeeded target)", len(runner.DestroyCalls))
    }
}
```

- [ ] **Step 2: Verify failures**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run "TestApply_"
```

Expected: the leave-policy partial-failure test partly works (since happy-path Apply already handles failures); retry-failed and rollback tests fail.

- [ ] **Step 3: Extend apply.go with retry + rollback**

Modify `apply.go`'s `Apply` to call new helpers after the initial pass:

```go
// At the bottom of Apply, before the return, insert:
results = rp.maybeRetry(ctx, in, results, work)
if in.PartialFailure == PartialFailureRollback && hasAnyFailure(results) {
    results = rp.rollback(ctx, in, results)
}
return &ApplyResult{
    DeploymentID:  plan.DeploymentID,
    Status:        summarizeApplyStatus(results),
    TargetResults: results,
    GeneratedAt:   time.Now().UTC(),
}, nil
```

Add helpers in `apply.go`:

```go
func hasAnyFailure(rs []TargetApplyResult) bool {
    for _, r := range rs {
        if r.Status == RunStatusFailed {
            return true
        }
    }
    return false
}

func (rp *runtimeProvisioner) maybeRetry(ctx context.Context, in ApplyInput, results []TargetApplyResult, work targetWorker) []TargetApplyResult {
    if in.PartialFailure != PartialFailureRetryFailed {
        return results
    }
    sems := newSemaphores(resolveCaps(concurrencyCaps{
        Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
    }))
    for attempt := 1; attempt <= in.MaxRetries; attempt++ {
        if !hasAnyFailure(results) {
            break
        }
        for i, r := range results {
            if r.Status != RunStatusFailed {
                continue
            }
            comp := ir.Component{Name: r.Component, Targets: []ir.DeploymentTarget{{
                Cloud: r.Cloud, Region: r.Region,
            }}}
            retried := runComponent(ctx, comp, sems, work)
            if len(retried) > 0 {
                results[i] = retried[0]
            }
        }
    }
    return results
}

func (rp *runtimeProvisioner) rollback(ctx context.Context, in ApplyInput, results []TargetApplyResult) []TargetApplyResult {
    plan := in.PlanResult
    for i, r := range results {
        if r.Status != RunStatusSucceeded {
            continue
        }
        tp := findTargetPlan(plan, r.Component, r.Cloud, r.Region)
        if tp == nil {
            continue
        }
        ws := tofu.Workspace{Dir: tp.WorkspaceDir}
        if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: true}); err != nil {
            results[i].Status = RunStatusFailed
            results[i].Error = fmt.Errorf("rollback destroy failed: %w (original status: succeeded)", err)
            continue
        }
        results[i].Status = RunStatusReverted
        results[i].FinishedAt = time.Now().UTC()
    }
    return results
}
```

Add `"github.com/klehmer/nimbusfab/pkg/ir"` to imports if not already present.

- [ ] **Step 4: Run, verify all policy tests pass**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run "TestApply_"
```

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/apply.go pkg/provisioner/apply_policies_test.go
git commit -m "provisioner: partial-failure policies (leave/retry-failed/rollback) for Apply"
```

---

## Task 7: Destroy implementation

**Files:**
- Create: `pkg/provisioner/destroy.go`
- Create: `pkg/provisioner/destroy_test.go`
- Modify: `pkg/provisioner/provisioner.go` (delete Destroy stub)

- [ ] **Step 1: Write failing test**

Create `pkg/provisioner/destroy_test.go`:

```go
package provisioner_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestDestroy_HappyPath(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-d",
    })
    if err != nil { t.Fatalf("Plan: %v", err) }

    res, err := p.Destroy(context.Background(), provisioner.DestroyInput{
        PlanResult: planRes, OrgID: "local",
    })
    if err != nil { t.Fatalf("Destroy: %v", err) }
    if res.Status != provisioner.ApplySucceeded {
        t.Errorf("Status = %q, want succeeded", res.Status)
    }
    if len(runner.DestroyCalls) != 2 {
        t.Errorf("Destroy calls = %d, want 2", len(runner.DestroyCalls))
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestDestroy_HappyPath
```

- [ ] **Step 3: Implement destroy.go**

Create `pkg/provisioner/destroy.go`:

```go
package provisioner

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
    if in.PlanResult == nil {
        return nil, fmt.Errorf("provisioner.Destroy: PlanResult required (Phase 2 does not yet resolve deployments from inventory)")
    }
    if in.PartialFailure == "" {
        in.PartialFailure = PartialFailureLeave
    }

    sems := newSemaphores(resolveCaps(concurrencyCaps{
        Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
    }))

    plan := in.PlanResult
    work := func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
        startedAt := time.Now().UTC()
        tp := findTargetPlan(plan, comp.Name, t.Cloud, t.Region)
        if tp == nil {
            return TargetApplyResult{
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                Status: RunStatusFailed,
                Error:  fmt.Errorf("destroy: no plan for %s/%s/%s", comp.Name, t.Cloud, t.Region),
                StartedAt: startedAt, FinishedAt: time.Now().UTC(),
            }
        }
        ws := tofu.Workspace{Dir: tp.WorkspaceDir}
        emit(in.EventSink, RunEvent{
            Timestamp:          time.Now().UTC(),
            DeploymentTargetID: tp.DeploymentTargetID,
            Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
            Kind: RunEventStart, Message: "destroy starting",
        })
        if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: true}); err != nil {
            return TargetApplyResult{
                DeploymentTargetID: tp.DeploymentTargetID,
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                RunID:  "run-" + uuid.NewString(),
                Status: RunStatusFailed, Error: fmt.Errorf("tofu destroy: %w", err),
                StartedAt: startedAt, FinishedAt: time.Now().UTC(),
            }
        }
        return TargetApplyResult{
            DeploymentTargetID: tp.DeploymentTargetID,
            Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
            RunID:  "run-" + uuid.NewString(),
            Status: RunStatusSucceeded,
            StartedAt: startedAt, FinishedAt: time.Now().UTC(),
        }
    }

    // Destroy walks components in REVERSE topo order: dependents first.
    componentsOrdered := componentsFromPlan(plan)
    reverse(componentsOrdered)

    var results []TargetApplyResult
    for _, comp := range componentsOrdered {
        results = append(results, runComponent(ctx, comp, sems, work)...)
    }
    return &ApplyResult{
        DeploymentID:  plan.DeploymentID,
        Status:        summarizeApplyStatus(results),
        TargetResults: results,
        GeneratedAt:   time.Now().UTC(),
    }, nil
}

func componentsFromPlan(plan *PlanResult) []ir.Component {
    seen := map[string]bool{}
    var out []ir.Component
    for _, tp := range plan.Targets {
        if !seen[tp.Component] {
            seen[tp.Component] = true
            out = append(out, ir.Component{Name: tp.Component})
        }
    }
    for i, c := range out {
        for _, tp := range plan.Targets {
            if tp.Component == c.Name {
                out[i].Targets = append(out[i].Targets, ir.DeploymentTarget{
                    Cloud: tp.Cloud, Region: tp.Region,
                })
            }
        }
    }
    return out
}

func reverse(cs []ir.Component) {
    for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
        cs[i], cs[j] = cs[j], cs[i]
    }
}
```

- [ ] **Step 4: Delete Destroy stub from provisioner.go**

Remove the `(*runtimeProvisioner).Destroy` stub.

- [ ] **Step 5: Run, commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestDestroy
git add pkg/provisioner/destroy.go pkg/provisioner/destroy_test.go pkg/provisioner/provisioner.go
git commit -m "provisioner: Destroy walks components in reverse topo order via tofu destroy"
```

---

## Task 8: Drift detection (`tofu plan -refresh-only`)

**Files:**
- Create: `pkg/provisioner/drift.go`
- Create: `pkg/provisioner/drift_test.go`
- Modify: `internal/tofu/runner.go` (add `PlanOpts.RefreshOnly`)
- Modify: `internal/tofu/fake_runner.go` (honor RefreshOnly; allow scripting drift)
- Modify: `internal/tofu/exec_runner.go` (pass -refresh-only when set)

- [ ] **Step 1: Extend PlanOpts**

In `internal/tofu/runner.go`, ADD a field to `PlanOpts`:

```go
type PlanOpts struct {
    Destroy bool
    Refresh bool
    RefreshOnly bool        // NEW: pass -refresh-only
    Targets []string
    OutFile string
    Timeout time.Duration
}
```

In `internal/tofu/exec_runner.go`, in `Plan()`, append `-refresh-only` to args when opts.RefreshOnly is set.

- [ ] **Step 2: Extend FakeRunner to script drift**

In `internal/tofu/fake_runner.go`, add a field:

```go
// DriftPlan, if non-nil, is returned from Plan when PlanOpts.RefreshOnly is true.
DriftPlan *PlanArtifact
```

In the `Plan()` method, branch on `opts.RefreshOnly`:

```go
if opts.RefreshOnly && f.DriftPlan != nil {
    if opts.OutFile != "" && len(f.PlanFileContents) > 0 {
        _ = os.WriteFile(opts.OutFile, f.PlanFileContents, 0o600)
    }
    return f.DriftPlan, nil
}
```

- [ ] **Step 3: Write failing drift test**

Create `pkg/provisioner/drift_test.go`:

```go
package provisioner_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestDetectDrift_NoDrift(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.DriftPlan = &tofu.PlanArtifact{
        PlanFile:   "/tmp/drift.bin",
        JSONPlan:   []byte(`{"resource_changes":[]}`),
        HasChanges: false,
    }
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-dr",
    })

    rep, err := p.DetectDrift(context.Background(), provisioner.DriftInput{
        PlanResult: planRes, OrgID: "local",
    })
    if err != nil { t.Fatalf("DetectDrift: %v", err) }
    if len(rep.TargetReports) != 2 {
        t.Fatalf("TargetReports len = %d", len(rep.TargetReports))
    }
    for _, tr := range rep.TargetReports {
        if tr.HasDrift {
            t.Errorf("expected no drift for %s/%s", tr.Cloud, tr.Region)
        }
    }
}

func TestDetectDrift_WithDrift(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.DriftPlan = &tofu.PlanArtifact{
        PlanFile: "/tmp/drift.bin",
        JSONPlan: []byte(`{"resource_changes":[{"address":"aws_vpc.web","change":{"actions":["update"]}}]}`),
        HasChanges: true,
    }
    p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

    planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
        Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-dr2",
    })
    rep, err := p.DetectDrift(context.Background(), provisioner.DriftInput{
        PlanResult: planRes, OrgID: "local",
    })
    if err != nil { t.Fatalf("DetectDrift: %v", err) }
    var driftedTargets int
    for _, tr := range rep.TargetReports {
        if tr.HasDrift {
            driftedTargets++
        }
    }
    if driftedTargets != 2 {
        t.Errorf("driftedTargets = %d, want 2", driftedTargets)
    }
}
```

- [ ] **Step 4: Implement drift.go**

Create `pkg/provisioner/drift.go`:

```go
package provisioner

import (
    "context"
    "encoding/json"
    "fmt"
    "path/filepath"
    "time"

    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

// DetectDrift adds a Provisioner method (not yet in the interface) that runs
// `tofu plan -refresh-only` per target and aggregates per-resource drift.
// Phase 2 surfaces this from the engine and CLI; the interface gets the
// method here.
func (rp *runtimeProvisioner) DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error) {
    if in.PlanResult == nil {
        return nil, fmt.Errorf("provisioner.DetectDrift: PlanResult required (Phase 2)")
    }
    sems := newSemaphores(resolveCaps(concurrencyCaps{
        Global: in.MaxConcurrentTargets, PerCloud: in.MaxConcurrentPerCloud,
    }))
    plan := in.PlanResult

    type slot struct {
        report TargetDriftReport
        err    error
    }
    slots := make([]slot, len(plan.Targets))

    work := func(ctx context.Context, comp ir.Component, t ir.DeploymentTarget) TargetApplyResult {
        // Drift uses TargetApplyResult as a structural carrier; not persisted.
        idx := findPlanIndex(plan, comp.Name, t.Cloud, t.Region)
        tp := plan.Targets[idx]
        ws := tofu.Workspace{Dir: tp.WorkspaceDir}
        driftFile := filepath.Join(tp.WorkspaceDir, "drift.bin")
        artifact, err := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{
            OutFile:     driftFile,
            RefreshOnly: true,
        })
        if err != nil {
            slots[idx] = slot{report: TargetDriftReport{
                DeploymentTargetID: tp.DeploymentTargetID,
                Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
                Error: err,
            }}
            return TargetApplyResult{}
        }
        rep := TargetDriftReport{
            DeploymentTargetID: tp.DeploymentTargetID,
            Component: comp.Name, Cloud: t.Cloud, Region: t.Region,
            HasDrift: artifact.HasChanges,
        }
        rep.Drifted, rep.Gone, rep.Discovered = extractDrift(artifact.JSONPlan)
        slots[idx] = slot{report: rep}
        return TargetApplyResult{}
    }

    componentsOrdered := componentsFromPlan(plan)
    for _, comp := range componentsOrdered {
        _ = runComponent(ctx, comp, sems, work)
    }

    rep := &DriftReport{
        DeploymentID: plan.DeploymentID,
        GeneratedAt:  time.Now().UTC(),
    }
    for _, s := range slots {
        rep.TargetReports = append(rep.TargetReports, s.report)
    }
    return rep, nil
}

func findPlanIndex(plan *PlanResult, component, cloud, region string) int {
    for i, tp := range plan.Targets {
        if tp.Component == component && tp.Cloud == cloud && tp.Region == region {
            return i
        }
    }
    return -1
}

func extractDrift(jsonPlan []byte) (drifted, gone, discovered []DriftedResource) {
    var p struct {
        ResourceChanges []struct {
            Address string `json:"address"`
            Change  struct {
                Actions []string       `json:"actions"`
                Before  map[string]any `json:"before"`
                After   map[string]any `json:"after"`
            } `json:"change"`
        } `json:"resource_changes"`
    }
    _ = json.Unmarshal(jsonPlan, &p)
    for _, rc := range p.ResourceChanges {
        kind := "drift"
        if len(rc.Change.Actions) > 0 {
            switch rc.Change.Actions[0] {
            case "delete":
                kind = "gone"
            case "create":
                kind = "discovered"
            }
        }
        dr := DriftedResource{
            Address:          rc.Address,
            Kind:             kind,
            AttributesBefore: rc.Change.Before,
            AttributesAfter:  rc.Change.After,
            DiffSummary:      fmt.Sprintf("%v", rc.Change.Actions),
        }
        switch kind {
        case "gone":
            gone = append(gone, dr)
        case "discovered":
            discovered = append(discovered, dr)
        default:
            drifted = append(drifted, dr)
        }
    }
    return
}
```

- [ ] **Step 5: Extend `Provisioner` interface**

In `pkg/provisioner/provisioner.go`, ADD to the `Provisioner` interface:

```go
DetectDrift(ctx context.Context, in DriftInput) (*DriftReport, error)
```

- [ ] **Step 6: Run, commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run TestDetectDrift
PATH=$HOME/.local/go/bin:$PATH go test ./internal/tofu/ -v
git add pkg/provisioner/drift.go pkg/provisioner/drift_test.go pkg/provisioner/provisioner.go internal/tofu/
git commit -m "provisioner: DetectDrift via tofu plan -refresh-only; FakeRunner gains DriftPlan scripting"
```

---

## Task 9: Cross-component refs — data.terraform_remote_state

**Files:**
- Create: `pkg/provisioner/refs.go`
- Create: `pkg/provisioner/refs_test.go`
- Modify: `pkg/provisioner/workspace.go` (accept upstream refs)
- Modify: `pkg/provisioner/plan.go` (collect outputs from upstream apply, pass to dependent plan)

Phase-9a scope: when a component declares `refs:`, the provisioner inserts a `data "terraform_remote_state"` block in the dependent's workspace pointing at the upstream's state backend, and assembles a `cloud.ResolvedRefs` map keyed by the user's `as` alias so the adapter can use it. Cross-component refs are computed at PLAN time (so the workspace's `data.terraform_remote_state` block is present even before apply); the runtime values resolve at Tofu time.

Phase 2 limits: same-stack only (cross-stack refs are v2). The upstream state backend MUST be reachable from the dependent's process (typical when both use the same backend kind/config; v1 assumes this).

- [ ] **Step 1: Write failing test**

Create `pkg/provisioner/refs_test.go`:

```go
package provisioner

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestWriteWorkspace_WithUpstreamRef(t *testing.T) {
    dir := t.TempDir()
    layout := WorkspaceLayout{
        Dir:            dir,
        ProviderName:   "aws",
        ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
        Backend:        ir.StateBackend{Kind: "local"},
        Primitives: []ir.ResourcePrimitive{{
            TofuType: "aws_instance", TofuName: "app",
            Attributes: map[string]any{"subnet_id": "${data.terraform_remote_state.web_network.outputs.subnet_id}"},
        }},
        UpstreamRefs: []UpstreamStateRef{{
            Component: "web-network",
            Backend:   ir.StateBackend{Kind: "local", Config: map[string]any{"path": "/tmp/upstream.tfstate"}},
        }},
    }
    if err := WriteWorkspace(layout); err != nil {
        t.Fatalf("WriteWorkspace: %v", err)
    }
    body, _ := os.ReadFile(filepath.Join(dir, "main.tf.json"))
    var parsed map[string]any
    _ = json.Unmarshal(body, &parsed)
    data, ok := parsed["data"].(map[string]any)
    if !ok {
        t.Fatalf("no data block: %s", body)
    }
    rs, ok := data["terraform_remote_state"].(map[string]any)
    if !ok {
        t.Fatalf("no terraform_remote_state block: %v", data)
    }
    if _, ok := rs["web_network"]; !ok {
        t.Errorf("missing remote state for web_network: %v", rs)
    }
}
```

- [ ] **Step 2: Implement UpstreamStateRef and integrate into workspace.go**

In `pkg/provisioner/workspace.go`, ADD a new field to `WorkspaceLayout`:

```go
type WorkspaceLayout struct {
    // ... existing fields ...
    UpstreamRefs []UpstreamStateRef
}
```

In a new file `pkg/provisioner/refs.go`:

```go
package provisioner

import (
    "github.com/klehmer/nimbusfab/pkg/ir"
)

// UpstreamStateRef captures one cross-component reference: the dependent's
// workspace needs to read the upstream component's outputs via
// data.terraform_remote_state.
type UpstreamStateRef struct {
    Component string
    Backend   ir.StateBackend
}

// tofuIdentForComponent matches the helper used in cloud adapters so the
// `data.terraform_remote_state.<name>` matches the upstream's local name.
func tofuIdentForComponent(name string) string {
    out := []byte{}
    for i := 0; i < len(name); i++ {
        c := name[i]
        switch {
        case c == '-':
            out = append(out, '_')
        case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_':
            out = append(out, c)
        case c >= 'A' && c <= 'Z':
            out = append(out, c+32)
        default:
            out = append(out, '_')
        }
    }
    if len(out) == 0 || (out[0] >= '0' && out[0] <= '9') {
        out = append([]byte{'_'}, out...)
    }
    return string(out)
}
```

Modify `pkg/provisioner/workspace.go`'s `WriteWorkspace`:

```go
// At the bottom of the files map, append a data block when UpstreamRefs is non-empty:
if len(layout.UpstreamRefs) > 0 {
    data := map[string]any{}
    for _, ref := range layout.UpstreamRefs {
        backend := ref.Backend
        if backend.Kind == "" {
            backend.Kind = "local"
        }
        cfg := backend.Config
        if cfg == nil {
            cfg = map[string]any{}
        }
        data[tofuIdentForComponent(ref.Component)] = map[string]any{
            "backend": backend.Kind,
            "config":  cfg,
        }
    }
    // Replace main.tf.json content with one that also has a data block.
    main := files["main.tf.json"].(map[string]any)
    main["data"] = map[string]any{"terraform_remote_state": data}
    files["main.tf.json"] = main
}
```

(adjust the loop above so this insertion happens BEFORE the serialization loop)

- [ ] **Step 3: Modify plan.go to plumb refs through**

In `pkg/provisioner/plan.go`'s `planOne`, after building the layout but before `WriteWorkspace`, add:

```go
// Gather upstream refs from component definition.
for _, ref := range comp.Refs {
    layout.UpstreamRefs = append(layout.UpstreamRefs, UpstreamStateRef{
        Component: ref.Component,
        Backend:   backend, // Phase 2: same backend as this target; cross-stack refs are v2
    })
}
```

- [ ] **Step 4: Run, commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run "TestWriteWorkspace_WithUpstreamRef"
git add pkg/provisioner/refs.go pkg/provisioner/refs_test.go pkg/provisioner/workspace.go pkg/provisioner/plan.go
git commit -m "provisioner: inject data.terraform_remote_state for cross-component refs"
```

---

## Task 10: Engine.Apply / Engine.Destroy / Engine.DetectDrift wiring

**Files:**
- Modify: `pkg/engine/engine.go` (add type aliases for ApplyResult, DriftReport, etc.)
- Modify: `pkg/engine/plan.go` (replace Apply/Destroy/DetectDrift stubs with real impls)
- Create: `pkg/engine/apply_test.go`

- [ ] **Step 1: Add type aliases in engine.go**

Below the existing PlanResult alias:

```go
type ApplyResult = provisioner.ApplyResult
type TargetApplyResult = provisioner.TargetApplyResult
type ApplyStatus = provisioner.ApplyStatus

// DriftReport (already declared with provisioner-shape fields earlier in
// engine.go) is REPLACED with an alias:
type DriftReport = provisioner.DriftReport
type TargetDriftReport = provisioner.TargetDriftReport
type DriftedResource = provisioner.DriftedResource
```

Delete the existing `DriftReport`/`TargetDrift`/`PrimitiveDrift` types from engine.go (now superseded by aliases).

- [ ] **Step 2: Replace Apply/Destroy/DetectDrift in plan.go**

```go
// In pkg/engine/plan.go:

func (e *runtimeEngine) Apply(ctx context.Context, planID string, opts ApplyOpts) (string, error) {
    // Phase 2: planID is unused (no inventory yet); caller passes PlanResult via a separate Engine method below.
    return "", fmt.Errorf("engine.Apply: use ApplyWithPlan in Phase 2 (inventory persistence not yet wired)")
}

// ApplyWithPlan is the Phase-2 surface: caller passes the PlanResult directly
// since inventory persistence isn't wired. Becomes Apply(planID) in the
// inventory phase.
func (e *runtimeEngine) ApplyWithPlan(ctx context.Context, plan *provisioner.PlanResult, opts ApplyOpts) (*ApplyResult, error) {
    runner := e.cfg.TofuRunner
    if runner == nil { runner = tofu.NewExecRunner() }
    workRoot := e.cfg.WorkRoot
    if workRoot == "" { workRoot = e.cfg.WorkDir }
    if workRoot == "" { workRoot = filepath.Join(os.TempDir(), "nimbusfab") }
    p, err := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: e.cfg.CloudAdapters, Runner: runner})
    if err != nil { return nil, err }
    return p.Apply(ctx, provisioner.ApplyInput{
        PlanResult:     plan,
        OrgID:          e.orgID(),
        PartialFailure: opts.PartialFailure,
        AutoApprove:    opts.AutoApprove,
    })
}

func (e *runtimeEngine) DestroyWithPlan(ctx context.Context, plan *provisioner.PlanResult, opts DestroyOpts) (*ApplyResult, error) {
    runner := e.cfg.TofuRunner
    if runner == nil { runner = tofu.NewExecRunner() }
    workRoot := e.cfg.WorkRoot
    if workRoot == "" { workRoot = e.cfg.WorkDir }
    if workRoot == "" { workRoot = filepath.Join(os.TempDir(), "nimbusfab") }
    p, err := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: e.cfg.CloudAdapters, Runner: runner})
    if err != nil { return nil, err }
    return p.Destroy(ctx, provisioner.DestroyInput{
        PlanResult:  plan,
        OrgID:       e.orgID(),
        AutoApprove: opts.AutoApprove,
    })
}

func (e *runtimeEngine) DetectDriftWithPlan(ctx context.Context, plan *provisioner.PlanResult) (*DriftReport, error) {
    runner := e.cfg.TofuRunner
    if runner == nil { runner = tofu.NewExecRunner() }
    workRoot := e.cfg.WorkRoot
    if workRoot == "" { workRoot = e.cfg.WorkDir }
    if workRoot == "" { workRoot = filepath.Join(os.TempDir(), "nimbusfab") }
    p, err := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: e.cfg.CloudAdapters, Runner: runner})
    if err != nil { return nil, err }
    return p.DetectDrift(ctx, provisioner.DriftInput{
        PlanResult: plan,
        OrgID:      e.orgID(),
    })
}
```

(The existing `DetectDrift(ctx, deploymentID string)` stub on the Engine interface still returns `errNotImplemented` since Phase 2 doesn't have inventory-resolved deployment IDs.)

- [ ] **Step 3: Write engine test**

Create `pkg/engine/apply_test.go`:

```go
package engine_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/engine"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEngine_ApplyWithPlan(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)
    eng, _ := engine.New(context.Background(), engine.Config{
        CloudAdapters: reg, TofuRunner: runner, WorkRoot: t.TempDir(),
    })
    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1, Name: "x",
        Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "web", Type: "network", Spec: map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }
    plan, _ := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
    res, err := eng.ApplyWithPlan(context.Background(), plan, engine.ApplyOpts{})
    if err != nil { t.Fatalf("ApplyWithPlan: %v", err) }
    if res.Status != engine.ApplySucceeded {
        t.Errorf("Status = %q", res.Status)
    }
}
```

- [ ] **Step 4: Commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/engine/ -v
git add pkg/engine/
git commit -m "engine: wire ApplyWithPlan/DestroyWithPlan/DetectDriftWithPlan through provisioner"
```

---

## Task 11: CLI — `nimbusfab apply`, `destroy`, `drift`

**Files:**
- Create: `cmd/cli/apply.go`
- Create: `cmd/cli/destroy.go`
- Create: `cmd/cli/drift.go`
- Create: `cmd/cli/apply_test.go`
- Modify: `cmd/cli/main.go` (register the three new commands)

Phase 2 CLI keeps it simple: each command runs validate → plan → its action in one process. The inventory-aware "apply against an existing plan ID" flow is Phase 3+.

- [ ] **Step 1: Implement apply.go**

Create `cmd/cli/apply.go`:

```go
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
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

type applyArgs struct {
    ProjectPath    string
    Stack          string
    AutoApprove    bool
    PartialFailure string
    Adapters       cloud.Registry
    Runner         tofu.Runner
    WorkRoot       string
    Stdout, Stderr io.Writer
}

func newApplyCommand() *cobra.Command {
    var stack, partialFailure string
    var autoApprove bool
    cmd := &cobra.Command{
        Use:   "apply [path]",
        Short: "Validate, plan, then apply against a stack",
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
            code := runApply(cmd.Context(), applyArgs{
                ProjectPath:    projectPath,
                Stack:          stack,
                AutoApprove:    autoApprove,
                PartialFailure: partialFailure,
                Adapters:       reg,
                Runner:         tofu.NewExecRunner(),
                Stdout:         cmd.OutOrStdout(),
                Stderr:         cmd.ErrOrStderr(),
            })
            if code != 0 {
                os.Exit(code)
            }
            return nil
        },
        SilenceUsage: true, SilenceErrors: true,
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
    cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
    cmd.Flags().StringVar(&partialFailure, "partial-failure", "leave", "leave | rollback | retry-failed")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

func runApply(ctx context.Context, in applyArgs) int {
    if ctx == nil { ctx = context.Background() }
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack is required")
        return 2
    }
    project, err := loader.New().Load(ctx, in.ProjectPath)
    if err != nil {
        fmt.Fprintf(in.Stderr, "load: %v\n", err)
        return 1
    }
    report, err := validator.New().Validate(ctx, project)
    if err != nil {
        fmt.Fprintf(in.Stderr, "validator: %v\n", err)
        return 2
    }
    if report != nil && !report.OK() {
        for _, issue := range report.Issues {
            fmt.Fprintln(in.Stderr, issue.String())
        }
        return 1
    }
    eng, err := engine.New(ctx, engine.Config{
        CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot,
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
    fmt.Fprintf(in.Stdout, "Planning %d targets... ✓\n", len(plan.Targets))
    if !in.AutoApprove {
        fmt.Fprintln(in.Stdout, "(Phase 2: --auto-approve is implied; interactive confirmation lands in inventory phase)")
    }
    res, err := eng.ApplyWithPlan(ctx, plan, engine.ApplyOpts{
        AutoApprove:    true,
        PartialFailure: provisioner.PartialFailurePolicy(in.PartialFailure),
    })
    if err != nil {
        fmt.Fprintf(in.Stderr, "apply: %v\n", err)
        return 1
    }
    var succeeded, failed, reverted, skipped int
    for _, r := range res.TargetResults {
        switch r.Status {
        case provisioner.RunStatusSucceeded: succeeded++
        case provisioner.RunStatusFailed:    failed++
        case provisioner.RunStatusReverted:  reverted++
        case provisioner.RunStatusSkipped:   skipped++
        }
        marker := "✓"
        if r.Status == provisioner.RunStatusFailed { marker = "✗" }
        if r.Status == provisioner.RunStatusReverted { marker = "↶" }
        if r.Status == provisioner.RunStatusSkipped { marker = "—" }
        fmt.Fprintf(in.Stdout, "  %s %s  %s/%s  status=%s\n", marker, r.Component, r.Cloud, r.Region, r.Status)
        if r.Error != nil {
            fmt.Fprintf(in.Stdout, "      %v\n", r.Error)
        }
    }
    fmt.Fprintf(in.Stdout, "\nApply %s: %d succeeded, %d failed, %d reverted, %d skipped\n",
        res.Status, succeeded, failed, reverted, skipped)
    if res.Status != provisioner.ApplySucceeded {
        return 1
    }
    return 0
}
```

Create `cmd/cli/destroy.go` (mirrors apply.go, calls `eng.DestroyWithPlan`):

```go
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
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func newDestroyCommand() *cobra.Command {
    var stack string
    var autoApprove bool
    cmd := &cobra.Command{
        Use:   "destroy [path]",
        Short: "Tear down a stack via tofu destroy",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            projectPath := "."
            if len(args) == 1 { projectPath = args[0] }
            reg := cloud.NewRegistry()
            if err := reg.Register(aws.New()); err != nil { return err }
            code := runDestroy(cmd.Context(), destroyArgs{
                ProjectPath: projectPath, Stack: stack, AutoApprove: autoApprove,
                Adapters: reg, Runner: tofu.NewExecRunner(),
                Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
            })
            if code != 0 { os.Exit(code) }
            return nil
        },
        SilenceUsage: true, SilenceErrors: true,
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
    cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip interactive confirmation")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

type destroyArgs struct {
    ProjectPath, Stack string
    AutoApprove        bool
    Adapters           cloud.Registry
    Runner             tofu.Runner
    WorkRoot           string
    Stdout, Stderr     io.Writer
}

func runDestroy(ctx context.Context, in destroyArgs) int {
    if ctx == nil { ctx = context.Background() }
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack is required")
        return 2
    }
    project, err := loader.New().Load(ctx, in.ProjectPath)
    if err != nil { fmt.Fprintf(in.Stderr, "load: %v\n", err); return 1 }
    rep, err := validator.New().Validate(ctx, project)
    if err != nil { fmt.Fprintf(in.Stderr, "validator: %v\n", err); return 2 }
    if rep != nil && !rep.OK() {
        for _, i := range rep.Issues { fmt.Fprintln(in.Stderr, i.String()) }
        return 1
    }
    eng, err := engine.New(ctx, engine.Config{CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot})
    if err != nil { fmt.Fprintf(in.Stderr, "engine: %v\n", err); return 1 }
    plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
    if err != nil { fmt.Fprintf(in.Stderr, "plan: %v\n", err); return 1 }
    res, err := eng.DestroyWithPlan(ctx, plan, engine.DestroyOpts{AutoApprove: true})
    if err != nil { fmt.Fprintf(in.Stderr, "destroy: %v\n", err); return 1 }
    fmt.Fprintf(in.Stdout, "Destroy %s\n", res.Status)
    for _, r := range res.TargetResults {
        marker := "✓"
        if r.Status == provisioner.RunStatusFailed { marker = "✗" }
        fmt.Fprintf(in.Stdout, "  %s %s  %s/%s\n", marker, r.Component, r.Cloud, r.Region)
    }
    if res.Status != provisioner.ApplySucceeded { return 1 }
    return 0
}
```

Create `cmd/cli/drift.go` (similar):

```go
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
)

func newDriftCommand() *cobra.Command {
    var stack string
    cmd := &cobra.Command{
        Use:   "drift [path]",
        Short: "Detect drift between Tofu state and current cloud state",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            projectPath := "."
            if len(args) == 1 { projectPath = args[0] }
            reg := cloud.NewRegistry()
            if err := reg.Register(aws.New()); err != nil { return err }
            code := runDrift(cmd.Context(), driftArgs{
                ProjectPath: projectPath, Stack: stack,
                Adapters: reg, Runner: tofu.NewExecRunner(),
                Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
            })
            if code != 0 { os.Exit(code) }
            return nil
        },
        SilenceUsage: true, SilenceErrors: true,
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack (required)")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

type driftArgs struct {
    ProjectPath, Stack string
    Adapters           cloud.Registry
    Runner             tofu.Runner
    WorkRoot           string
    Stdout, Stderr     io.Writer
}

func runDrift(ctx context.Context, in driftArgs) int {
    if ctx == nil { ctx = context.Background() }
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack is required")
        return 2
    }
    project, err := loader.New().Load(ctx, in.ProjectPath)
    if err != nil { fmt.Fprintf(in.Stderr, "load: %v\n", err); return 1 }
    rep, err := validator.New().Validate(ctx, project)
    if err != nil { fmt.Fprintf(in.Stderr, "validator: %v\n", err); return 2 }
    if rep != nil && !rep.OK() {
        for _, i := range rep.Issues { fmt.Fprintln(in.Stderr, i.String()) }
        return 1
    }
    eng, err := engine.New(ctx, engine.Config{CloudAdapters: in.Adapters, TofuRunner: in.Runner, WorkRoot: in.WorkRoot})
    if err != nil { fmt.Fprintf(in.Stderr, "engine: %v\n", err); return 1 }
    plan, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
    if err != nil { fmt.Fprintf(in.Stderr, "plan: %v\n", err); return 1 }
    drift, err := eng.DetectDriftWithPlan(ctx, plan)
    if err != nil { fmt.Fprintf(in.Stderr, "drift: %v\n", err); return 1 }
    anyDrift := false
    for _, tr := range drift.TargetReports {
        marker := "="
        if tr.HasDrift { marker = "≠"; anyDrift = true }
        fmt.Fprintf(in.Stdout, "  %s %s  %s/%s  drift=%v\n", marker, tr.Component, tr.Cloud, tr.Region, tr.HasDrift)
        for _, d := range tr.Drifted {
            fmt.Fprintf(in.Stdout, "      ~ %s  %s\n", d.Address, d.DiffSummary)
        }
        for _, d := range tr.Gone {
            fmt.Fprintf(in.Stdout, "      - %s (gone)\n", d.Address)
        }
        for _, d := range tr.Discovered {
            fmt.Fprintf(in.Stdout, "      + %s (discovered)\n", d.Address)
        }
    }
    if anyDrift {
        fmt.Fprintln(in.Stdout, "\nDrift detected.")
    } else {
        fmt.Fprintln(in.Stdout, "\nNo drift.")
    }
    return 0
}
```

- [ ] **Step 2: Register commands in main.go**

In `cmd/cli/main.go`:

```go
root.AddCommand(newValidateCommand())
root.AddCommand(newPlanCommand())
root.AddCommand(newApplyCommand())
root.AddCommand(newDestroyCommand())
root.AddCommand(newDriftCommand())
```

- [ ] **Step 3: Write CLI test**

Create `cmd/cli/apply_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestApplyCommand_HappyPath(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.StateShowReturn = []byte(`{"format_version":"1.0","terraform_version":"1.7.0"}`)

    var stdout, stderr bytes.Buffer
    code := runApply(context.Background(), applyArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        AutoApprove: true,
        Adapters:    reg, Runner: runner, WorkRoot: t.TempDir(),
        Stdout: &stdout, Stderr: &stderr,
    })
    if code != 0 {
        t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
    }
    if !strings.Contains(stdout.String(), "Apply succeeded") {
        t.Errorf("stdout missing summary: %s", stdout.String())
    }
}

func TestDestroyCommand_HappyPath(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    var stdout, stderr bytes.Buffer
    code := runDestroy(context.Background(), destroyArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        AutoApprove: true,
        Adapters:    reg, Runner: tofu.NewFakeRunner(), WorkRoot: t.TempDir(),
        Stdout: &stdout, Stderr: &stderr,
    })
    if code != 0 {
        t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
    }
}

func TestDriftCommand_NoDrift(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    runner := tofu.NewFakeRunner()
    runner.DriftPlan = &tofu.PlanArtifact{JSONPlan: []byte(`{"resource_changes":[]}`), HasChanges: false}
    var stdout, stderr bytes.Buffer
    code := runDrift(context.Background(), driftArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        Adapters:    reg, Runner: runner, WorkRoot: t.TempDir(),
        Stdout: &stdout, Stderr: &stderr,
    })
    if code != 0 {
        t.Errorf("exit code = %d (stderr=%s)", code, stderr.String())
    }
    if !strings.Contains(stdout.String(), "No drift") {
        t.Errorf("stdout missing summary: %s", stdout.String())
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./cmd/cli/ -v
git add cmd/cli/
git commit -m "cli: nimbusfab apply/destroy/drift commands (Phase 2)"
```

---

## Task 12: README and CHANGELOG

**Files:**
- Modify: `README.md` (add new commands)
- Modify: `CHANGELOG.md` (add Phase 2 section)

- [ ] **Step 1: Update README**

Add to the Commands section:

```markdown
### `nimbusfab apply --stack <stack> [path]`

Validates → plans → applies. Phase 2 supports partial-failure policies via
`--partial-failure {leave|rollback|retry-failed}`. Apply still works
in-process (no inventory persistence yet); a future phase wires Apply against
a stored Plan ID.

### `nimbusfab destroy --stack <stack> [path]`

Tears down a stack by running `tofu destroy` per target in reverse topological
order of the components.

### `nimbusfab drift --stack <stack> [path]`

Runs `tofu plan -refresh-only` per target and reports per-resource drift.
```

Also update Status:

```markdown
**Status:** pre-alpha. Architecture spec landed; DSL/IR Phase 1, Provisioner
Phase 1, and Provisioner Phase 2 merged. Apply/Destroy/Drift work in-process
against the FakeRunner and via the real `tofu` binary; inventory persistence
is the next phase.
```

- [ ] **Step 2: Update CHANGELOG**

Add at the top:

```markdown
## Unreleased — Provisioner Phase 2

### Added

- `nimbusfab apply --stack <stack>` — validates → plans → applies with
  `--partial-failure {leave|rollback|retry-failed}` policy.
- `nimbusfab destroy --stack <stack>` — reverse-topo order tear-down.
- `nimbusfab drift --stack <stack>` — `tofu plan -refresh-only` per target.
- `pkg/provisioner` — Apply/Destroy/DetectDrift implementations.
- `pkg/provisioner/orchestrator.go` — component DAG topo sort, parallel
  target fan-out with three semaphores (global / per-cloud / per-credential),
  partial-failure policies (leave / rollback / retry-failed).
- `internal/state/bridge` — parses `tofu show -json` into `StateSnapshot`
  with deterministic per-resource attribute hash.
- `pkg/provisioner.RunEvent` — typed per-target event stream (consumed by
  CLI; web SSE wires later).
- Cross-component refs: `data.terraform_remote_state` block auto-injected
  into dependent workspaces.

### Changed

- `tofu.Runner.Plan` supports `PlanOpts.RefreshOnly` for drift detection.
- `pkg/engine` adds `ApplyWithPlan`, `DestroyWithPlan`, `DetectDriftWithPlan`
  surfaces. The interface `Apply(planID)` still returns
  `ErrNotImplementedYet` pending inventory persistence.
```

- [ ] **Step 3: Final verification**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH go vet ./...
gofmt -l . | head
```

All pass; vet clean; no formatting drift.

- [ ] **Step 4: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: README + CHANGELOG for Provisioner Phase 2"
```

---

## Final verification (matches Phase 1 pattern)

- [ ] **Run the full test suite:**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
```

- [ ] **Build the binary, exercise commands:**

```bash
PATH=$HOME/.local/go/bin:$PATH go build -o bin/nimbusfab ./cmd/cli
./bin/nimbusfab --help | grep -E "apply|destroy|drift"
```

- [ ] **End-to-end smoke (no real cloud):**

```bash
./bin/nimbusfab apply --stack dev --auto-approve cmd/cli/testdata/network-only-project 2>&1 | head -20
```

Expect: validates, plans, attempts apply, fails at `tofu init` (no binary) or at provider call (binary but no creds) — both acceptable. The CLI surface and orchestration code paths are exercised.

---

## What's NOT in Phase 2 (intentional)

- **Inventory persistence.** All Phase 2 runs stay per-process. The `deployments`, `deployment_targets`, `runs`, `run_logs`, `tofu_resources`, `drift_status` tables remain DDL-only stubs until the inventory phase.
- **Workspace caching.** Phase 2 always re-`tofu init`s. Caching is a measured optimization for a later phase.
- **`nimbusfab state {show,rm,mv,unlock}` CLI.** The Runner interface exposes these; CLI wraps them in a separate phase.
- **Web SSE streaming.** `RunEvent` produced; consumer is the web app phase.
- **AWS resource expansion** (subnets, route tables, database, compute, storage) — Phase 3.
- **Azure / GCP adapters** — Phases 4–5.
- **Cross-stack refs.** Phase 2 only supports same-stack cross-component refs.
- **Real cost / parity wiring against Apply results.** Cost estimator + parity engine are separate specs.

End-state: a user with `tofu` installed and AWS credentials can write a YAML project with one or more `network` components, run `nimbusfab apply --stack dev`, and have real VPCs provisioned. `destroy` tears them down. `drift` flags out-of-band changes. Partial failures across targets get the chosen policy. Component dependencies resolve via `data.terraform_remote_state`. The substrate for AWS resource expansion (Phase 3) and inventory persistence (separate phase) is in place.
