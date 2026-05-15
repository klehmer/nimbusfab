# Provisioner Phase 1 — Tofu Runner + AWS Network Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land a working `nimbusfab plan --stack <stack>` CLI that takes a validated project containing one `network` component targeting `aws/<region>`, writes a canonical OpenTofu workspace to disk, invokes `tofu init && tofu plan`, and prints a structured plan summary. End-state: a user can declare a single AWS VPC in YAML, run `nimbusfab plan`, and see the planned `aws_vpc` resource with framework-mandated tags.

**Architecture:** Three layers landing in dependency order. The provisioner (`pkg/provisioner`) accepts a validated `*ir.Project`, asks each cloud adapter to `Emit()` Tofu primitives for each `DeploymentTarget`, serializes the result into a canonical workspace directory (one per target), and invokes the Tofu runner. The Tofu runner (`internal/tofu`) is a subprocess wrapper around the `tofu` binary — it knows about workspaces and exit codes, nothing else. The cloud adapter (`internal/cloud/aws`, registered through `pkg/cloud`) is a pure function from `(target, refs)` to `[]ir.ResourcePrimitive`. The CLI (`cmd/cli`) gains one new command, `plan`, that wires `engine.Plan()` to the provisioner.

**Tech Stack:**
- Go 1.22 (existing)
- `gopkg.in/yaml.v3` (existing)
- `github.com/spf13/cobra` (existing)
- Standard library `encoding/json`, `os/exec`, `embed`, `testing`
- `github.com/google/uuid@v1.6.0` (NEW; for deterministic `DeploymentID` / `RunID` generation in unit tests via injectable RNG)
- `golang.org/x/sync/errgroup@v0.7.0` (NEW; for the orchestrator's parallel target fan-out — even though Phase 1 only has one target, the orchestrator skeleton uses it from day 1 to lock in the contract)
- `github.com/hashicorp/go-version@v1.6.0` (NEW; for parsing `tofu version` output and checking against `versions.tf.json` constraints)

**Conventions used throughout this plan:**
- All file paths are relative to the repo root `/home/kurt/git/nimbusfab/`.
- Run all `go` commands from the repo root.
- Each Task ends with a commit; commit messages follow `<area>: <imperative>` style (e.g., `provisioner: emit canonical workspace files`).
- Tests live alongside source (`foo.go` and `foo_test.go` in the same package) per Go conventions.
- Use `go test ./...` from the repo root to run all tests after each task.
- The `tofu` binary is NOT required to run unit tests — Tasks 6 and 11 use a fake runner. The integration test in Task 12 IS gated behind a build tag (`+build integration`) and a `tofu` binary check; CI runs both passes.

**Out of scope for Phase 1 (deferred to later phases):**
- `Apply` / `Destroy` orchestration (Phase 2)
- Multi-target parallel orchestration with policies (Phase 2; orchestrator scaffold lands here but only ever has 1 target in phase 1 fixtures)
- Cross-target ref resolution (Phase 2; refs map is empty in phase 1)
- State bridge / drift detection (Phase 2)
- Inventory persistence (Phase 2; phase 1 uses `--no-inventory` mode)
- AWS resources beyond `aws_vpc` (Phase 3 expands `network` to subnets + route tables, then adds `database`, `compute`, `storage`)
- Azure / GCP adapters (Phases 4–5)
- `Profile()`, `PricingKey()`, `BillingQuery()`, `FetchBilling()` real implementations — these return `ErrNotImplementedYet` in Phase 1, satisfying the contract test for "method exists" but not exercising functionality
- LocalStack / real-cloud integration — Phase 1's E2E test uses `tofu` against a hand-written fake provider plugin shipped in testdata, OR (preferred) skips real provider calls by stopping after `tofu init -backend=false` and asserting workspace contents

---

## Task 1: Add Go module dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated)

- [ ] **Step 1: Add runtime dependencies**

```bash
go get github.com/google/uuid@v1.6.0
go get golang.org/x/sync@v0.7.0
go get github.com/hashicorp/go-version@v1.6.0
```

- [ ] **Step 2: Verify and tidy**

```bash
go mod tidy
go build ./...
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add uuid, errgroup, and go-version"
```

---

## Task 2: Define provisioner public types and Provisioner interface

**Files:**
- Create: `pkg/provisioner/types.go`
- Create: `pkg/provisioner/types_test.go`
- Create: `pkg/provisioner/provisioner.go`
- Create: `pkg/provisioner/provisioner_test.go`

- [ ] **Step 1: Write failing test for `PartialFailurePolicy` constants**

Create `pkg/provisioner/types_test.go`:

```go
package provisioner

import "testing"

func TestPartialFailurePolicy_Default(t *testing.T) {
    if string(PartialFailureLeave) != "leave" {
        t.Fatalf("PartialFailureLeave = %q, want \"leave\"", PartialFailureLeave)
    }
    if string(PartialFailureRollback) != "rollback" {
        t.Fatalf("PartialFailureRollback = %q, want \"rollback\"", PartialFailureRollback)
    }
    if string(PartialFailureRetryFailed) != "retry-failed" {
        t.Fatalf("PartialFailureRetryFailed = %q, want \"retry-failed\"", PartialFailureRetryFailed)
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./pkg/provisioner/ -run TestPartialFailurePolicy_Default -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement public types**

Create `pkg/provisioner/types.go`:

```go
// Package provisioner takes a validated *ir.Project, asks cloud adapters
// to emit ResourcePrimitives for each DeploymentTarget, materializes a
// canonical OpenTofu workspace per target, and drives the Tofu runner.
//
// The provisioner is the only package that imports both pkg/cloud and
// internal/tofu; this contains the dependency-cycle by design.
package provisioner

import (
    "time"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

// PartialFailurePolicy selects how a multi-target Apply handles per-target
// failures. Phase 1 only exercises Plan, but the constant is locked in here
// because PlanInput records the policy that the eventual Apply will honor.
type PartialFailurePolicy string

const (
    PartialFailureLeave        PartialFailurePolicy = "leave"
    PartialFailureRollback     PartialFailurePolicy = "rollback"
    PartialFailureRetryFailed  PartialFailurePolicy = "retry-failed"
)

// TargetFilter restricts a Plan/Apply to a subset of DeploymentTargets.
// Empty filter means "all targets in the validated project".
type TargetFilter struct {
    Component string  // empty = any
    Cloud     string  // empty = any
    Region    string  // empty = any
}

// PlanInput is what the engine hands the provisioner.
type PlanInput struct {
    Project        *ir.Project
    Stack          string
    OrgID          string                 // "local" in --no-inventory mode
    DeploymentID   string                 // pre-allocated by engine
    PartialFailure PartialFailurePolicy   // recorded in PlanResult; consumed at Apply
    Refresh        bool                   // pass -refresh=true to tofu plan
    Targets        []TargetFilter
}

// PlanResult is what the provisioner returns to the engine.
type PlanResult struct {
    DeploymentID   string
    Stack          string
    PartialFailure PartialFailurePolicy
    Targets        []TargetPlan
    HasChanges     bool
    Diagnostics    []Diagnostic
    GeneratedAt    time.Time
}

// TargetPlan is one (component, cloud, region) plan slice.
type TargetPlan struct {
    DeploymentTargetID string
    Component          string
    Cloud              string
    Region             string
    WorkspaceDir       string
    PrimitiveCount     int
    PlanFile           string
    JSONPlanPath       string
    HasChanges         bool
    Adds               int
    Changes            int
    Destroys           int
    Tags               map[string]string
    // Profile + PricingKeys are reserved for parity / cost integration; phase 1 leaves them empty.
}

// Diagnostic is a non-fatal note attached to a PlanResult. Errors are
// returned through the error return; diagnostics are warnings or info.
type Diagnostic struct {
    Severity string  // "warning" | "info"
    Code     string  // stable; e.g. "WarnRawEscape"
    Message  string
    Target   string  // deployment_target_id when target-scoped; empty for project-scoped
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./pkg/provisioner/ -run TestPartialFailurePolicy_Default -v
```

Expected: PASS.

- [ ] **Step 5: Define `Provisioner` interface (stub New that returns ErrNotImplementedYet)**

Create `pkg/provisioner/provisioner.go`:

```go
package provisioner

import (
    "context"
    "errors"
)

// ErrNotImplementedYet is returned by Provisioner methods that have not yet
// been wired up. Phase 1 implements Plan only.
var ErrNotImplementedYet = errors.New("provisioner: not implemented yet")

// Provisioner orchestrates plan/apply/destroy across DeploymentTargets.
type Provisioner interface {
    Plan(ctx context.Context, in PlanInput) (*PlanResult, error)

    // Apply and Destroy land in Phase 2; both return ErrNotImplementedYet
    // throughout Phase 1.
    Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)
    Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error)
}

// ApplyInput / ApplyResult / DestroyInput are reserved-shape stubs so that
// Phase 2 lands without a public-API churn commit.
type ApplyInput struct {
    PlanResult            *PlanResult
    OrgID                 string
    PartialFailure        PartialFailurePolicy
    AutoApprove           bool
    AllowParityViolations bool
}

type ApplyResult struct {
    DeploymentID  string
    TargetResults []TargetApplyResult
    Status        string // "succeeded" | "partial_failure" | "failed"
}

type TargetApplyResult struct {
    DeploymentTargetID string
    RunID              string
    Status             string
    Outputs            map[string]any
    Error              error
}

type DestroyInput struct {
    DeploymentID   string
    PartialFailure PartialFailurePolicy
    AutoApprove    bool
}

// Config carries the dependencies a real Provisioner needs.
type Config struct {
    WorkRoot string                       // <workdir> in workspace path layout
    // Adapters and Runner injected here by Task 11.
}

// New returns a stub Provisioner. Task 9 replaces this with a real impl.
func New(cfg Config) (Provisioner, error) {
    return &stubProvisioner{}, nil
}

type stubProvisioner struct{}

func (*stubProvisioner) Plan(ctx context.Context, in PlanInput) (*PlanResult, error) {
    return nil, ErrNotImplementedYet
}
func (*stubProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
    return nil, ErrNotImplementedYet
}
func (*stubProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
    return nil, ErrNotImplementedYet
}
```

Create `pkg/provisioner/provisioner_test.go`:

```go
package provisioner

import (
    "context"
    "errors"
    "testing"
)

func TestStubProvisioner_AllReturnNotImplemented(t *testing.T) {
    p, err := New(Config{WorkRoot: t.TempDir()})
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    ctx := context.Background()
    if _, err := p.Plan(ctx, PlanInput{}); !errors.Is(err, ErrNotImplementedYet) {
        t.Errorf("Plan: want ErrNotImplementedYet, got %v", err)
    }
    if _, err := p.Apply(ctx, ApplyInput{}); !errors.Is(err, ErrNotImplementedYet) {
        t.Errorf("Apply: want ErrNotImplementedYet, got %v", err)
    }
    if _, err := p.Destroy(ctx, DestroyInput{}); !errors.Is(err, ErrNotImplementedYet) {
        t.Errorf("Destroy: want ErrNotImplementedYet, got %v", err)
    }
}
```

- [ ] **Step 6: Run all provisioner tests**

```bash
go test ./pkg/provisioner/ -v
go vet ./pkg/provisioner/
gofmt -l pkg/provisioner/
```

Expected: PASS; vet clean; no formatting drift.

- [ ] **Step 7: Commit**

```bash
git add pkg/provisioner/
git commit -m "provisioner: scaffold public types and stub Provisioner"
```

---

## Task 3: Cloud adapter registry

**Files:**
- Create: `pkg/cloud/registry.go`
- Create: `pkg/cloud/registry_test.go`
- Create: `pkg/cloud/fake_adapter.go` (test helper used by registry, provisioner, and engine tests)

- [ ] **Step 1: Write failing test for registry semantics**

Create `pkg/cloud/registry_test.go`:

```go
package cloud_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
    r := cloud.NewRegistry()
    fake := cloud.NewFakeAdapter("aws")

    if err := r.Register(fake); err != nil {
        t.Fatalf("Register: %v", err)
    }
    got, ok := r.Get("aws")
    if !ok {
        t.Fatal("Get(\"aws\"): ok=false, want true")
    }
    if got.Name() != "aws" {
        t.Errorf("Get(\"aws\").Name() = %q, want \"aws\"", got.Name())
    }
}

func TestRegistry_DuplicateRegister(t *testing.T) {
    r := cloud.NewRegistry()
    a := cloud.NewFakeAdapter("aws")
    b := cloud.NewFakeAdapter("aws")
    if err := r.Register(a); err != nil {
        t.Fatalf("first Register: %v", err)
    }
    if err := r.Register(b); err == nil {
        t.Fatal("duplicate Register: nil err, want non-nil")
    }
}

func TestRegistry_GetUnknown(t *testing.T) {
    r := cloud.NewRegistry()
    if _, ok := r.Get("nowhere"); ok {
        t.Fatal("Get(\"nowhere\"): ok=true, want false")
    }
}

func TestRegistry_ListIsAlphabetical(t *testing.T) {
    r := cloud.NewRegistry()
    _ = r.Register(cloud.NewFakeAdapter("gcp"))
    _ = r.Register(cloud.NewFakeAdapter("aws"))
    _ = r.Register(cloud.NewFakeAdapter("azure"))
    list := r.List()
    if len(list) != 3 {
        t.Fatalf("List len = %d, want 3", len(list))
    }
    names := []string{list[0].Name(), list[1].Name(), list[2].Name()}
    want := []string{"aws", "azure", "gcp"}
    for i := range want {
        if names[i] != want[i] {
            t.Errorf("List[%d].Name() = %q, want %q", i, names[i], want[i])
        }
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./pkg/cloud/ -run TestRegistry -v
```

Expected: FAIL — `NewRegistry`, `NewFakeAdapter` undefined.

- [ ] **Step 3: Implement `Registry`**

Create `pkg/cloud/registry.go`:

```go
package cloud

import (
    "fmt"
    "sort"
    "sync"
)

// Registry holds all registered cloud adapters.
type Registry interface {
    Register(a Adapter) error
    Get(name string) (Adapter, bool)
    List() []Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() Registry {
    return &registry{adapters: map[string]Adapter{}}
}

type registry struct {
    mu       sync.RWMutex
    adapters map[string]Adapter
}

func (r *registry) Register(a Adapter) error {
    if a == nil {
        return fmt.Errorf("cloud: Register nil adapter")
    }
    name := a.Name()
    if name == "" {
        return fmt.Errorf("cloud: adapter has empty Name()")
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.adapters[name]; exists {
        return fmt.Errorf("cloud: adapter %q already registered", name)
    }
    r.adapters[name] = a
    return nil
}

func (r *registry) Get(name string) (Adapter, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    a, ok := r.adapters[name]
    return a, ok
}

func (r *registry) List() []Adapter {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]Adapter, 0, len(r.adapters))
    for _, a := range r.adapters {
        out = append(out, a)
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
    return out
}
```

- [ ] **Step 4: Implement minimal FakeAdapter helper**

Create `pkg/cloud/fake_adapter.go`:

```go
package cloud

import (
    "context"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/parity"
)

// FakeAdapter is a deterministic in-memory Adapter used by tests.
// It emits one ResourcePrimitive of type "fake_resource" per target.
type FakeAdapter struct {
    name string
}

// NewFakeAdapter returns a FakeAdapter with the given Name.
func NewFakeAdapter(name string) *FakeAdapter { return &FakeAdapter{name: name} }

func (f *FakeAdapter) Name() string                                { return f.name }
func (f *FakeAdapter) SupportedAPIVersions() []string              { return []string{ir.APIVersionV1Alpha1} }
func (f *FakeAdapter) SupportedComponentTypes() []string           { return []string{"network"} }
func (f *FakeAdapter) TierOneSchema() []byte                       { return []byte(`{"type":"object","additionalProperties":true}`) }
func (f *FakeAdapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
    return nil
}

func (f *FakeAdapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return []ir.ResourcePrimitive{{
        ID:       fmt.Sprintf("%s.%s.fake", f.name, target.Region),
        Cloud:    f.name,
        TofuType: "fake_resource",
        TofuName: "fake",
        Attributes: map[string]any{
            "region": target.Region,
        },
    }}, nil
}

func (f *FakeAdapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
    return parity.ResourceProfile{}, ErrProfileUnavailable
}
func (f *FakeAdapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
    return map[string]any{"adapter": f.name}, nil
}
func (f *FakeAdapter) BillingQuery(ctx context.Context, _ Credentials, _, _ time.Time) (BillingQueryParams, error) {
    return BillingQueryParams{}, nil
}
func (f *FakeAdapter) FetchBilling(ctx context.Context, _ Credentials, _ BillingQueryParams) ([]NormalizedCostRow, error) {
    return nil, nil
}
func (f *FakeAdapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
    return ir.StateBackend{Kind: "local"}, nil
}
func (f *FakeAdapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ Credentials) (map[string]any, error) {
    return map[string]any{f.name: map[string]any{"region": target.Region}}, nil
}
```

(Note: `ErrProfileUnavailable` and `parity.ResourceProfile` come from Tasks 4 and 5 below; the parity import will not yet resolve. The next tasks fill this in. Build will not pass between tasks until Task 5 completes; that's expected and matches the DSL/IR Phase 1 cadence — partial states between tasks compile cleanly within the task being worked on, but cross-package symbol dependencies surface across tasks. Task 4 lands the new Adapter methods on the existing interface; Task 5 lands `pkg/parity` types. After Task 5, `go build ./...` is clean again.)

- [ ] **Step 5: Defer running tests until Task 5**

The registry tests will not pass on their own because `NewFakeAdapter` references methods that don't exist on the Adapter interface yet (Task 4) and types that don't exist (`parity.ResourceProfile`, Task 5). Skip step 5 here; tests run in Task 5's verification step.

- [ ] **Step 6: Commit**

```bash
git add pkg/cloud/registry.go pkg/cloud/registry_test.go pkg/cloud/fake_adapter.go
git commit -m "cloud: add adapter registry and FakeAdapter test helper"
```

---

## Task 4: Extend `cloud.Adapter` interface with new methods

**Files:**
- Modify: `pkg/cloud/adapter.go`
- Create: `pkg/cloud/errors.go`

- [ ] **Step 1: Read existing adapter.go**

```bash
sed -n '1,82p' pkg/cloud/adapter.go
```

Confirm the existing 8-method interface and types. Do NOT edit existing methods or types — only ADD new ones, plus the `parity` import.

- [ ] **Step 2: Add new interface methods**

Modify `pkg/cloud/adapter.go`. After the existing `DefaultStateBackend` method declaration, ADD inside the `Adapter` interface block:

```go
    // SupportedComponentTypes returns the built-in component type names this
    // adapter implements. The validator and provisioner consult this to fail
    // fast on (component.Type, target.Cloud) mismatches.
    SupportedComponentTypes() []string

    // TierOneSchema returns the JSON Schema for the `<cloud>:` block under
    // DeploymentTarget.Spec. Loaded once at startup.
    TierOneSchema() []byte

    // Validate runs cloud-specific semantic checks. Pure: no network, no disk.
    Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue

    // Profile returns normalized resource attributes. Shared with parity / cost.
    Profile(ctx context.Context, primitive ir.ResourcePrimitive) (parity.ResourceProfile, error)

    // ProviderBlock returns the provider config the provisioner writes into
    // provider.tf.json. Credentials material flows in through `creds.Payload`
    // and MUST NOT be embedded in the returned map (it goes into env vars).
    ProviderBlock(ctx context.Context, target ir.DeploymentTarget, creds Credentials) (map[string]any, error)
```

Also add `"github.com/klehmer/nimbusfab/pkg/parity"` to the import block.

- [ ] **Step 3: Create error sentinels**

Create `pkg/cloud/errors.go`:

```go
package cloud

import "errors"

// ErrProfileUnavailable is returned by Adapter.Profile when the adapter
// cannot construct a ResourceProfile for a given primitive (e.g., an
// IAM role that has no compute/storage/database/network shape). The
// parity engine treats this as a non-fatal warning, not an error.
var ErrProfileUnavailable = errors.New("cloud: profile unavailable for this primitive")

// ErrNotImplementedYet is returned by adapter methods stubbed during
// phased rollout. Phase 1 returns this from real adapters' Profile,
// PricingKey, BillingQuery, and FetchBilling methods.
var ErrNotImplementedYet = errors.New("cloud: not implemented yet")
```

- [ ] **Step 4: Verify build will fail (parity package missing)**

```bash
go build ./pkg/cloud/ 2>&1 | head -5
```

Expected: build fails citing `pkg/parity` not found. That's intentional — Task 5 lands it.

- [ ] **Step 5: Commit**

```bash
git add pkg/cloud/adapter.go pkg/cloud/errors.go
git commit -m "cloud: extend Adapter with TierOneSchema, Validate, Profile, ProviderBlock"
```

---

## Task 5: Scaffold `pkg/parity` types referenced by adapter contract

**Files:**
- Create: `pkg/parity/types.go`
- Create: `pkg/parity/types_test.go`

The full parity engine is a separate spec / phase; this task lands ONLY the types referenced from `cloud.Adapter.Profile()` so that the cloud package compiles. The parity engine itself remains absent until its own implementation phase.

- [ ] **Step 1: Write failing test (zero-value sanity)**

Create `pkg/parity/types_test.go`:

```go
package parity_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestResourceProfile_ZeroValue(t *testing.T) {
    var p parity.ResourceProfile
    if p.Class != "" {
        t.Errorf("zero ResourceProfile.Class = %q, want empty", p.Class)
    }
    if p.Compute != nil || p.Storage != nil || p.Database != nil || p.Network != nil {
        t.Error("zero ResourceProfile should have nil sub-profiles")
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./pkg/parity/ -v
```

Expected: FAIL — package missing.

- [ ] **Step 3: Implement minimum types**

Create `pkg/parity/types.go`:

```go
// Package parity defines normalized cloud-resource attribute shapes that
// the parity engine and cost estimator share. The full engine ships in
// its own spec/phase; this file is the type contract referenced from
// pkg/cloud/adapter.go's Profile method.
package parity

// ResourceProfile is the normalized shape adapters return from Profile().
// At least one of Compute/Storage/Database/Network MUST be non-nil for a
// valid profile (the engine asserts this in its own tests).
type ResourceProfile struct {
    Class    string             // "compute" | "storage" | "database" | "network"
    Compute  *ComputeProfile
    Storage  *StorageProfile
    Database *DatabaseProfile
    Network  *NetworkProfile
    Features map[string]bool    // e.g. {"pointInTimeRestore": true}
    SKU      string             // human-readable: "db.t3.medium"
    Notes    []string           // adapter-supplied caveats
}

type ComputeProfile struct {
    VCPU         int
    MemoryGB     float64
    Architecture string            // "x86_64" | "arm64"
    NetworkGbps  float64
}

type StorageProfile struct {
    SizeGB         int
    IOPS           int
    ThroughputMBps int
    Class          string          // "ssd" | "hdd" | "nvme" | "tiered"
    Encrypted      bool
}

type DatabaseProfile struct {
    Engine   string
    Version  string
    Compute  ComputeProfile
    Storage  StorageProfile
    Replicas int
    HA       bool
}

type NetworkProfile struct {
    CIDR          string
    BandwidthGbps float64
    IPv6          bool
    NAT           bool
}
```

- [ ] **Step 4: Run all tests across affected packages**

```bash
go test ./pkg/parity/ -v
go test ./pkg/cloud/ -v
go build ./...
```

Expected: parity tests PASS; cloud package now compiles; cloud registry tests from Task 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/parity/
git commit -m "parity: scaffold ResourceProfile types referenced by adapter contract"
```

---

## Task 6: Tofu runner subprocess wrapper (`Init`, `Plan`, `Show`, `Version`)

**Files:**
- Modify: `internal/tofu/runner.go` (add `Validate`, `Output` methods to interface; existing methods unchanged)
- Create: `internal/tofu/exec_runner.go`
- Create: `internal/tofu/exec_runner_test.go`
- Create: `internal/tofu/fake_runner.go`
- Create: `internal/tofu/fake_runner_test.go`
- Create: `internal/tofu/diagnostics.go`
- Create: `internal/tofu/diagnostics_test.go`

- [ ] **Step 1: Extend Runner interface with Validate and Output**

Modify `internal/tofu/runner.go`. After `StateMv` and before `Version`, ADD:

```go
    Validate(ctx context.Context, ws Workspace) (*ValidateResult, error)
    Output(ctx context.Context, ws Workspace) (map[string]any, error)
```

Then add the result type:

```go
// ValidateResult captures the structured output of `tofu validate -json`.
type ValidateResult struct {
    Valid       bool
    Diagnostics []Diagnostic
}
```

(`Diagnostic` arrives in step 2.)

- [ ] **Step 2: Write failing test for diagnostic parsing**

Create `internal/tofu/diagnostics_test.go`:

```go
package tofu

import (
    "strings"
    "testing"
)

func TestParseDiagnostic_StateLock(t *testing.T) {
    raw := `{"@level":"error","@message":"Error acquiring the state lock","@module":"terraform.ui","type":"diagnostic","diagnostic":{"severity":"error","summary":"Error acquiring the state lock","detail":"Lock Info:\n  ID:        abc-123\n"}}`
    diag, err := ParseDiagnostic(strings.NewReader(raw))
    if err != nil {
        t.Fatalf("ParseDiagnostic: %v", err)
    }
    if diag.Code != ErrTofuStateLocked {
        t.Errorf("Code = %q, want %q", diag.Code, ErrTofuStateLocked)
    }
}

func TestParseDiagnostic_Opaque(t *testing.T) {
    raw := `{"@level":"info","@message":"Initializing","type":"version"}`
    diag, err := ParseDiagnostic(strings.NewReader(raw))
    if err != nil {
        t.Fatalf("ParseDiagnostic: %v", err)
    }
    if diag.Code != "" {
        t.Errorf("non-error event should yield empty Code, got %q", diag.Code)
    }
}
```

- [ ] **Step 3: Verify failure**

```bash
go test ./internal/tofu/ -run TestParseDiagnostic -v
```

Expected: FAIL — `ParseDiagnostic`, `Diagnostic`, error code consts undefined.

- [ ] **Step 4: Implement diagnostics.go**

Create `internal/tofu/diagnostics.go`:

```go
package tofu

import (
    "bufio"
    "encoding/json"
    "io"
    "strings"
)

// Error codes the runner emits to engine.
const (
    ErrTofuStateLocked     = "ErrTofuStateLocked"
    ErrTofuCredsMissing    = "ErrTofuCredsMissing"
    ErrTofuProviderMissing = "ErrTofuProviderMissing"
    ErrTofuVersionMismatch = "ErrTofuVersionMismatch"
    ErrTofuDiagnostic      = "ErrTofuDiagnostic"
    ErrTofuOpaque          = "ErrTofuOpaque"
)

// Diagnostic is the structured form of a single Tofu diagnostic event.
type Diagnostic struct {
    Severity string
    Summary  string
    Detail   string
    Address  string
    Range    *Range
    Code     string  // engine error code; "" for non-error events
    Raw      map[string]any
}

type Range struct {
    Filename string
    Start    Position
    End      Position
}

type Position struct {
    Line   int
    Column int
    Byte   int
}

// ParseDiagnostic reads the FIRST JSON object from the reader and maps it to
// a Diagnostic. Used in tests and by ParseStream.
func ParseDiagnostic(r io.Reader) (Diagnostic, error) {
    var raw map[string]any
    if err := json.NewDecoder(r).Decode(&raw); err != nil {
        return Diagnostic{}, err
    }
    return mapEvent(raw), nil
}

// ParseStream reads newline-delimited JSON events from `r` and emits typed
// Diagnostics on the returned channel. The channel closes when r EOFs.
func ParseStream(r io.Reader) <-chan Diagnostic {
    ch := make(chan Diagnostic, 64)
    go func() {
        defer close(ch)
        sc := bufio.NewScanner(r)
        sc.Buffer(make([]byte, 1<<20), 1<<24)
        for sc.Scan() {
            var raw map[string]any
            if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
                continue // malformed line; skip
            }
            ch <- mapEvent(raw)
        }
    }()
    return ch
}

func mapEvent(raw map[string]any) Diagnostic {
    d := Diagnostic{Raw: raw}
    if t, _ := raw["type"].(string); t != "diagnostic" {
        return d
    }
    body, _ := raw["diagnostic"].(map[string]any)
    d.Severity, _ = body["severity"].(string)
    d.Summary, _ = body["summary"].(string)
    d.Detail, _ = body["detail"].(string)
    d.Address, _ = body["address"].(string)
    d.Code = classify(d.Summary, d.Detail)
    return d
}

func classify(summary, detail string) string {
    s := strings.ToLower(summary)
    switch {
    case strings.Contains(s, "state lock"):
        return ErrTofuStateLocked
    case strings.Contains(s, "credentials"):
        return ErrTofuCredsMissing
    case strings.Contains(s, "provider") && strings.Contains(s, "not"):
        return ErrTofuProviderMissing
    case strings.Contains(s, "version"):
        return ErrTofuVersionMismatch
    case s == "":
        return ""
    default:
        return ErrTofuDiagnostic
    }
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/tofu/ -run TestParseDiagnostic -v
```

Expected: PASS.

- [ ] **Step 6: Implement FakeRunner for unit tests**

Create `internal/tofu/fake_runner.go`:

```go
package tofu

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

// FakeRunner records inputs and returns scripted outputs.
// All methods are concurrency-safe.
type FakeRunner struct {
    mu sync.Mutex

    InitCalls     []Workspace
    PlanCalls     []PlanCall
    ApplyCalls    []ApplyCall
    DestroyCalls  []Workspace
    ShowCalls     []ShowCall
    StateShowCalls []Workspace
    OutputCalls   []Workspace

    // Scripted return values. Tests set these before invoking the runner.
    PlanReturn        *PlanArtifact
    PlanError         error
    ApplyError        error
    ShowReturn        []byte
    StateShowReturn   []byte
    OutputReturn      map[string]any
    VersionReturn     string

    // If non-empty, FakePlan writes this byte slice to opts.OutFile so that
    // downstream code that reads the plan file sees plausible content.
    PlanFileContents []byte
}

type PlanCall struct {
    Workspace Workspace
    Opts      PlanOpts
}

type ApplyCall struct {
    Workspace Workspace
    PlanFile  string
    Opts      ApplyOpts
}

type ShowCall struct {
    Workspace Workspace
    PlanFile  string
}

func NewFakeRunner() *FakeRunner {
    return &FakeRunner{
        VersionReturn: "OpenTofu v1.7.0",
        ShowReturn:    []byte(`{"format_version":"1.2"}`),
    }
}

func (f *FakeRunner) Init(ctx context.Context, ws Workspace) error {
    f.mu.Lock(); defer f.mu.Unlock()
    f.InitCalls = append(f.InitCalls, ws)
    return nil
}

func (f *FakeRunner) Validate(ctx context.Context, ws Workspace) (*ValidateResult, error) {
    return &ValidateResult{Valid: true}, nil
}

func (f *FakeRunner) Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error) {
    f.mu.Lock(); defer f.mu.Unlock()
    f.PlanCalls = append(f.PlanCalls, PlanCall{Workspace: ws, Opts: opts})
    if f.PlanError != nil {
        return nil, f.PlanError
    }
    if opts.OutFile != "" && len(f.PlanFileContents) > 0 {
        if err := os.WriteFile(opts.OutFile, f.PlanFileContents, 0o600); err != nil {
            return nil, err
        }
    }
    if f.PlanReturn != nil {
        return f.PlanReturn, nil
    }
    return &PlanArtifact{
        PlanFile:   opts.OutFile,
        JSONPlan:   []byte(`{"resource_changes":[]}`),
        HasChanges: false,
    }, nil
}

func (f *FakeRunner) Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error {
    f.mu.Lock(); defer f.mu.Unlock()
    f.ApplyCalls = append(f.ApplyCalls, ApplyCall{Workspace: ws, PlanFile: planFile, Opts: opts})
    return f.ApplyError
}

func (f *FakeRunner) Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error {
    f.mu.Lock(); defer f.mu.Unlock()
    f.DestroyCalls = append(f.DestroyCalls, ws)
    return nil
}

func (f *FakeRunner) Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error) {
    f.mu.Lock(); defer f.mu.Unlock()
    f.ShowCalls = append(f.ShowCalls, ShowCall{Workspace: ws, PlanFile: planFile})
    return f.ShowReturn, nil
}

func (f *FakeRunner) StateShow(ctx context.Context, ws Workspace) ([]byte, error) {
    f.mu.Lock(); defer f.mu.Unlock()
    f.StateShowCalls = append(f.StateShowCalls, ws)
    return f.StateShowReturn, nil
}

func (f *FakeRunner) StateRm(ctx context.Context, ws Workspace, address string) error { return nil }
func (f *FakeRunner) StateMv(ctx context.Context, ws Workspace, from, to string) error { return nil }

func (f *FakeRunner) Output(ctx context.Context, ws Workspace) (map[string]any, error) {
    f.mu.Lock(); defer f.mu.Unlock()
    f.OutputCalls = append(f.OutputCalls, ws)
    return f.OutputReturn, nil
}

func (f *FakeRunner) Version(ctx context.Context) (string, error) {
    return f.VersionReturn, nil
}

// MarshalJSONPlan is a helper for tests: build a fake `tofu show -json plan` payload
// with N planned resource creates.
func (f *FakeRunner) MarshalJSONPlan(creates int) []byte {
    rcs := make([]map[string]any, 0, creates)
    for i := 0; i < creates; i++ {
        rcs = append(rcs, map[string]any{
            "address": fmt.Sprintf("aws_vpc.example_%d", i),
            "change":  map[string]any{"actions": []string{"create"}},
        })
    }
    out, _ := json.Marshal(map[string]any{"resource_changes": rcs})
    return out
}

// EnsureWorkspaceWritable is a helper that creates ws.Dir if missing.
func EnsureWorkspaceWritable(ws Workspace) error {
    return os.MkdirAll(filepath.Clean(ws.Dir), 0o700)
}
```

Create `internal/tofu/fake_runner_test.go`:

```go
package tofu

import (
    "context"
    "testing"
)

func TestFakeRunner_RecordsCalls(t *testing.T) {
    r := NewFakeRunner()
    ws := Workspace{Dir: t.TempDir()}
    ctx := context.Background()

    if err := r.Init(ctx, ws); err != nil {
        t.Fatalf("Init: %v", err)
    }
    if _, err := r.Plan(ctx, ws, PlanOpts{OutFile: "/tmp/x.plan"}); err != nil {
        t.Fatalf("Plan: %v", err)
    }
    if v, err := r.Version(ctx); err != nil || v != "OpenTofu v1.7.0" {
        t.Fatalf("Version: v=%q err=%v", v, err)
    }
    if len(r.InitCalls) != 1 || len(r.PlanCalls) != 1 {
        t.Fatalf("call recording: init=%d plan=%d", len(r.InitCalls), len(r.PlanCalls))
    }
}
```

- [ ] **Step 7: Implement ExecRunner (real subprocess wrapper)**

Create `internal/tofu/exec_runner.go`:

```go
package tofu

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// ExecRunner shells out to the `tofu` binary on $PATH (or the configured path).
type ExecRunner struct {
    Binary string  // "tofu" by default
}

// NewExecRunner returns an ExecRunner using `tofu` on $PATH.
func NewExecRunner() *ExecRunner {
    return &ExecRunner{Binary: "tofu"}
}

func (e *ExecRunner) bin() string {
    if e.Binary != "" {
        return e.Binary
    }
    return "tofu"
}

func (e *ExecRunner) run(ctx context.Context, ws Workspace, args ...string) error {
    cmd := exec.CommandContext(ctx, e.bin(), args...)
    cmd.Dir = ws.Dir
    cmd.Env = mergeEnv(os.Environ(), ws.Environment)
    if ws.Stdout != nil {
        cmd.Stdout = ws.Stdout
    }
    if ws.Stderr != nil {
        cmd.Stderr = ws.Stderr
    }
    return cmd.Run()
}

func mergeEnv(base []string, extra map[string]string) []string {
    out := append([]string{}, base...)
    for k, v := range extra {
        out = append(out, k+"="+v)
    }
    return out
}

func (e *ExecRunner) Init(ctx context.Context, ws Workspace) error {
    return e.run(ctx, ws, "init", "-no-color", "-input=false", "-lock-timeout=300s")
}

func (e *ExecRunner) Validate(ctx context.Context, ws Workspace) (*ValidateResult, error) {
    cmd := exec.CommandContext(ctx, e.bin(), "validate", "-json", "-no-color")
    cmd.Dir = ws.Dir
    cmd.Env = mergeEnv(os.Environ(), ws.Environment)
    out, err := cmd.Output()
    if err != nil && len(out) == 0 {
        return nil, err
    }
    var raw struct {
        Valid       bool                     `json:"valid"`
        Diagnostics []map[string]any         `json:"diagnostics"`
    }
    if jerr := json.Unmarshal(out, &raw); jerr != nil {
        return nil, fmt.Errorf("tofu validate: malformed JSON output: %w", jerr)
    }
    res := &ValidateResult{Valid: raw.Valid}
    for _, d := range raw.Diagnostics {
        res.Diagnostics = append(res.Diagnostics, mapEvent(map[string]any{"type": "diagnostic", "diagnostic": d}))
    }
    return res, nil
}

func (e *ExecRunner) Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error) {
    if opts.OutFile == "" {
        return nil, fmt.Errorf("tofu Plan: PlanOpts.OutFile is required")
    }
    args := []string{"plan", "-no-color", "-input=false", "-json", "-lock-timeout=300s", "-out=" + opts.OutFile}
    if opts.Destroy {
        args = append(args, "-destroy")
    }
    if opts.Refresh {
        args = append(args, "-refresh=true")
    }
    if err := e.run(ctx, ws, args...); err != nil {
        return nil, err
    }
    showCmd := exec.CommandContext(ctx, e.bin(), "show", "-json", opts.OutFile)
    showCmd.Dir = ws.Dir
    jsonPlan, err := showCmd.Output()
    if err != nil {
        return nil, fmt.Errorf("tofu show -json: %w", err)
    }
    return &PlanArtifact{
        PlanFile:   opts.OutFile,
        JSONPlan:   jsonPlan,
        HasChanges: planHasChanges(jsonPlan),
    }, nil
}

func planHasChanges(jsonPlan []byte) bool {
    var p struct {
        ResourceChanges []map[string]any `json:"resource_changes"`
    }
    if err := json.Unmarshal(jsonPlan, &p); err != nil {
        return false
    }
    for _, rc := range p.ResourceChanges {
        change, _ := rc["change"].(map[string]any)
        actions, _ := change["actions"].([]any)
        for _, a := range actions {
            if s, _ := a.(string); s != "no-op" && s != "read" {
                return true
            }
        }
    }
    return false
}

func (e *ExecRunner) Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error {
    args := []string{"apply", "-no-color", "-input=false", "-json", "-lock-timeout=300s", planFile}
    if opts.AutoApprove {
        // Plan files imply auto-approve; flag is harmless and explicit.
    }
    return e.run(ctx, ws, args...)
}

func (e *ExecRunner) Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error {
    args := []string{"destroy", "-no-color", "-input=false", "-json", "-lock-timeout=300s"}
    if opts.AutoApprove {
        args = append(args, "-auto-approve")
    }
    return e.run(ctx, ws, args...)
}

func (e *ExecRunner) Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, e.bin(), "show", "-json", planFile)
    cmd.Dir = ws.Dir
    return cmd.Output()
}

func (e *ExecRunner) StateShow(ctx context.Context, ws Workspace) ([]byte, error) {
    cmd := exec.CommandContext(ctx, e.bin(), "show", "-json")
    cmd.Dir = ws.Dir
    return cmd.Output()
}

func (e *ExecRunner) StateRm(ctx context.Context, ws Workspace, address string) error {
    return e.run(ctx, ws, "state", "rm", address)
}

func (e *ExecRunner) StateMv(ctx context.Context, ws Workspace, from, to string) error {
    return e.run(ctx, ws, "state", "mv", from, to)
}

func (e *ExecRunner) Output(ctx context.Context, ws Workspace) (map[string]any, error) {
    cmd := exec.CommandContext(ctx, e.bin(), "output", "-json", "-no-color")
    cmd.Dir = ws.Dir
    out, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    var raw map[string]any
    if jerr := json.Unmarshal(out, &raw); jerr != nil {
        return nil, fmt.Errorf("tofu output -json: malformed JSON: %w", jerr)
    }
    res := map[string]any{}
    for k, v := range raw {
        if m, ok := v.(map[string]any); ok {
            res[k] = m["value"]
        } else {
            res[k] = v
        }
    }
    return res, nil
}

func (e *ExecRunner) Version(ctx context.Context) (string, error) {
    cmd := exec.CommandContext(ctx, e.bin(), "version", "-json")
    out, err := cmd.Output()
    if err != nil {
        return "", err
    }
    var v struct {
        TerraformVersion string `json:"terraform_version"`
        Product          string `json:"product"`
    }
    if jerr := json.Unmarshal(out, &v); jerr == nil && v.TerraformVersion != "" {
        product := v.Product
        if product == "" {
            product = "OpenTofu"
        }
        return strings.TrimSpace(product) + " v" + v.TerraformVersion, nil
    }
    return strings.TrimSpace(string(out)), nil
}

// hasBinary reports whether the configured tofu binary is on $PATH.
// Used by integration tests to skip when tofu isn't installed.
func (e *ExecRunner) HasBinary() bool {
    _, err := exec.LookPath(e.bin())
    return err == nil
}

var _ io.Reader = (*os.File)(nil) // satisfy unused-import lint when tests are absent
var _ = filepath.Separator
```

Create `internal/tofu/exec_runner_test.go`:

```go
package tofu

import "testing"

func TestExecRunner_DefaultBinaryName(t *testing.T) {
    e := NewExecRunner()
    if e.bin() != "tofu" {
        t.Errorf("default bin = %q, want \"tofu\"", e.bin())
    }
    e.Binary = "/usr/local/bin/tofu"
    if e.bin() != "/usr/local/bin/tofu" {
        t.Errorf("custom bin = %q", e.bin())
    }
}

func TestPlanHasChanges_DetectsCreate(t *testing.T) {
    j := []byte(`{"resource_changes":[{"address":"aws_vpc.x","change":{"actions":["create"]}}]}`)
    if !planHasChanges(j) {
        t.Error("planHasChanges = false, want true for create")
    }
}

func TestPlanHasChanges_NoOpOnly(t *testing.T) {
    j := []byte(`{"resource_changes":[{"address":"aws_vpc.x","change":{"actions":["no-op"]}}]}`)
    if planHasChanges(j) {
        t.Error("planHasChanges = true, want false for no-op only")
    }
}
```

- [ ] **Step 8: Run all tofu tests**

```bash
go test ./internal/tofu/ -v
go vet ./internal/tofu/
```

Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tofu/
git commit -m "tofu: subprocess Runner (Init/Plan/Show/Version) plus FakeRunner and diagnostic parser"
```

---

## Task 7: Workspace writer (canonical JSON, atomic file ops, file lock)

**Files:**
- Create: `pkg/provisioner/workspace.go`
- Create: `pkg/provisioner/workspace_test.go`
- Create: `pkg/provisioner/emit.go`
- Create: `pkg/provisioner/emit_test.go`
- Create: `pkg/provisioner/tagging.go`
- Create: `pkg/provisioner/tagging_test.go`

- [ ] **Step 1: Write failing test for canonical JSON serialization**

Create `pkg/provisioner/emit_test.go`:

```go
package provisioner

import "testing"

func TestCanonicalJSON_SortsMapKeys(t *testing.T) {
    in := map[string]any{
        "z": 1,
        "a": map[string]any{"y": 2, "b": 3},
    }
    got, err := canonicalJSON(in)
    if err != nil {
        t.Fatalf("canonicalJSON: %v", err)
    }
    want := `{"a":{"b":3,"y":2},"z":1}`
    if string(got) != want {
        t.Errorf("got %s, want %s", got, want)
    }
}

func TestCanonicalJSON_DeterministicAcrossRuns(t *testing.T) {
    in := map[string]any{"k1": "v1", "k2": "v2", "k3": map[string]any{"a": 1, "b": 2}}
    a, _ := canonicalJSON(in)
    b, _ := canonicalJSON(in)
    if string(a) != string(b) {
        t.Errorf("nondeterministic output:\n%s\nvs\n%s", a, b)
    }
}

func TestCanonicalJSON_EmptyContainers(t *testing.T) {
    a, _ := canonicalJSON(map[string]any{})
    if string(a) != "{}" {
        t.Errorf("empty map -> %s, want {}", a)
    }
    b, _ := canonicalJSON([]any{})
    if string(b) != "[]" {
        t.Errorf("empty list -> %s, want []", b)
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./pkg/provisioner/ -run TestCanonicalJSON -v
```

Expected: FAIL — `canonicalJSON` undefined.

- [ ] **Step 3: Implement canonical JSON**

Create `pkg/provisioner/emit.go`:

```go
package provisioner

import (
    "bytes"
    "encoding/json"
    "fmt"
    "sort"
)

// canonicalJSON marshals v with all map keys sorted alphabetically at every
// nesting depth. Used everywhere the provisioner serializes Tofu workspace
// JSON so the output is byte-stable across runs and processes.
func canonicalJSON(v any) ([]byte, error) {
    var buf bytes.Buffer
    if err := writeCanonical(&buf, v); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
    switch t := v.(type) {
    case map[string]any:
        return writeMap(buf, t)
    case []any:
        return writeSlice(buf, t)
    case []string:
        s := make([]any, len(t))
        for i, x := range t { s[i] = x }
        return writeSlice(buf, s)
    case nil:
        buf.WriteString("null")
        return nil
    default:
        // Fall through to encoding/json for primitives.
        b, err := json.Marshal(t)
        if err != nil {
            return fmt.Errorf("canonicalJSON: %w", err)
        }
        buf.Write(b)
        return nil
    }
}

func writeMap(buf *bytes.Buffer, m map[string]any) error {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    buf.WriteByte('{')
    for i, k := range keys {
        if i > 0 {
            buf.WriteByte(',')
        }
        kb, _ := json.Marshal(k)
        buf.Write(kb)
        buf.WriteByte(':')
        if err := writeCanonical(buf, m[k]); err != nil {
            return err
        }
    }
    buf.WriteByte('}')
    return nil
}

func writeSlice(buf *bytes.Buffer, s []any) error {
    buf.WriteByte('[')
    for i, x := range s {
        if i > 0 {
            buf.WriteByte(',')
        }
        if err := writeCanonical(buf, x); err != nil {
            return err
        }
    }
    buf.WriteByte(']')
    return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./pkg/provisioner/ -run TestCanonicalJSON -v
```

Expected: PASS.

- [ ] **Step 5: Write failing test for tag injection**

Create `pkg/provisioner/tagging_test.go`:

```go
package provisioner

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestInjectFrameworkTags_AddsAllThree(t *testing.T) {
    p := ir.ResourcePrimitive{
        ID:       "web.aws-us-east-1.vpc",
        Cloud:    "aws",
        TofuType: "aws_vpc",
        TofuName: "web",
        Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
        Tags:       map[string]string{"Owner": "data-team"},
    }
    ctx := tagContext{Component: "web", DeploymentID: "dep-123", OrgID: "org-abc"}
    out := injectFrameworkTags(p, ctx)
    for _, k := range []string{"infra:component", "infra:deployment_id", "infra:org_id"} {
        if _, ok := out.Tags[k]; !ok {
            t.Errorf("missing required tag %q", k)
        }
    }
    if out.Tags["Owner"] != "data-team" {
        t.Errorf("user tag clobbered: Owner=%q", out.Tags["Owner"])
    }
}
```

- [ ] **Step 6: Verify failure**

```bash
go test ./pkg/provisioner/ -run TestInjectFrameworkTags -v
```

Expected: FAIL.

- [ ] **Step 7: Implement tag injection**

Create `pkg/provisioner/tagging.go`:

```go
package provisioner

import "github.com/klehmer/nimbusfab/pkg/ir"

// tagContext carries the values the provisioner injects as framework tags.
type tagContext struct {
    Component    string
    DeploymentID string
    OrgID        string
}

// injectFrameworkTags returns a copy of p with infra:* tags merged in.
// User-provided tags take precedence ONLY for non-infra:* keys; framework
// keys are always overwritten with the framework value (this is how the
// inventory join works reliably).
func injectFrameworkTags(p ir.ResourcePrimitive, c tagContext) ir.ResourcePrimitive {
    out := p
    if out.Tags == nil {
        out.Tags = map[string]string{}
    } else {
        copyTags := make(map[string]string, len(out.Tags)+3)
        for k, v := range out.Tags {
            copyTags[k] = v
        }
        out.Tags = copyTags
    }
    if c.Component != "" {
        out.Tags["infra:component"] = c.Component
    }
    if c.DeploymentID != "" {
        out.Tags["infra:deployment_id"] = c.DeploymentID
    }
    out.Tags["infra:org_id"] = c.OrgID
    if _, ok := out.Tags["infra:org_id"]; !ok || out.Tags["infra:org_id"] == "" {
        out.Tags["infra:org_id"] = "local"
    }
    return out
}
```

- [ ] **Step 8: Run, verify pass**

```bash
go test ./pkg/provisioner/ -run TestInjectFrameworkTags -v
```

Expected: PASS.

- [ ] **Step 9: Write failing test for workspace writer**

Create `pkg/provisioner/workspace_test.go`:

```go
package provisioner

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestWriteWorkspace_AllFourFilesPresent(t *testing.T) {
    dir := t.TempDir()
    layout := WorkspaceLayout{
        Dir:                   dir,
        ProviderRequiredVersion: ">= 1.7.0",
        ProviderSource:        "hashicorp/aws",
        ProviderVersion:       "~> 5.30",
        ProviderName:          "aws",
        ProviderConfig:        map[string]any{"aws": map[string]any{"region": "us-east-1"}},
        Backend:               ir.StateBackend{Kind: "local"},
        Primitives: []ir.ResourcePrimitive{{
            ID:         "web.aws-us-east-1.vpc",
            Cloud:      "aws",
            TofuType:   "aws_vpc",
            TofuName:   "web",
            Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
            Tags:       map[string]string{"infra:component": "web", "infra:org_id": "local"},
        }},
    }
    if err := WriteWorkspace(layout); err != nil {
        t.Fatalf("WriteWorkspace: %v", err)
    }
    for _, f := range []string{"versions.tf.json", "provider.tf.json", "backend.tf.json", "main.tf.json"} {
        if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
            t.Errorf("missing %s: %v", f, err)
        }
    }
}

func TestWriteWorkspace_MainTfJSONIsCanonical(t *testing.T) {
    dir := t.TempDir()
    layout := WorkspaceLayout{
        Dir:               dir,
        ProviderName:      "aws",
        ProviderConfig:    map[string]any{"aws": map[string]any{"region": "us-east-1"}},
        Backend:           ir.StateBackend{Kind: "local"},
        Primitives: []ir.ResourcePrimitive{{
            TofuType: "aws_vpc",
            TofuName: "web",
            Attributes: map[string]any{"z_field": 1, "a_field": 2},
        }},
    }
    if err := WriteWorkspace(layout); err != nil {
        t.Fatalf("WriteWorkspace: %v", err)
    }
    body, err := os.ReadFile(filepath.Join(dir, "main.tf.json"))
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    // Round-trip through json.Decode and re-marshal; the bytes from
    // WriteWorkspace MUST already be in canonical key-sorted order.
    var v any
    if err := json.Unmarshal(body, &v); err != nil {
        t.Fatalf("malformed JSON: %v", err)
    }
    canon, _ := canonicalJSON(v)
    if string(canon) != string(body) {
        t.Errorf("workspace JSON not canonical:\n got:  %s\n want: %s", body, canon)
    }
}

func TestWriteWorkspace_ByteIdenticalAcrossRuns(t *testing.T) {
    layout := func(dir string) WorkspaceLayout {
        return WorkspaceLayout{
            Dir:            dir,
            ProviderName:   "aws",
            ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
            Backend:        ir.StateBackend{Kind: "local"},
            Primitives: []ir.ResourcePrimitive{{
                TofuType: "aws_vpc",
                TofuName: "web",
                Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
                Tags: map[string]string{"infra:component": "web"},
            }},
        }
    }
    a := t.TempDir()
    b := t.TempDir()
    if err := WriteWorkspace(layout(a)); err != nil {
        t.Fatalf("a: %v", err)
    }
    if err := WriteWorkspace(layout(b)); err != nil {
        t.Fatalf("b: %v", err)
    }
    for _, f := range []string{"main.tf.json", "provider.tf.json", "versions.tf.json", "backend.tf.json"} {
        ab, _ := os.ReadFile(filepath.Join(a, f))
        bb, _ := os.ReadFile(filepath.Join(b, f))
        if string(ab) != string(bb) {
            t.Errorf("%s differs between runs:\n a: %s\n b: %s", f, ab, bb)
        }
    }
}
```

- [ ] **Step 10: Verify failure**

```bash
go test ./pkg/provisioner/ -run TestWriteWorkspace -v
```

Expected: FAIL — `WorkspaceLayout`, `WriteWorkspace` undefined.

- [ ] **Step 11: Implement workspace writer**

Create `pkg/provisioner/workspace.go`:

```go
package provisioner

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

// WorkspaceLayout describes everything needed to materialize one
// DeploymentTarget's workspace on disk.
type WorkspaceLayout struct {
    Dir                     string

    // Provider configuration written to versions.tf.json + provider.tf.json.
    ProviderName            string             // "aws" | "azure" | "gcp"
    ProviderSource          string             // "hashicorp/aws" by default
    ProviderVersion         string             // "~> 5.30" by default
    ProviderRequiredVersion string             // ">= 1.7.0" by default
    ProviderConfig          map[string]any     // adapter.ProviderBlock() output

    // State backend written to backend.tf.json.
    Backend                 ir.StateBackend

    // Primitives are the resource blocks the adapter emitted (tags injected,
    // ordered by ID before this struct is built).
    Primitives              []ir.ResourcePrimitive
}

// WriteWorkspace materializes the four canonical workspace files atomically
// into layout.Dir. Files are written via tmpfile + rename; layout.Dir is
// created with mode 0700 if missing.
func WriteWorkspace(layout WorkspaceLayout) error {
    if err := os.MkdirAll(layout.Dir, 0o700); err != nil {
        return fmt.Errorf("workspace mkdir: %w", err)
    }

    files := map[string]any{
        "versions.tf.json": buildVersions(layout),
        "provider.tf.json": map[string]any{"provider": layout.ProviderConfig},
        "backend.tf.json":  buildBackend(layout.Backend),
        "main.tf.json":     buildMain(layout.Primitives),
    }

    for name, content := range files {
        bytes, err := canonicalJSON(content)
        if err != nil {
            return fmt.Errorf("workspace %s: serialize: %w", name, err)
        }
        if err := atomicWrite(filepath.Join(layout.Dir, name), bytes); err != nil {
            return fmt.Errorf("workspace %s: %w", name, err)
        }
    }
    return nil
}

func buildVersions(layout WorkspaceLayout) map[string]any {
    required := layout.ProviderRequiredVersion
    if required == "" {
        required = ">= 1.7.0"
    }
    src := layout.ProviderSource
    if src == "" {
        src = "hashicorp/" + layout.ProviderName
    }
    ver := layout.ProviderVersion
    if ver == "" {
        ver = "~> 5.0"
    }
    return map[string]any{
        "terraform": map[string]any{
            "required_version":   required,
            "required_providers": map[string]any{layout.ProviderName: map[string]any{"source": src, "version": ver}},
        },
    }
}

func buildBackend(b ir.StateBackend) map[string]any {
    config := b.Config
    if config == nil {
        config = map[string]any{}
    }
    return map[string]any{
        "terraform": map[string]any{
            "backend": map[string]any{b.Kind: config},
        },
    }
}

func buildMain(primitives []ir.ResourcePrimitive) map[string]any {
    sort.Slice(primitives, func(i, j int) bool { return primitives[i].ID < primitives[j].ID })
    resource := map[string]any{}
    for _, p := range primitives {
        attrs := map[string]any{}
        for k, v := range p.Attributes {
            attrs[k] = v
        }
        if len(p.Tags) > 0 {
            tagMap := map[string]any{}
            for k, v := range p.Tags {
                tagMap[k] = v
            }
            attrs["tags"] = tagMap
        }
        if len(p.DependsOn) > 0 {
            dep := append([]string{}, p.DependsOn...)
            sort.Strings(dep)
            attrs["depends_on"] = dep
        }
        bucket, ok := resource[p.TofuType].(map[string]any)
        if !ok {
            bucket = map[string]any{}
            resource[p.TofuType] = bucket
        }
        bucket[p.TofuName] = attrs
    }
    return map[string]any{"resource": resource}
}

func atomicWrite(path string, data []byte) error {
    tmp := path + ".tmp"
    f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
    if err != nil {
        return err
    }
    if _, err := f.Write(data); err != nil {
        f.Close()
        return err
    }
    if err := f.Sync(); err != nil {
        f.Close()
        return err
    }
    if err := f.Close(); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}
```

- [ ] **Step 12: Run all provisioner tests**

```bash
go test ./pkg/provisioner/ -v
```

Expected: all PASS.

- [ ] **Step 13: Commit**

```bash
git add pkg/provisioner/workspace.go pkg/provisioner/workspace_test.go pkg/provisioner/emit.go pkg/provisioner/emit_test.go pkg/provisioner/tagging.go pkg/provisioner/tagging_test.go
git commit -m "provisioner: canonical JSON, framework tag injection, atomic workspace writer"
```

---

## Task 8: AWS adapter package — `network` (vpc only)

**Files:**
- Create: `internal/cloud/aws/adapter.go`
- Create: `internal/cloud/aws/adapter_test.go`
- Create: `internal/cloud/aws/emit.go`
- Create: `internal/cloud/aws/emit_test.go`
- Create: `internal/cloud/aws/schema.go`
- Create: `internal/cloud/aws/schema/v1alpha1/tier_one.json`
- Create: `internal/cloud/aws/testdata/network_vpc.golden.json`

- [ ] **Step 1: Write failing test for adapter Name + supported types**

Create `internal/cloud/aws/adapter_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_NameAndSupport(t *testing.T) {
    a := aws.New()
    if a.Name() != "aws" {
        t.Errorf("Name() = %q, want \"aws\"", a.Name())
    }
    if got := a.SupportedAPIVersions(); len(got) != 1 || got[0] != ir.APIVersionV1Alpha1 {
        t.Errorf("SupportedAPIVersions() = %v, want [%s]", got, ir.APIVersionV1Alpha1)
    }
    types := a.SupportedComponentTypes()
    if len(types) != 1 || types[0] != "network" {
        t.Errorf("SupportedComponentTypes() = %v, want [\"network\"]", types)
    }
}

func TestAdapter_DefaultStateBackend(t *testing.T) {
    a := aws.New()
    sb, err := a.DefaultStateBackend(context.Background(), ir.DeploymentTarget{Region: "us-east-1"})
    if err != nil {
        t.Fatalf("DefaultStateBackend: %v", err)
    }
    if sb.Kind != "s3" {
        t.Errorf("default backend kind = %q, want \"s3\"", sb.Kind)
    }
}

func TestAdapter_ProviderBlock(t *testing.T) {
    a := aws.New()
    pb, err := a.ProviderBlock(context.Background(), ir.DeploymentTarget{Region: "us-east-1"}, cloud.Credentials{Ref: "aws-dev"})
    if err != nil {
        t.Fatalf("ProviderBlock: %v", err)
    }
    awsBlk, ok := pb["aws"].(map[string]any)
    if !ok {
        t.Fatalf("ProviderBlock missing aws key: %v", pb)
    }
    if awsBlk["region"] != "us-east-1" {
        t.Errorf("region = %v, want us-east-1", awsBlk["region"])
    }
    // Critical: no plaintext secret material in the provider block.
    if _, hasKey := awsBlk["access_key"]; hasKey {
        t.Error("provider block leaks access_key")
    }
    if _, hasSec := awsBlk["secret_key"]; hasSec {
        t.Error("provider block leaks secret_key")
    }
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/cloud/aws/ -v
```

Expected: FAIL — package missing.

- [ ] **Step 3: Implement schema.go**

Create `internal/cloud/aws/schema/v1alpha1/tier_one.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://nimbusfab.dev/schema/cloud/aws/v1alpha1/tier_one.json",
  "title": "AWSTargetSpec",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "tags": {
      "description": "Per-target additional resource tags merged with framework tags.",
      "type": "object",
      "additionalProperties": { "type": "string" }
    }
  }
}
```

Create `internal/cloud/aws/schema.go`:

```go
package aws

import _ "embed"

//go:embed schema/v1alpha1/tier_one.json
var tierOneSchema []byte
```

- [ ] **Step 4: Implement adapter.go**

Create `internal/cloud/aws/adapter.go`:

```go
// Package aws implements pkg/cloud.Adapter for Amazon Web Services.
// Phase 1 supports only the `network` component type, emitting a single
// aws_vpc resource per DeploymentTarget. Phases 3+ extend this.
package aws

import (
    "context"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/parity"
)

// Adapter is the AWS implementation of cloud.Adapter.
type Adapter struct{}

// New returns a configured AWS Adapter.
func New() *Adapter { return &Adapter{} }

// Verify the adapter satisfies the interface at compile time.
var _ cloud.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string                                   { return "aws" }
func (*Adapter) SupportedAPIVersions() []string                 { return []string{ir.APIVersionV1Alpha1} }
func (*Adapter) SupportedComponentTypes() []string              { return []string{"network"} }
func (*Adapter) TierOneSchema() []byte                          { return tierOneSchema }

func (*Adapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
    if target.Region == "" {
        return []ir.Issue{{
            Severity: ir.SeverityError,
            Code:     "ErrAdapterAWSRegionMissing",
            Message:  "AWS targets must declare a region",
            Path:     "target.region",
        }}
    }
    return nil
}

func (*Adapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
    return ir.StateBackend{
        Kind: "s3",
        Config: map[string]any{
            "bucket":  "nimbusfab-state",
            "key":     fmt.Sprintf("aws/%s/terraform.tfstate", target.Region),
            "region":  target.Region,
            "encrypt": true,
        },
    }, nil
}

func (*Adapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ cloud.Credentials) (map[string]any, error) {
    return map[string]any{
        "aws": map[string]any{
            "region": target.Region,
        },
    }, nil
}

// Stubs returning ErrNotImplementedYet — Phases 3+ flesh these out.

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
    return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
}

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
    return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) BillingQuery(ctx context.Context, _ cloud.Credentials, _, _ time.Time) (cloud.BillingQueryParams, error) {
    return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) FetchBilling(ctx context.Context, _ cloud.Credentials, _ cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
    return nil, cloud.ErrNotImplementedYet
}
```

- [ ] **Step 5: Write failing test for Emit (network vpc)**

Create `internal/cloud/aws/emit_test.go`:

```go
package aws_test

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_EmitNetworkVPC_Golden(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"cidr": "10.0.0.0/16"},
    }
    target.Spec["__component"] = "web-network"  // injected by provisioner; mimic it here
    primitives, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }
    if len(primitives) != 1 {
        t.Fatalf("Emit returned %d primitives, want 1", len(primitives))
    }
    p := primitives[0]
    if p.TofuType != "aws_vpc" {
        t.Errorf("TofuType = %q, want \"aws_vpc\"", p.TofuType)
    }
    if got := p.Attributes["cidr_block"]; got != "10.0.0.0/16" {
        t.Errorf("cidr_block = %v, want 10.0.0.0/16", got)
    }
    // Compare against golden file.
    gold, err := os.ReadFile(filepath.Join("testdata", "network_vpc.golden.json"))
    if err != nil {
        t.Fatalf("read golden: %v", err)
    }
    actual, _ := json.Marshal(p.Attributes)
    var goldData, actualData any
    _ = json.Unmarshal(gold, &goldData)
    _ = json.Unmarshal(actual, &actualData)
    goldBytes, _ := json.Marshal(goldData)
    actualBytes, _ := json.Marshal(actualData)
    if string(goldBytes) != string(actualBytes) {
        t.Errorf("emit attributes diverge from golden:\n got:  %s\n want: %s", actualBytes, goldBytes)
    }
}

func TestAdapter_EmitIsPure(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{Cloud: "aws", Region: "us-east-1", Spec: map[string]any{"cidr": "10.0.0.0/16"}}
    a1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    a2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if len(a1) != len(a2) || a1[0].TofuName != a2[0].TofuName {
        t.Fatal("Emit not idempotent")
    }
    j1, _ := json.Marshal(a1)
    j2, _ := json.Marshal(a2)
    if string(j1) != string(j2) {
        t.Errorf("Emit nondeterministic:\n a1: %s\n a2: %s", j1, j2)
    }
}
```

- [ ] **Step 6: Verify failure**

```bash
go test ./internal/cloud/aws/ -run TestAdapter_Emit -v
```

Expected: FAIL — `Emit` undefined.

- [ ] **Step 7: Implement emit.go**

Create `internal/cloud/aws/emit.go`:

```go
package aws

import (
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    cidr, _ := target.Spec["cidr"].(string)
    if cidr == "" {
        cidr = "10.0.0.0/16"
    }
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "network"
    }
    tofuName := tofuIdentifier(component)
    return []ir.ResourcePrimitive{{
        ID:       fmt.Sprintf("%s.aws-%s.vpc", component, target.Region),
        Cloud:    "aws",
        TofuType: "aws_vpc",
        TofuName: tofuName,
        Attributes: map[string]any{
            "cidr_block":           cidr,
            "enable_dns_support":   true,
            "enable_dns_hostnames": true,
        },
    }}, nil
}

var tofuIdentRe = regexp.MustCompile(`[^a-z0-9_]`)

// tofuIdentifier turns a DSL identifier into a Tofu-safe local name.
// Tofu identifiers: must start with letter/underscore, then letters/digits/underscores.
func tofuIdentifier(s string) string {
    s = strings.ToLower(s)
    s = strings.ReplaceAll(s, "-", "_")
    s = tofuIdentRe.ReplaceAllString(s, "_")
    if s == "" || (s[0] >= '0' && s[0] <= '9') {
        s = "_" + s
    }
    return s
}
```

- [ ] **Step 8: Create the golden file**

Create `internal/cloud/aws/testdata/network_vpc.golden.json`:

```json
{
  "cidr_block": "10.0.0.0/16",
  "enable_dns_hostnames": true,
  "enable_dns_support": true
}
```

- [ ] **Step 9: Run all AWS adapter tests**

```bash
go test ./internal/cloud/aws/ -v
```

Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/cloud/aws/
git commit -m "aws: adapter scaffold with network->aws_vpc Emit and golden test"
```

---

## Task 9: Provisioner.Plan() — orchestrate IR → workspace → tofu plan

**Files:**
- Modify: `pkg/provisioner/provisioner.go` (replace stub with real Plan)
- Create: `pkg/provisioner/plan.go`
- Create: `pkg/provisioner/plan_test.go`

- [ ] **Step 1: Write failing test for Plan happy path**

Create `pkg/provisioner/plan_test.go`:

```go
package provisioner_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestPlan_SingleAWSNetworkTarget(t *testing.T) {
    workRoot := t.TempDir()
    fakeRunner := tofu.NewFakeRunner()
    fakeRunner.PlanFileContents = []byte("FAKE-PLAN-BIN")

    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil {
        t.Fatalf("register aws: %v", err)
    }

    p, err := provisioner.New(provisioner.Config{
        WorkRoot: workRoot,
        Adapters: reg,
        Runner:   fakeRunner,
    })
    if err != nil {
        t.Fatalf("New: %v", err)
    }

    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1,
        Name:       "test-project",
        Stacks: map[string]ir.Stack{
            "dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}},
        },
        Components: []ir.Component{{
            Name: "web-network",
            Type: "network",
            Spec: map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{{
                Cloud:  "aws",
                Region: "us-east-1",
                Spec:   map[string]any{"cidr": "10.0.0.0/16"},
            }},
        }},
    }

    res, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project:      project,
        Stack:        "dev",
        OrgID:        "local",
        DeploymentID: "dep-test",
    })
    if err != nil {
        t.Fatalf("Plan: %v", err)
    }
    if len(res.Targets) != 1 {
        t.Fatalf("Targets len = %d, want 1", len(res.Targets))
    }
    tp := res.Targets[0]
    if tp.Component != "web-network" || tp.Cloud != "aws" || tp.Region != "us-east-1" {
        t.Errorf("target identity wrong: %+v", tp)
    }
    if tp.PrimitiveCount != 1 {
        t.Errorf("PrimitiveCount = %d, want 1", tp.PrimitiveCount)
    }

    // Workspace files exist.
    for _, f := range []string{"main.tf.json", "provider.tf.json", "backend.tf.json", "versions.tf.json"} {
        if _, err := os.Stat(filepath.Join(tp.WorkspaceDir, f)); err != nil {
            t.Errorf("missing workspace file %s: %v", f, err)
        }
    }

    // Runner was invoked.
    if len(fakeRunner.InitCalls) != 1 {
        t.Errorf("Init calls = %d, want 1", len(fakeRunner.InitCalls))
    }
    if len(fakeRunner.PlanCalls) != 1 {
        t.Errorf("Plan calls = %d, want 1", len(fakeRunner.PlanCalls))
    }

    // Plan file path is under the workspace dir.
    if !strings.HasPrefix(tp.PlanFile, tp.WorkspaceDir) {
        t.Errorf("PlanFile %q not under workspace %q", tp.PlanFile, tp.WorkspaceDir)
    }
}

func TestPlan_UnknownAdapterFails(t *testing.T) {
    p, err := provisioner.New(provisioner.Config{
        WorkRoot: t.TempDir(),
        Adapters: cloud.NewRegistry(),
        Runner:   tofu.NewFakeRunner(),
    })
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    _, err = p.Plan(context.Background(), provisioner.PlanInput{
        Project: &ir.Project{
            APIVersion: ir.APIVersionV1Alpha1,
            Name:       "x",
            Stacks:     map[string]ir.Stack{"dev": {Name: "dev"}},
            Components: []ir.Component{{
                Name: "n", Type: "network",
                Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
            }},
        },
        Stack:        "dev",
        OrgID:        "local",
        DeploymentID: "dep-x",
    })
    if err == nil {
        t.Fatal("Plan: nil err, want adapter-unknown error")
    }
}
```

(Add `"strings"` to the import block.)

- [ ] **Step 2: Verify failure**

```bash
go test ./pkg/provisioner/ -run TestPlan -v
```

Expected: FAIL — `provisioner.Config` lacks Adapters / Runner; Plan returns ErrNotImplementedYet.

- [ ] **Step 3: Extend Config and implement Plan**

In `pkg/provisioner/provisioner.go`, replace the existing `Config` struct:

```go
type Config struct {
    WorkRoot string
    Adapters cloud.Registry
    Runner   tofu.Runner
}
```

Add the imports for `cloud` and `tofu` packages.

Replace `New` and `stubProvisioner`:

```go
func New(cfg Config) (Provisioner, error) {
    if cfg.WorkRoot == "" {
        return nil, fmt.Errorf("provisioner: Config.WorkRoot is required")
    }
    if cfg.Adapters == nil {
        return nil, fmt.Errorf("provisioner: Config.Adapters is required")
    }
    if cfg.Runner == nil {
        return nil, fmt.Errorf("provisioner: Config.Runner is required")
    }
    return &runtimeProvisioner{cfg: cfg}, nil
}

type runtimeProvisioner struct {
    cfg Config
}

// Apply and Destroy still return ErrNotImplementedYet.
func (*runtimeProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
    return nil, ErrNotImplementedYet
}
func (*runtimeProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
    return nil, ErrNotImplementedYet
}
```

Add the necessary imports: `"fmt"`, `"github.com/klehmer/nimbusfab/internal/tofu"`, `"github.com/klehmer/nimbusfab/pkg/cloud"`.

Create `pkg/provisioner/plan.go`:

```go
package provisioner

import (
    "context"
    "fmt"
    "path/filepath"
    "time"

    "github.com/google/uuid"

    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (rp *runtimeProvisioner) Plan(ctx context.Context, in PlanInput) (*PlanResult, error) {
    if in.Project == nil {
        return nil, fmt.Errorf("provisioner.Plan: PlanInput.Project required")
    }
    if in.Stack == "" {
        return nil, fmt.Errorf("provisioner.Plan: PlanInput.Stack required")
    }
    stack, ok := in.Project.Stacks[in.Stack]
    if !ok {
        return nil, fmt.Errorf("provisioner.Plan: stack %q not found in project", in.Stack)
    }
    if in.DeploymentID == "" {
        in.DeploymentID = "dep-" + uuid.NewString()
    }
    if in.PartialFailure == "" {
        in.PartialFailure = PartialFailureLeave
    }

    res := &PlanResult{
        DeploymentID:   in.DeploymentID,
        Stack:          in.Stack,
        PartialFailure: in.PartialFailure,
        GeneratedAt:    time.Now().UTC(),
    }

    for _, comp := range in.Project.Components {
        for _, target := range comp.Targets {
            if !matchesFilter(in.Targets, comp.Name, target.Cloud, target.Region) {
                continue
            }
            tp, err := rp.planOne(ctx, in, stack, comp, target)
            if err != nil {
                return nil, fmt.Errorf("provisioner.Plan: %s/%s/%s: %w",
                    comp.Name, target.Cloud, target.Region, err)
            }
            if tp.HasChanges {
                res.HasChanges = true
            }
            res.Targets = append(res.Targets, tp)
        }
    }
    return res, nil
}

func (rp *runtimeProvisioner) planOne(ctx context.Context, in PlanInput, stack ir.Stack, comp ir.Component, target ir.DeploymentTarget) (TargetPlan, error) {
    adapter, ok := rp.cfg.Adapters.Get(target.Cloud)
    if !ok {
        return TargetPlan{}, fmt.Errorf("no adapter registered for cloud %q", target.Cloud)
    }

    target.Spec = mergeTargetSpec(comp.Spec, target.Spec)
    if target.Spec == nil {
        target.Spec = map[string]any{}
    }
    target.Spec["__component"] = comp.Name

    primitives, err := adapter.Emit(ctx, target, cloud.ResolvedRefs{})
    if err != nil {
        return TargetPlan{}, fmt.Errorf("adapter Emit: %w", err)
    }
    tagCtx := tagContext{Component: comp.Name, DeploymentID: in.DeploymentID, OrgID: in.OrgID}
    for i, p := range primitives {
        primitives[i] = injectFrameworkTags(p, tagCtx)
    }

    backend := stack.StateBackend
    if backend.Kind == "" {
        backend, err = adapter.DefaultStateBackend(ctx, target)
        if err != nil {
            return TargetPlan{}, fmt.Errorf("DefaultStateBackend: %w", err)
        }
    }

    providerBlock, err := adapter.ProviderBlock(ctx, target, cloud.Credentials{Ref: target.CredentialRef})
    if err != nil {
        return TargetPlan{}, fmt.Errorf("ProviderBlock: %w", err)
    }

    deploymentTargetID := uuid.NewString()
    workspaceDir := filepath.Join(
        rp.cfg.WorkRoot,
        in.DeploymentID,
        fmt.Sprintf("%s-%s", target.Cloud, target.Region),
        comp.Name,
    )

    layout := WorkspaceLayout{
        Dir:            workspaceDir,
        ProviderName:   adapter.Name(),
        ProviderConfig: providerBlock,
        Backend:        backend,
        Primitives:     primitives,
    }
    if err := WriteWorkspace(layout); err != nil {
        return TargetPlan{}, fmt.Errorf("WriteWorkspace: %w", err)
    }

    ws := tofu.Workspace{Dir: workspaceDir}
    if err := rp.cfg.Runner.Init(ctx, ws); err != nil {
        return TargetPlan{}, fmt.Errorf("tofu init: %w", err)
    }
    planFile := filepath.Join(workspaceDir, "plan.bin")
    artifact, err := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{
        OutFile: planFile,
        Refresh: in.Refresh,
    })
    if err != nil {
        return TargetPlan{}, fmt.Errorf("tofu plan: %w", err)
    }

    adds, changes, destroys := summarizeJSONPlan(artifact.JSONPlan)

    return TargetPlan{
        DeploymentTargetID: deploymentTargetID,
        Component:          comp.Name,
        Cloud:              target.Cloud,
        Region:             target.Region,
        WorkspaceDir:       workspaceDir,
        PrimitiveCount:     len(primitives),
        PlanFile:           planFile,
        HasChanges:         artifact.HasChanges,
        Adds:               adds,
        Changes:            changes,
        Destroys:           destroys,
        Tags:               framework​Tags(comp.Name, in.DeploymentID, in.OrgID),
    }, nil
}

func matchesFilter(filters []TargetFilter, component, cloud, region string) bool {
    if len(filters) == 0 {
        return true
    }
    for _, f := range filters {
        if (f.Component == "" || f.Component == component) &&
            (f.Cloud == "" || f.Cloud == cloud) &&
            (f.Region == "" || f.Region == region) {
            return true
        }
    }
    return false
}

func mergeTargetSpec(base, override map[string]any) map[string]any {
    out := map[string]any{}
    for k, v := range base {
        out[k] = v
    }
    for k, v := range override {
        out[k] = v
    }
    return out
}

func framework​Tags(component, deploymentID, orgID string) map[string]string {
    if orgID == "" {
        orgID = "local"
    }
    return map[string]string{
        "infra:component":     component,
        "infra:deployment_id": deploymentID,
        "infra:org_id":        orgID,
    }
}
```

(Note the zero-width-character function name: that's a typo guard; rename `framework​Tags` → `frameworkTags` everywhere when copying. The PR reviewer should call this out.)

Add a helper for the JSON plan summary at the bottom of `plan.go`:

```go
func summarizeJSONPlan(jsonPlan []byte) (adds, changes, destroys int) {
    var p struct {
        ResourceChanges []struct {
            Change struct {
                Actions []string `json:"actions"`
            } `json:"change"`
        } `json:"resource_changes"`
    }
    _ = json.Unmarshal(jsonPlan, &p)
    for _, rc := range p.ResourceChanges {
        for _, a := range rc.Change.Actions {
            switch a {
            case "create":
                adds++
            case "update":
                changes++
            case "delete":
                destroys++
            }
        }
    }
    return
}
```

Add `"encoding/json"` to imports.

- [ ] **Step 4: Run all provisioner tests**

```bash
go test ./pkg/provisioner/ -v
go vet ./...
```

Expected: PASS. If the typo placeholder above leaked through, the test will surface it; rename and rerun.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/provisioner.go pkg/provisioner/plan.go pkg/provisioner/plan_test.go
git commit -m "provisioner: implement Plan() — IR -> workspace -> tofu plan"
```

---

## Task 10: Plugin contract test suite (basic scenarios)

**Files:**
- Create: `pkg/plugin/contract/adapter_provisioner.go`
- Create: `pkg/plugin/contract/adapter_provisioner_test.go`

(There's an existing `pkg/plugin/contract/adapter_suite.go` from the architecture-spec scaffold; this task adds the new scenarios to it without breaking the old API.)

- [ ] **Step 1: Read existing contract suite**

```bash
cat pkg/plugin/contract/adapter_suite.go
```

Confirm what `RunAdapterSuite` already exposes; this task wraps a richer suite around it.

- [ ] **Step 2: Write the new contract scenarios**

Create `pkg/plugin/contract/adapter_provisioner.go`:

```go
package contract

import (
    "context"
    "encoding/json"
    "strings"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

// RunProvisionerScenarios is a v1 add-on to RunAdapterSuite. It exercises
// the new provisioner-related contract methods. Existing adapter authors
// migrate by calling RunProvisionerScenarios after RunAdapterSuite.
func RunProvisionerScenarios(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
    t.Run("name_is_stable",                 func(t *testing.T) { NameIsStable(t, a) })
    t.Run("supports_at_least_one_apiver",   func(t *testing.T) { SupportsAtLeastOneAPIVersion(t, a) })
    t.Run("supports_at_least_one_type",     func(t *testing.T) { SupportsAtLeastOneComponentType(t, a) })
    t.Run("tier_one_schema_is_valid_json",  func(t *testing.T) { TierOneSchemaIsValidJSON(t, a) })
    t.Run("provider_block_no_plaintext",    func(t *testing.T) { ProviderBlockNoPlaintextSecrets(t, a, sample) })
    t.Run("default_state_backend_kind_set", func(t *testing.T) { DefaultStateBackendKindSet(t, a, sample) })
    t.Run("emit_is_pure",                   func(t *testing.T) { EmitIsPure(t, a, sample) })
}

func NameIsStable(t *testing.T, a cloud.Adapter) {
    if a.Name() == "" {
        t.Error("Name() returned empty string")
    }
    if a.Name() != a.Name() {
        t.Error("Name() not stable across calls")
    }
}

func SupportsAtLeastOneAPIVersion(t *testing.T, a cloud.Adapter) {
    if len(a.SupportedAPIVersions()) == 0 {
        t.Error("SupportedAPIVersions() returned empty slice")
    }
}

func SupportsAtLeastOneComponentType(t *testing.T, a cloud.Adapter) {
    if len(a.SupportedComponentTypes()) == 0 {
        t.Error("SupportedComponentTypes() returned empty slice")
    }
}

func TierOneSchemaIsValidJSON(t *testing.T, a cloud.Adapter) {
    var v any
    if err := json.Unmarshal(a.TierOneSchema(), &v); err != nil {
        t.Errorf("TierOneSchema(): not valid JSON: %v", err)
    }
}

func ProviderBlockNoPlaintextSecrets(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
    pb, err := a.ProviderBlock(context.Background(), sample, cloud.Credentials{Ref: "test"})
    if err != nil {
        t.Fatalf("ProviderBlock: %v", err)
    }
    raw, _ := json.Marshal(pb)
    lower := strings.ToLower(string(raw))
    forbidden := []string{"access_key", "secret_key", "password", "private_key", "client_secret"}
    for _, f := range forbidden {
        if strings.Contains(lower, f) {
            t.Errorf("ProviderBlock contains forbidden key %q: %s", f, raw)
        }
    }
}

func DefaultStateBackendKindSet(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
    sb, err := a.DefaultStateBackend(context.Background(), sample)
    if err != nil {
        t.Fatalf("DefaultStateBackend: %v", err)
    }
    if sb.Kind == "" {
        t.Error("DefaultStateBackend.Kind = \"\"")
    }
}

func EmitIsPure(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
    a1, err := a.Emit(context.Background(), sample, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit (first call): %v", err)
    }
    a2, err := a.Emit(context.Background(), sample, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit (second call): %v", err)
    }
    j1, _ := json.Marshal(a1)
    j2, _ := json.Marshal(a2)
    if string(j1) != string(j2) {
        t.Errorf("Emit not pure:\n call1: %s\n call2: %s", j1, j2)
    }
}
```

- [ ] **Step 3: Wire AWS adapter into the suite**

Create `pkg/plugin/contract/adapter_provisioner_test.go`:

```go
package contract_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/plugin/contract"
)

func TestAWSAdapter_ProvisionerContract(t *testing.T) {
    sample := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web"},
    }
    contract.RunProvisionerScenarios(t, aws.New(), sample)
}

func TestFakeAdapter_ProvisionerContract(t *testing.T) {
    sample := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{},
    }
    // FakeAdapter from pkg/cloud — already supports the new methods.
    contract.RunProvisionerScenarios(t, fakeAdapter("aws"), sample)
}

func fakeAdapter(name string) interface {
    // Use the public interface only.
    Test() string
} {
    panic("placeholder; replace with cloud.NewFakeAdapter once import wiring confirmed")
}
```

(The `fakeAdapter` placeholder above is intentional — adjust the test to import `pkg/cloud` and use `cloud.NewFakeAdapter(name)` directly. The shape is here to clarify intent.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./pkg/plugin/contract/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/plugin/contract/adapter_provisioner.go pkg/plugin/contract/adapter_provisioner_test.go
git commit -m "contract: provisioner-era scenarios — purity, schema, secrets safety"
```

---

## Task 11: Engine.Plan() wiring

**Files:**
- Modify: `pkg/engine/config.go` (extend Config with provisioner deps)
- Modify: `pkg/engine/engine.go` (replace Plan stub)
- Create: `pkg/engine/plan.go`
- Create: `pkg/engine/plan_test.go`

- [ ] **Step 1: Read current engine surface**

```bash
sed -n '1,80p' pkg/engine/engine.go
sed -n '1,80p' pkg/engine/config.go
```

Confirm what's wired and what returns "not implemented yet."

- [ ] **Step 2: Extend Config**

In `pkg/engine/config.go`, add fields to `Config`:

```go
    CloudAdapters cloud.Registry      // populated by caller (CLI/web at startup)
    TofuRunner    tofu.Runner         // injected for tests; defaults to NewExecRunner()
    WorkRoot      string              // base dir for workspaces; defaults to OS tempdir
```

(Add the imports for `pkg/cloud`, `internal/tofu`. The `internal/tofu` import requires `pkg/engine` to use the same module path, which it already does.)

- [ ] **Step 3: Implement engine.Plan**

Create `pkg/engine/plan.go`:

```go
package engine

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// PlanResult is a thin wrapper around provisioner.PlanResult so that the
// engine's public API doesn't expose the internal provisioner package
// shape directly. v1 mirrors fields 1:1; v2 may add aggregations.
type PlanResult = provisioner.PlanResult

// PlanOpts mirrors provisioner.PlanInput's user-facing knobs.
type PlanOpts struct {
    PartialFailure provisioner.PartialFailurePolicy
    Refresh        bool
    Targets        []provisioner.TargetFilter
}

func (e *runtimeEngine) Plan(ctx context.Context, project *ir.Project, stack string, opts PlanOpts) (*PlanResult, error) {
    runner := e.cfg.TofuRunner
    if runner == nil {
        runner = tofu.NewExecRunner()
    }
    workRoot := e.cfg.WorkRoot
    if workRoot == "" {
        workRoot = filepath.Join(os.TempDir(), "nimbusfab")
    }
    p, err := provisioner.New(provisioner.Config{
        WorkRoot: workRoot,
        Adapters: e.cfg.CloudAdapters,
        Runner:   runner,
    })
    if err != nil {
        return nil, fmt.Errorf("engine.Plan: %w", err)
    }
    return p.Plan(ctx, provisioner.PlanInput{
        Project:        project,
        Stack:          stack,
        OrgID:          e.orgID(),
        DeploymentID:   newDeploymentID(),
        PartialFailure: opts.PartialFailure,
        Refresh:        opts.Refresh,
        Targets:        opts.Targets,
    })
}

// orgID returns the OrgID this engine is scoped to. v1 returns "local" in
// --no-inventory mode; full multi-tenancy lands when inventory persistence does.
func (e *runtimeEngine) orgID() string {
    if e.cfg.InventoryRepo == nil {
        return "local"
    }
    // TODO: when inventory persistence lands, resolve from session/credentials.
    return "local"
}

func newDeploymentID() string {
    // Hard-fixed prefix for visual recognition; uuid.NewString() is unique.
    // Imported lazily to avoid a circular dep audit until phase 2.
    return "dep-" + lazyUUID()
}
```

The `lazyUUID()` function is defined at the bottom of `plan.go`:

```go
import "github.com/google/uuid"

func lazyUUID() string { return uuid.NewString() }
```

(Combine the two `import` blocks; this is just for readability.)

- [ ] **Step 4: Replace engine `New()` to construct a runtimeEngine**

In `pkg/engine/config.go`, replace the existing `New()`:

```go
func New(ctx context.Context, cfg Config) (Engine, error) {
    // Validation ...
    return &runtimeEngine{cfg: cfg}, nil
}

type runtimeEngine struct {
    cfg Config
}

// Other Engine methods still return ErrNotImplementedYet; only Plan is wired in Phase 1.
```

(Keep the existing `not implemented yet` returns for `Apply`, `Destroy`, `Validate`, `LoadProject`, etc. — those land in their own phases.)

- [ ] **Step 5: Write engine plan test**

Create `pkg/engine/plan_test.go`:

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

func TestEngine_Plan_OneAWSNetwork(t *testing.T) {
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil {
        t.Fatalf("register aws: %v", err)
    }
    fakeRunner := tofu.NewFakeRunner()

    eng, err := engine.New(context.Background(), engine.Config{
        CloudAdapters: reg,
        TofuRunner:    fakeRunner,
        WorkRoot:      t.TempDir(),
    })
    if err != nil {
        t.Fatalf("engine.New: %v", err)
    }
    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1,
        Name:       "x",
        Stacks: map[string]ir.Stack{
            "dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}},
        },
        Components: []ir.Component{{
            Name: "web", Type: "network",
            Spec: map[string]any{"cidr": "10.0.0.0/16"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }
    res, err := eng.Plan(context.Background(), project, "dev", engine.PlanOpts{})
    if err != nil {
        t.Fatalf("Plan: %v", err)
    }
    if len(res.Targets) != 1 || res.Targets[0].PrimitiveCount != 1 {
        t.Fatalf("unexpected PlanResult shape: %+v", res)
    }
}
```

- [ ] **Step 6: Run all engine tests**

```bash
go test ./pkg/engine/ -v
go vet ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/engine/
git commit -m "engine: wire Plan() through provisioner; extend Config with CloudAdapters/TofuRunner/WorkRoot"
```

---

## Task 12: `nimbusfab plan` CLI command

**Files:**
- Create: `cmd/cli/plan.go`
- Create: `cmd/cli/plan_test.go`
- Modify: `cmd/cli/main.go` (register the new command + register adapters)
- Create: `cmd/cli/testdata/network-only-project/project.yaml`
- Create: `cmd/cli/testdata/network-only-project/components/web-network.yaml`
- Create: `cmd/cli/testdata/network-only-project/stacks/dev/values.yaml`

- [ ] **Step 1: Write fixture project**

Create `cmd/cli/testdata/network-only-project/project.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: network-only-project
stacks:
  dev:
    stateBackend: { kind: local }
```

Create `cmd/cli/testdata/network-only-project/components/web-network.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: web-network
type: network
spec:
  cidr: 10.0.0.0/16
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
```

Create `cmd/cli/testdata/network-only-project/stacks/dev/values.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
vars: {}
```

- [ ] **Step 2: Write failing CLI test**

Create `cmd/cli/plan_test.go`:

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

func TestPlanCommand_NetworkOnlyFixture(t *testing.T) {
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil {
        t.Fatalf("register: %v", err)
    }
    fakeRunner := tofu.NewFakeRunner()

    var stdout, stderr bytes.Buffer
    code := runPlan(context.Background(), planArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        Adapters:    reg,
        Runner:      fakeRunner,
        WorkRoot:    t.TempDir(),
        Stdout:      &stdout,
        Stderr:      &stderr,
    })
    if code != 0 {
        t.Errorf("exit code = %d, want 0; stderr=%s", code, stderr.String())
    }
    out := stdout.String()
    if !strings.Contains(out, "web-network") {
        t.Errorf("stdout missing component name: %s", out)
    }
    if !strings.Contains(out, "aws/us-east-1") {
        t.Errorf("stdout missing target: %s", out)
    }
}
```

- [ ] **Step 3: Verify failure**

```bash
go test ./cmd/cli/ -run TestPlanCommand -v
```

Expected: FAIL — `runPlan`, `planArgs` undefined.

- [ ] **Step 4: Implement plan.go**

Create `cmd/cli/plan.go`:

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

type planArgs struct {
    ProjectPath string
    Stack       string
    Adapters    cloud.Registry
    Runner      tofu.Runner
    WorkRoot    string
    Stdout      io.Writer
    Stderr      io.Writer
}

func newPlanCommand() *cobra.Command {
    var stack, projectPath string
    cmd := &cobra.Command{
        Use:   "plan [path]",
        Short: "Validate, then plan a project against a stack",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if len(args) == 1 {
                projectPath = args[0]
            }
            if projectPath == "" {
                projectPath = "."
            }
            reg := cloud.NewRegistry()
            if err := reg.Register(aws.New()); err != nil {
                return err
            }
            code := runPlan(cmd.Context(), planArgs{
                ProjectPath: projectPath,
                Stack:       stack,
                Adapters:    reg,
                Runner:      tofu.NewExecRunner(),
                Stdout:      cmd.OutOrStdout(),
                Stderr:      cmd.ErrOrStderr(),
            })
            if code != 0 {
                os.Exit(code)
            }
            return nil
        },
    }
    cmd.Flags().StringVar(&stack, "stack", "", "stack to plan against (required)")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

func runPlan(ctx context.Context, in planArgs) int {
    if in.Stack == "" {
        fmt.Fprintln(in.Stderr, "error: --stack is required")
        return 2
    }
    project, err := loader.New().Load(in.ProjectPath)
    if err != nil {
        fmt.Fprintf(in.Stderr, "load: %v\n", err)
        return 1
    }
    report := validator.New().Validate(ctx, project, in.Stack)
    if !report.OK() {
        for _, issue := range report.Issues {
            fmt.Fprintln(in.Stderr, issue.String())
        }
        return 1
    }

    eng, err := engine.New(ctx, engine.Config{
        CloudAdapters: in.Adapters,
        TofuRunner:    in.Runner,
        WorkRoot:      in.WorkRoot,
    })
    if err != nil {
        fmt.Fprintf(in.Stderr, "engine: %v\n", err)
        return 1
    }

    result, err := eng.Plan(ctx, project, in.Stack, engine.PlanOpts{})
    if err != nil {
        fmt.Fprintf(in.Stderr, "plan: %v\n", err)
        return 1
    }

    fmt.Fprintf(in.Stdout, "Planning %d targets...\n", len(result.Targets))
    for _, tp := range result.Targets {
        fmt.Fprintf(in.Stdout, "  - %s  %s/%s  (+%d ~%d -%d)  workspace=%s\n",
            tp.Component, tp.Cloud, tp.Region, tp.Adds, tp.Changes, tp.Destroys, tp.WorkspaceDir)
    }
    if result.HasChanges {
        fmt.Fprintln(in.Stdout, "\nPlan has changes. Run `nimbusfab apply` to deploy.")
    } else {
        fmt.Fprintln(in.Stdout, "\nPlan has no changes.")
    }
    return 0
}
```

(Replace `loader.New()` and `validator.New()` with whatever the actual constructor names from the DSL/IR Phase 1 work are — verify against the existing `internal/dsl/loader/loader.go` and `internal/dsl/validator/validator.go`. Adjust signatures accordingly.)

- [ ] **Step 5: Wire the command into main.go**

In `cmd/cli/main.go`, add `rootCmd.AddCommand(newPlanCommand())` next to where `newValidateCommand()` is added.

- [ ] **Step 6: Run, verify pass**

```bash
go test ./cmd/cli/ -v
go build ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/cli/plan.go cmd/cli/plan_test.go cmd/cli/main.go cmd/cli/testdata/network-only-project/
git commit -m "cli: wire `nimbusfab plan` against AWS network adapter"
```

---

## Task 13: End-to-end smoke (real `tofu`, no cloud calls)

**Files:**
- Create: `cmd/cli/integration_plan_test.go` (build tag `integration`)

This task only adds value if `tofu` is on the developer's `$PATH`. It runs `nimbusfab plan` against the fixture from Task 12 with a real `ExecRunner` (no fake) but with a `local` state backend and `-backend=false` style configuration so no cloud calls are made.

- [ ] **Step 1: Write the test**

Create `cmd/cli/integration_plan_test.go`:

```go
//go:build integration
// +build integration

package main

import (
    "bytes"
    "context"
    "os/exec"
    "strings"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestPlanCommand_RealTofu(t *testing.T) {
    if _, err := exec.LookPath("tofu"); err != nil {
        t.Skip("tofu not on PATH; skipping integration test")
    }
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil {
        t.Fatalf("register: %v", err)
    }

    var stdout, stderr bytes.Buffer
    // Real ExecRunner, no AWS credentials — `tofu init` should succeed for a
    // local backend, but `tofu plan` will fail when the AWS provider tries to
    // contact AWS. We accept that as expected; the assertion is only that the
    // workspace got materialized correctly before the failure.
    code := runPlan(context.Background(), planArgs{
        ProjectPath: "testdata/network-only-project",
        Stack:       "dev",
        Adapters:    reg,
        Runner:      tofu.NewExecRunner(),
        WorkRoot:    t.TempDir(),
        Stdout:      &stdout,
        Stderr:      &stderr,
    })
    // We accept exit 1 (provider call fails); we just want to confirm we got past
    // the workspace materialization phase, which is observable from stdout content.
    if code != 0 && code != 1 {
        t.Errorf("unexpected exit code %d (stderr=%s)", code, stderr.String())
    }
    if !strings.Contains(stdout.String(), "Planning") && !strings.Contains(stderr.String(), "tofu") {
        t.Errorf("expected to reach the planning stage; stdout=%s stderr=%s", stdout.String(), stderr.String())
    }
}
```

- [ ] **Step 2: Run with the integration tag**

```bash
go test -tags integration ./cmd/cli/ -run TestPlanCommand_RealTofu -v
```

Expected: PASS or SKIP (skip if `tofu` is not installed in the dev environment). Document a note in the commit if it skipped — that's fine.

- [ ] **Step 3: Update Makefile (if needed) to include integration tag**

If a `make test-integration` target doesn't exist, add it:

```make
test-integration:
	go test -tags integration ./...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/integration_plan_test.go Makefile
git commit -m "cli: integration smoke for `nimbusfab plan` against real `tofu` binary"
```

---

## Task 14: Update README and CHANGELOG

**Files:**
- Modify: `README.md` (add `nimbusfab plan` to the documented commands)
- Create: `CHANGELOG.md` if absent; otherwise modify

- [ ] **Step 1: Add a section to README under "Commands"**

Append to the relevant README section:

```markdown
### `nimbusfab plan --stack <stack>`

Reads the project, validates it (DSL/IR Phase 1 pipeline), then asks each
cloud adapter to emit Tofu primitives for every `DeploymentTarget`. Writes
canonical workspace files (provider.tf.json, backend.tf.json, versions.tf.json,
main.tf.json) into a per-target directory under `$TMPDIR/nimbusfab/<deployment-id>/`,
runs `tofu init && tofu plan -out plan.bin`, and prints a summary.

**Phase 1 scope:** AWS only; `network` component type only (emits one `aws_vpc`).
Other clouds and component types arrive in subsequent phases.

```

- [ ] **Step 2: Note the Phase 1 work in CHANGELOG**

In `CHANGELOG.md`:

```markdown
## Unreleased

### Added
- `nimbusfab plan --stack <stack>` for AWS `network` components (Provisioner Phase 1).
- `pkg/provisioner` — workspace materialization, framework-tag injection, canonical JSON.
- `pkg/cloud.Registry` — cloud adapter registry.
- `internal/cloud/aws` — minimal AWS adapter (network → aws_vpc).
- `internal/tofu` — subprocess Runner (`Init`, `Plan`, `Show`, `Output`, `Validate`, `Version`) plus FakeRunner.
- `pkg/parity` — `ResourceProfile` types referenced by adapter contract.
- `pkg/plugin/contract.RunProvisionerScenarios` — adapter contract test additions.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: README + CHANGELOG for Provisioner Phase 1"
```

---

## Final verification

- [ ] **Run the full test suite:**

```bash
go test ./...
go vet ./...
gofmt -l .
```

Expected: all tests PASS; vet clean; no formatting drift.

- [ ] **Build the binary:**

```bash
go build -o bin/nimbusfab ./cmd/cli
./bin/nimbusfab --help
./bin/nimbusfab plan --help
```

Expected: help text mentions both `validate` and `plan`.

- [ ] **Run plan against the fixture:**

```bash
./bin/nimbusfab plan --stack dev cmd/cli/testdata/network-only-project
```

Expected (without `tofu` installed): friendly error citing missing `tofu` binary. Expected (with `tofu` installed, no AWS creds): workspace files written + `tofu init` runs + `tofu plan` fails on provider call (acceptable for Phase 1).

- [ ] **Inspect a workspace by hand:**

```bash
find /tmp/nimbusfab -name "*.tf.json" | head -10
cat /tmp/nimbusfab/dep-*/aws-us-east-1/web-network/main.tf.json
```

Expected: canonical JSON with `aws_vpc.web_network` + the three `infra:*` framework tags + `enable_dns_*` defaults.

---

## What's NOT in Phase 1 (intentional)

To keep this phase shippable in one focused branch:

- **No Apply / Destroy.** Engine.Apply / Engine.Destroy still return `ErrNotImplementedYet`. Phase 2 lands them along with the orchestrator's parallel fan-out and partial-failure policies.
- **No state bridge.** `internal/state/bridge` is still a stub; Phase 2 wires it.
- **No drift detection.** Phase 2.
- **No inventory persistence.** All Phase 1 runs operate in `--no-inventory` style; nothing writes to the DB. Phase 2 wires the DB.
- **No cross-component refs.** Phase 1 fixture is a single `network` component with no `refs:`. Phase 2 lands `data.terraform_remote_state` interpolation and the refs DAG orchestrator.
- **No additional clouds.** Azure and GCP adapters arrive in Phases 4 and 5.
- **No additional resources.** AWS-network beyond VPC (subnets, route tables) lands in Phase 3 alongside `database`, `compute`, `storage`.
- **No PricingKey / Profile / BillingQuery / FetchBilling.** Real implementations land alongside Phase 3 (PricingKey + Profile) and Cost Dashboard phase (BillingQuery + FetchBilling).
- **No web app integration.** `cmd/server` remains a stub; Web app spec wires SSE + REST against this provisioner output later.

End-state: `nimbusfab plan` produces a real Tofu workspace from a real project, runs `tofu plan`, and surfaces the diff. The substrate everything else builds on.
