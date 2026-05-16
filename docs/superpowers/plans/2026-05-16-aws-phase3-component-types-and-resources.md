# AWS Phase 3 Plan — v1 Component Types + AWS Resource Expansion

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Concrete `components.Type` implementations for `network`, `compute`, `database`, `storage` (with `SpecSchema` + `Outputs`), plus AWS adapter expansion so `Emit()` dispatches on component type and produces a realistic primitive set per type (VPC + subnets + IGW + RTs for network; SG + instances for compute; subnet group + RDS for database; S3 bucket + secure defaults for storage). PricingKey + Profile populated per primitive — cost estimator + parity engine slot in later with zero adapter changes.

**Architecture:** Each new emit lives in its own file under `internal/cloud/aws/`. The adapter's `Emit()` becomes a thin dispatch on `target.Spec["__type"]` (a new field the provisioner stuffs alongside `__component`). A new `pkg/components/types.go` defines the four concrete `Type` values plus `DefaultRegistry()`. The engine defaults `Config.ComponentTypes` to `DefaultRegistry()` when nil, mirroring the nullRepo pattern.

**Tech Stack:** No new dependencies. Phase 3 is pure Go + tests.

**Conventions:**
- All paths relative to repo root `/home/kurt/git/nimbusfab-aws-phase3/`.
- Run all `go` commands with `PATH=$HOME/.local/go/bin:$PATH`.
- Bash CWD persists; we stay in the worktree throughout.
- One commit per task; commit messages follow `<area>: <imperative>`.

**Out of scope (deferred):**
- Validator Phase 4 (per-type SpecSchema validation in the validator pipeline). The Type values ship their schemas; wiring is its own validator phase.
- Cost estimator / parity engine wiring. This phase produces the data; consuming it is later.
- Tier-1 `<cloud>:` escape hatch wiring + tier-2 `raw:` merging. Schemas ship; wiring is later.
- LocalStack / real-cloud integration. Unit tests + golden files only.
- Azure / GCP adapters. Each is its own phase.

---

## Task 1: `components.Type` interface extension + scaffold

**Files:**
- Modify: `pkg/components/registry.go` (add `Outputs() map[string]OutputType` to `Type` interface; add `OutputType` struct)
- Create: `pkg/components/registry_test.go`

- [ ] **Step 1: Extend the interface**

In `pkg/components/registry.go`, add to the `Type` interface (after `SupportedClouds`):

```go
// Outputs declares what reference targets this type publishes.
Outputs() map[string]OutputType
```

Add the struct:

```go
// OutputType describes one declared output of a component type.
type OutputType struct {
    Kind        string  // "string" | "integer" | "boolean" | "list<string>" | "map<string,string>"
    Description string
}
```

- [ ] **Step 2: Test the interface contract on the existing in-memory registry**

Create `pkg/components/registry_test.go`:

```go
package components_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/components"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

type fakeType struct {
    name string
}

func (f fakeType) Name() string                                                                                                               { return f.name }
func (f fakeType) SpecSchema() []byte                                                                                                         { return []byte(`{}`) }
func (f fakeType) SupportedClouds() []string                                                                                                  { return []string{"aws"} }
func (f fakeType) Outputs() map[string]components.OutputType                                                                                  { return map[string]components.OutputType{} }
func (f fakeType) Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
    r := components.NewInMemoryRegistry()
    if err := r.Register(fakeType{name: "network"}); err != nil {
        t.Fatalf("Register: %v", err)
    }
    tp, ok := r.Type("network")
    if !ok || tp.Name() != "network" {
        t.Errorf("Type(network): ok=%v tp=%v", ok, tp)
    }
    if names := r.Types(); len(names) != 1 || names[0] != "network" {
        t.Errorf("Types(): %v", names)
    }
}

func TestRegistry_DuplicateRegister(t *testing.T) {
    r := components.NewInMemoryRegistry()
    _ = r.Register(fakeType{name: "x"})
    if err := r.Register(fakeType{name: "x"}); err == nil {
        t.Error("duplicate Register: nil err, want non-nil")
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/components/ -v
git add pkg/components/registry.go pkg/components/registry_test.go
git commit -m "components: add Outputs() to Type interface; cover registry round-trip"
```

---

## Task 2: Concrete v1 type values + `DefaultRegistry()`

**Files:**
- Create: `pkg/components/types.go`
- Create: `pkg/components/schema/v1alpha1/network.json`
- Create: `pkg/components/schema/v1alpha1/compute.json`
- Create: `pkg/components/schema/v1alpha1/database.json`
- Create: `pkg/components/schema/v1alpha1/storage.json`
- Create: `pkg/components/types_test.go`

- [ ] **Step 1: Write the four JSON Schemas**

Use the schemas from the spec (`docs/superpowers/specs/2026-05-16-aws-expansion-design.md` § "The four v1 component types"). Embed each:

```bash
mkdir -p pkg/components/schema/v1alpha1
```

Save the spec schemas verbatim as JSON. Example for `network.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://nimbusfab.dev/schema/components/v1alpha1/network.json",
  "title": "NetworkSpec",
  "type": "object",
  "required": ["cidr"],
  "additionalProperties": false,
  "properties": {
    "cidr": {
      "type": "string",
      "description": "IPv4 CIDR block for the VPC.",
      "pattern": "^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+/[0-9]+$"
    },
    "enableIPv6": { "type": "boolean", "default": false },
    "subnetCount": { "type": "integer", "minimum": 1, "maximum": 16, "default": 3 }
  }
}
```

Do similarly for compute / database / storage from the spec.

- [ ] **Step 2: Implement types.go**

Create `pkg/components/types.go`:

```go
package components

import (
    "context"
    _ "embed"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

//go:embed schema/v1alpha1/network.json
var networkSchema []byte

//go:embed schema/v1alpha1/compute.json
var computeSchema []byte

//go:embed schema/v1alpha1/database.json
var databaseSchema []byte

//go:embed schema/v1alpha1/storage.json
var storageSchema []byte

// builtinType is the common implementation for all four v1 types. The Type
// interface's Emit method delegates to the adapter; in-tree types don't add
// per-type Go logic — the adapter's Emit() handles dispatch.
type builtinType struct {
    name           string
    schema         []byte
    supportedClouds []string
    outputs        map[string]OutputType
}

func (t builtinType) Name() string                       { return t.name }
func (t builtinType) SpecSchema() []byte                 { return t.schema }
func (t builtinType) SupportedClouds() []string          { return t.supportedClouds }
func (t builtinType) Outputs() map[string]OutputType     { return t.outputs }

func (t builtinType) Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    // Delegate to the adapter, which dispatches on target.Spec["__type"].
    return adapter.Emit(ctx, target, refs)
}

// Network returns the built-in "network" component type.
func Network() Type {
    return builtinType{
        name: "network", schema: networkSchema,
        supportedClouds: []string{"aws", "azure", "gcp"},
        outputs: map[string]OutputType{
            "vpc_id":          {Kind: "string", Description: "The VPC ID"},
            "subnet_ids":      {Kind: "list<string>", Description: "Subnet IDs in declaration order"},
            "route_table_ids": {Kind: "list<string>", Description: "Route table IDs"},
        },
    }
}

// Compute returns the built-in "compute" component type.
func Compute() Type {
    return builtinType{
        name: "compute", schema: computeSchema,
        supportedClouds: []string{"aws", "azure", "gcp"},
        outputs: map[string]OutputType{
            "instance_ids":      {Kind: "list<string>", Description: "Instance IDs"},
            "private_ips":       {Kind: "list<string>", Description: "Primary private IP per instance"},
            "security_group_id": {Kind: "string", Description: "Security group / firewall ID"},
        },
    }
}

// Database returns the built-in "database" component type.
func Database() Type {
    return builtinType{
        name: "database", schema: databaseSchema,
        supportedClouds: []string{"aws", "azure", "gcp"},
        outputs: map[string]OutputType{
            "endpoint": {Kind: "string", Description: "DB endpoint hostname"},
            "port":     {Kind: "integer", Description: "DB port"},
            "db_name":  {Kind: "string", Description: "Default DB name"},
        },
    }
}

// Storage returns the built-in "storage" component type.
func Storage() Type {
    return builtinType{
        name: "storage", schema: storageSchema,
        supportedClouds: []string{"aws", "azure", "gcp"},
        outputs: map[string]OutputType{
            "bucket_name": {Kind: "string", Description: "S3 / GCS / Blob bucket name"},
            "bucket_arn":  {Kind: "string", Description: "Cloud-native bucket identifier (ARN / self-link / resource ID)"},
            "bucket_url":  {Kind: "string", Description: "HTTPS endpoint for the bucket"},
        },
    }
}

// DefaultRegistry returns a Registry populated with the four v1 component types.
func DefaultRegistry() Registry {
    r := NewInMemoryRegistry()
    _ = r.Register(Network())
    _ = r.Register(Compute())
    _ = r.Register(Database())
    _ = r.Register(Storage())
    return r
}
```

- [ ] **Step 3: Test the four types**

Create `pkg/components/types_test.go`:

```go
package components_test

import (
    "encoding/json"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/components"
)

func TestDefaultRegistry_HasAllFourTypes(t *testing.T) {
    r := components.DefaultRegistry()
    for _, name := range []string{"network", "compute", "database", "storage"} {
        if _, ok := r.Type(name); !ok {
            t.Errorf("DefaultRegistry missing type %q", name)
        }
    }
}

func TestTypes_SchemasAreValidJSON(t *testing.T) {
    for _, tp := range []components.Type{components.Network(), components.Compute(), components.Database(), components.Storage()} {
        var v any
        if err := json.Unmarshal(tp.SpecSchema(), &v); err != nil {
            t.Errorf("%s schema not valid JSON: %v", tp.Name(), err)
        }
    }
}

func TestTypes_OutputsDeclared(t *testing.T) {
    cases := map[string][]string{
        "network":  {"vpc_id", "subnet_ids", "route_table_ids"},
        "compute":  {"instance_ids", "private_ips", "security_group_id"},
        "database": {"endpoint", "port", "db_name"},
        "storage":  {"bucket_name", "bucket_arn", "bucket_url"},
    }
    r := components.DefaultRegistry()
    for typeName, wantOutputs := range cases {
        tp, _ := r.Type(typeName)
        outputs := tp.Outputs()
        for _, name := range wantOutputs {
            if _, ok := outputs[name]; !ok {
                t.Errorf("%s missing output %q", typeName, name)
            }
        }
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/components/ -v
git add pkg/components/types.go pkg/components/types_test.go pkg/components/schema/
git commit -m "components: concrete v1 types (network/compute/database/storage) with embedded SpecSchemas"
```

---

## Task 3: Wire `DefaultRegistry()` into the engine; provisioner passes `__type`

**Files:**
- Modify: `pkg/engine/config.go` (default `ComponentTypes` to `DefaultRegistry()` when nil)
- Modify: `pkg/provisioner/plan.go` (set `target.Spec["__type"] = comp.Type`)
- Create: `pkg/provisioner/types_dispatch_test.go`

- [ ] **Step 1: Default the ComponentTypes registry**

In `pkg/engine/config.go`, in `New()`:

```go
if cfg.ComponentTypes == nil {
    cfg.ComponentTypes = components.DefaultRegistry()
}
```

Import `"github.com/klehmer/nimbusfab/pkg/components"`.

- [ ] **Step 2: Provisioner stuffs `__type`**

In `pkg/provisioner/plan.go`'s `planOne`, immediately after `target.Spec["__component"] = comp.Name`:

```go
target.Spec["__type"] = comp.Type
```

- [ ] **Step 3: Test that the dispatch field reaches the adapter**

Create `pkg/provisioner/types_dispatch_test.go`:

```go
package provisioner_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/tofu"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/provisioner"
)

// captureAdapter wraps a FakeAdapter and records the target it sees.
type captureAdapter struct {
    *cloud.FakeAdapter
    captured ir.DeploymentTarget
}

func (c *captureAdapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    c.captured = target
    return c.FakeAdapter.Emit(ctx, target, refs)
}

func TestPlan_StuffsComponentTypeIntoSpec(t *testing.T) {
    cap := &captureAdapter{FakeAdapter: cloud.NewFakeAdapter("aws")}
    reg := cloud.NewRegistry()
    _ = reg.Register(cap)

    p, _ := provisioner.New(provisioner.Config{
        WorkRoot: t.TempDir(),
        Adapters: reg,
        Runner:   tofu.NewFakeRunner(),
    })
    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1, Name: "x",
        Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "orders-db", Type: "database",
            Spec: map[string]any{"engine": "postgres"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }
    _, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-t",
    })
    if err != nil {
        t.Fatalf("Plan: %v", err)
    }
    if cap.captured.Spec["__type"] != "database" {
        t.Errorf("__type = %v, want \"database\"", cap.captured.Spec["__type"])
    }
    if cap.captured.Spec["__component"] != "orders-db" {
        t.Errorf("__component = %v", cap.captured.Spec["__component"])
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/engine/ ./pkg/provisioner/ -v -run "TestPlan_StuffsComponentTypeIntoSpec|TestEngine_"
git add pkg/engine/config.go pkg/provisioner/plan.go pkg/provisioner/types_dispatch_test.go
git commit -m "engine+provisioner: default ComponentTypes registry; pass component type to adapter via __type"
```

---

## Task 4: AWS adapter Emit dispatch + scaffold for per-type files

**Files:**
- Modify: `internal/cloud/aws/emit.go` (replace single-type Emit with type-dispatching switch)
- Modify: `internal/cloud/aws/adapter.go` (`SupportedComponentTypes()` returns all four)
- Create: `internal/cloud/aws/dispatch_test.go`

- [ ] **Step 1: Replace `Emit` with a dispatch shim**

In `internal/cloud/aws/emit.go`, replace the existing single-type body. Keep `tofuIdentifier` and the existing identifier-mangling regex; move the network emit into a new file (Task 5). For Phase-3 Task 4 just rewrite `Emit` as:

```go
func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    compType, _ := target.Spec["__type"].(string)
    switch compType {
    case "network", "":  // empty = back-compat for tests that don't set __type
        return a.emitNetwork(ctx, target, refs)
    case "compute":
        return a.emitCompute(ctx, target, refs)
    case "database":
        return a.emitDatabase(ctx, target, refs)
    case "storage":
        return a.emitStorage(ctx, target, refs)
    default:
        return nil, fmt.Errorf("aws: unsupported component type %q", compType)
    }
}
```

Move the existing network logic to `internal/cloud/aws/network.go` as `emitNetwork`. Provide stub `emitCompute`/`emitDatabase`/`emitStorage` that return `nil, fmt.Errorf("aws: %s emit not yet implemented", compType)` — Tasks 6-8 land them.

- [ ] **Step 2: Update `SupportedComponentTypes`**

```go
func (*Adapter) SupportedComponentTypes() []string {
    return []string{"network", "compute", "database", "storage"}
}
```

- [ ] **Step 3: Test dispatch rejects unknown types**

Create `internal/cloud/aws/dispatch_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_Emit_UnsupportedType(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "exotic"},
    }
    _, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if err == nil {
        t.Error("Emit with unsupported type: nil err, want non-nil")
    }
}

func TestAdapter_SupportsAllFourTypes(t *testing.T) {
    a := aws.New()
    got := a.SupportedComponentTypes()
    want := map[string]bool{"network": true, "compute": true, "database": true, "storage": true}
    for _, name := range got {
        delete(want, name)
    }
    if len(want) != 0 {
        t.Errorf("missing types: %v", want)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v -run "TestAdapter_(Emit_Unsupported|SupportsAllFour)"
git add internal/cloud/aws/
git commit -m "aws: Emit dispatches on __type; SupportedComponentTypes returns all four"
```

---

## Task 5: Network emit expansion (VPC + subnets + IGW + RTs)

**Files:**
- Modify: `internal/cloud/aws/network.go` (extracted in Task 4; now expanded)
- Create: `internal/cloud/aws/network_test.go`
- Create: `internal/cloud/aws/testdata/network_full.golden.json`

- [ ] **Step 1: Write a failing golden-file test**

Create `internal/cloud/aws/testdata/network_full.golden.json` with the expected attribute maps for VPC + IGW + RT + 3 subnets + 3 RT associations (6 primitives total):

```json
{
  "primitives_by_type": {
    "aws_vpc": {
      "count": 1,
      "attribute_keys": ["cidr_block", "enable_dns_hostnames", "enable_dns_support"]
    },
    "aws_internet_gateway": {
      "count": 1,
      "attribute_keys": ["vpc_id"]
    },
    "aws_route_table": {
      "count": 1,
      "attribute_keys": ["vpc_id", "route"]
    },
    "aws_subnet": {
      "count": 3,
      "attribute_keys": ["vpc_id", "cidr_block", "availability_zone", "map_public_ip_on_launch"]
    },
    "aws_route_table_association": {
      "count": 3,
      "attribute_keys": ["subnet_id", "route_table_id"]
    }
  }
}
```

Create `internal/cloud/aws/network_test.go`:

```go
package aws_test

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "sort"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitNetwork_FullShape(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"__type": "network", "__component": "web-network", "cidr": "10.0.0.0/16"},
    }
    prims, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }

    byType := map[string]int{}
    keysByType := map[string]map[string]bool{}
    for _, p := range prims {
        byType[p.TofuType]++
        if keysByType[p.TofuType] == nil {
            keysByType[p.TofuType] = map[string]bool{}
        }
        for k := range p.Attributes {
            keysByType[p.TofuType][k] = true
        }
    }

    gold, err := os.ReadFile(filepath.Join("testdata", "network_full.golden.json"))
    if err != nil {
        t.Fatalf("read golden: %v", err)
    }
    var want struct {
        PrimitivesByType map[string]struct {
            Count        int      `json:"count"`
            AttributeKeys []string `json:"attribute_keys"`
        } `json:"primitives_by_type"`
    }
    _ = json.Unmarshal(gold, &want)

    for typeName, expected := range want.PrimitivesByType {
        if byType[typeName] != expected.Count {
            t.Errorf("%s count = %d, want %d", typeName, byType[typeName], expected.Count)
        }
        for _, key := range expected.AttributeKeys {
            if !keysByType[typeName][key] {
                got := []string{}
                for k := range keysByType[typeName] {
                    got = append(got, k)
                }
                sort.Strings(got)
                t.Errorf("%s missing attribute %q (have: %v)", typeName, key, got)
            }
        }
    }
}

func TestEmitNetwork_Deterministic(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
    }
    a1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    a2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    j1, _ := json.Marshal(a1)
    j2, _ := json.Marshal(a2)
    if string(j1) != string(j2) {
        t.Errorf("non-deterministic:\n%s\nvs\n%s", j1, j2)
    }
}

func TestEmitNetwork_CustomSubnetCount(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16", "subnetCount": 2},
    }
    prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    var subnets int
    for _, p := range prims {
        if p.TofuType == "aws_subnet" {
            subnets++
        }
    }
    if subnets != 2 {
        t.Errorf("subnet count = %d, want 2", subnets)
    }
}
```

- [ ] **Step 2: Implement `emitNetwork` in `network.go`**

Create `internal/cloud/aws/network.go`:

```go
package aws

import (
    "context"
    "fmt"
    "net"
    "strings"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    cidr, _ := target.Spec["cidr"].(string)
    if cidr == "" {
        cidr = "10.0.0.0/16"
    }
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "network"
    }
    subnetCount := intFromSpec(target.Spec, "subnetCount", 3)

    name := tofuIdentifier(component)
    azs := defaultAZsForRegion(target.Region, subnetCount)
    subnetCIDRs, err := splitCIDR(cidr, subnetCount)
    if err != nil {
        return nil, fmt.Errorf("aws.emitNetwork: %w", err)
    }

    out := []ir.ResourcePrimitive{
        {
            ID:       fmt.Sprintf("%s.aws-%s.vpc", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_vpc",
            TofuName: name,
            Attributes: map[string]any{
                "cidr_block":           cidr,
                "enable_dns_support":   true,
                "enable_dns_hostnames": true,
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.igw", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_internet_gateway",
            TofuName: name,
            Attributes: map[string]any{
                "vpc_id": "${aws_vpc." + name + ".id}",
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.rt", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_route_table",
            TofuName: name,
            Attributes: map[string]any{
                "vpc_id": "${aws_vpc." + name + ".id}",
                "route": []any{
                    map[string]any{
                        "cidr_block": "0.0.0.0/0",
                        "gateway_id": "${aws_internet_gateway." + name + ".id}",
                    },
                },
            },
        },
    }
    for i := 0; i < subnetCount; i++ {
        subnetName := fmt.Sprintf("%s_%d", name, i)
        out = append(out, ir.ResourcePrimitive{
            ID:       fmt.Sprintf("%s.aws-%s.subnet_%d", component, target.Region, i),
            Cloud:    "aws",
            TofuType: "aws_subnet",
            TofuName: subnetName,
            Attributes: map[string]any{
                "vpc_id":                  "${aws_vpc." + name + ".id}",
                "cidr_block":              subnetCIDRs[i],
                "availability_zone":       azs[i%len(azs)],
                "map_public_ip_on_launch": true,
            },
        })
        out = append(out, ir.ResourcePrimitive{
            ID:       fmt.Sprintf("%s.aws-%s.rta_%d", component, target.Region, i),
            Cloud:    "aws",
            TofuType: "aws_route_table_association",
            TofuName: subnetName,
            Attributes: map[string]any{
                "subnet_id":      "${aws_subnet." + subnetName + ".id}",
                "route_table_id": "${aws_route_table." + name + ".id}",
            },
        })
    }
    return out, nil
}

func intFromSpec(spec map[string]any, key string, def int) int {
    if v, ok := spec[key]; ok {
        switch t := v.(type) {
        case int:
            return t
        case int64:
            return int(t)
        case float64:
            return int(t)
        }
    }
    return def
}

func defaultAZsForRegion(region string, n int) []string {
    // Phase 3 covers the canonical "<region>{a,b,c}" trio that holds for
    // most production regions. Out-of-the-box regions get warned via a
    // (future) Validate() diagnostic.
    az := []string{region + "a", region + "b", region + "c", region + "d", region + "e"}
    if n > len(az) {
        n = len(az)
    }
    return az[:n]
}

func splitCIDR(parent string, n int) ([]string, error) {
    _, ipNet, err := net.ParseCIDR(parent)
    if err != nil {
        return nil, fmt.Errorf("invalid CIDR %q: %w", parent, err)
    }
    ones, bits := ipNet.Mask.Size()
    if bits != 32 {
        return nil, fmt.Errorf("only IPv4 supported in Phase 3 (got %q)", parent)
    }
    // For /16 parent + 3 subnets, we use /24 slices.
    newPrefix := ones + 8
    if newPrefix > 30 {
        newPrefix = ones + 4
    }
    out := make([]string, n)
    base := ipNet.IP.To4()
    if base == nil {
        return nil, fmt.Errorf("not IPv4 CIDR: %s", parent)
    }
    for i := 0; i < n; i++ {
        // For /16 -> /24, third octet bumps by i.
        ip := []byte{base[0], base[1], byte(i), 0}
        mask := net.CIDRMask(newPrefix, 32)
        subnet := &net.IPNet{IP: ip, Mask: mask}
        out[i] = subnet.String()
    }
    _ = strings.TrimSpace // silence import-not-used if refactored
    return out, nil
}
```

(Remove the now-redundant `emitNetwork`-equivalent code from `emit.go`; the original Phase-1 `Emit` body lives in `network.go` now.)

- [ ] **Step 3: Update existing emit_test.go golden file**

Phase 1's `internal/cloud/aws/emit_test.go` tested a single `aws_vpc` golden. Add `__type: network` to its test inputs (or rely on the back-compat empty `__type` case) and accept that Emit now returns 8 primitives instead of 1. Update assertions to check the first primitive is `aws_vpc`.

Modify `internal/cloud/aws/emit_test.go`:

```go
func TestAdapter_EmitNetworkVPC_Golden(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web-network", "__type": "network"},
    }
    primitives, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }
    if len(primitives) < 1 {
        t.Fatalf("Emit returned 0 primitives")
    }
    var vpc *ir.ResourcePrimitive
    for i := range primitives {
        if primitives[i].TofuType == "aws_vpc" {
            vpc = &primitives[i]
            break
        }
    }
    if vpc == nil {
        t.Fatalf("no aws_vpc in primitives: %v", primitives)
    }
    if got := vpc.Attributes["cidr_block"]; got != "10.0.0.0/16" {
        t.Errorf("cidr_block = %v, want 10.0.0.0/16", got)
    }
    // (Drop the byte-for-byte golden check; network emit is multi-primitive now.)
}
```

`TestAdapter_EmitIsPure` and `TestAdapter_EmitTofuNameIsSafe` continue to work — they just see the first primitive (VPC) in the array.

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v
git add internal/cloud/aws/
git commit -m "aws: emitNetwork expands to VPC + IGW + RT + N subnets + RT associations"
```

---

## Task 6: Database emit (DB subnet group + RDS instance) + sizing

**Files:**
- Create: `internal/cloud/aws/database.go`
- Create: `internal/cloud/aws/database_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/cloud/aws/database_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitDatabase_BasicShape(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud:  "aws",
        Region: "us-east-1",
        Spec: map[string]any{
            "__type": "database", "__component": "orders-db",
            "engine": "postgres", "size": "small",
        },
    }
    refs := cloud.ResolvedRefs{
        "vpcId":     "vpc-0abc",
        "subnetIds": []string{"subnet-1", "subnet-2"},
    }
    prims, err := a.Emit(context.Background(), target, refs)
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }
    if len(prims) != 2 {
        t.Fatalf("got %d primitives, want 2", len(prims))
    }
    var sg, db *ir.ResourcePrimitive
    for i := range prims {
        switch prims[i].TofuType {
        case "aws_db_subnet_group":
            sg = &prims[i]
        case "aws_db_instance":
            db = &prims[i]
        }
    }
    if sg == nil || db == nil {
        t.Fatalf("missing primitives: %+v", prims)
    }
    if db.Attributes["instance_class"] != "db.t3.small" {
        t.Errorf("instance_class = %v", db.Attributes["instance_class"])
    }
    if db.Attributes["engine"] != "postgres" {
        t.Errorf("engine = %v", db.Attributes["engine"])
    }
    if db.Attributes["allocated_storage"] != 100 {
        t.Errorf("allocated_storage = %v, want 100", db.Attributes["allocated_storage"])
    }
}

func TestEmitDatabase_SizeMapping(t *testing.T) {
    cases := map[string]struct {
        instanceClass string
        storage       int
    }{
        "small":  {"db.t3.small", 100},
        "medium": {"db.t3.medium", 250},
        "large":  {"db.m6i.large", 500},
        "xlarge": {"db.m6i.xlarge", 1000},
    }
    a := aws.New()
    for size, want := range cases {
        prims, err := a.Emit(context.Background(), ir.DeploymentTarget{
            Cloud: "aws", Region: "us-east-1",
            Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": size},
        }, cloud.ResolvedRefs{"subnetIds": []string{"subnet-1"}})
        if err != nil {
            t.Errorf("%s: Emit: %v", size, err)
            continue
        }
        for _, p := range prims {
            if p.TofuType == "aws_db_instance" {
                if p.Attributes["instance_class"] != want.instanceClass {
                    t.Errorf("%s: instance_class = %v, want %v", size, p.Attributes["instance_class"], want.instanceClass)
                }
                if p.Attributes["allocated_storage"] != want.storage {
                    t.Errorf("%s: storage = %v, want %v", size, p.Attributes["allocated_storage"], want.storage)
                }
            }
        }
    }
}

func TestEmitDatabase_UnknownEngine(t *testing.T) {
    a := aws.New()
    _, err := a.Emit(context.Background(), ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "database", "__component": "db", "engine": "oracle", "size": "small"},
    }, cloud.ResolvedRefs{"subnetIds": []string{"s"}})
    if err == nil {
        t.Error("expected error for unsupported engine")
    }
}
```

- [ ] **Step 2: Implement `emitDatabase`**

Create `internal/cloud/aws/database.go`:

```go
package aws

import (
    "context"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

type dbSizeProfile struct {
    InstanceClass string
    StorageGB     int
    VCPU          int
    MemoryGB      float64
}

var dbSizes = map[string]dbSizeProfile{
    "small":  {InstanceClass: "db.t3.small", StorageGB: 100, VCPU: 2, MemoryGB: 2},
    "medium": {InstanceClass: "db.t3.medium", StorageGB: 250, VCPU: 2, MemoryGB: 4},
    "large":  {InstanceClass: "db.m6i.large", StorageGB: 500, VCPU: 2, MemoryGB: 8},
    "xlarge": {InstanceClass: "db.m6i.xlarge", StorageGB: 1000, VCPU: 4, MemoryGB: 16},
}

var dbEngineDefaults = map[string]string{
    "postgres": "16",
    "mysql":    "8.0",
    "mariadb":  "10.11",
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "database"
    }
    name := tofuIdentifier(component)

    engine, _ := target.Spec["engine"].(string)
    if engine == "" {
        return nil, fmt.Errorf("aws.emitDatabase: spec.engine required")
    }
    defaultVer, ok := dbEngineDefaults[engine]
    if !ok {
        return nil, fmt.Errorf("aws.emitDatabase: unsupported engine %q (supported: postgres, mysql, mariadb)", engine)
    }
    version, _ := target.Spec["version"].(string)
    if version == "" {
        version = defaultVer
    }

    profile, err := resolveDBSize(target.Spec)
    if err != nil {
        return nil, fmt.Errorf("aws.emitDatabase: %w", err)
    }

    subnetIDs := subnetIDsFromRefs(refs, component)
    multiAZ := boolFromSpec(target.Spec, "multiAZ", false)
    backupRetention := 7
    if !boolFromSpec(target.Spec, "pointInTimeRestore", true) {
        backupRetention = 0
    }

    return []ir.ResourcePrimitive{
        {
            ID:       fmt.Sprintf("%s.aws-%s.subnet_group", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_db_subnet_group",
            TofuName: name,
            Attributes: map[string]any{
                "name":       name + "-subnet-group",
                "subnet_ids": subnetIDs,
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.db", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_db_instance",
            TofuName: name,
            Attributes: map[string]any{
                "identifier":              name,
                "engine":                  engine,
                "engine_version":          version,
                "instance_class":          profile.InstanceClass,
                "allocated_storage":       profile.StorageGB,
                "storage_type":            "gp3",
                "storage_encrypted":       true,
                "db_subnet_group_name":    "${aws_db_subnet_group." + name + ".name}",
                "multi_az":                multiAZ,
                "backup_retention_period": backupRetention,
                "skip_final_snapshot":     true,
                "publicly_accessible":     false,
            },
        },
    }, nil
}

func resolveDBSize(spec map[string]any) (dbSizeProfile, error) {
    if size, ok := spec["size"].(string); ok && size != "" {
        profile, ok := dbSizes[size]
        if !ok {
            return dbSizeProfile{}, fmt.Errorf("unknown size %q (use small/medium/large/xlarge)", size)
        }
        if explicitCompute, hasC := spec["compute"]; hasC && explicitCompute != nil {
            return dbSizeProfile{}, fmt.Errorf("size and compute are mutually exclusive")
        }
        return profile, nil
    }
    // Explicit compute path.
    compute, _ := spec["compute"].(map[string]any)
    if compute == nil {
        return dbSizeProfile{}, fmt.Errorf("spec.size or spec.compute required")
    }
    vcpu := intFromMap(compute, "vCPU", 0)
    memGB := floatFromMap(compute, "memoryGB", 0)
    if vcpu == 0 || memGB == 0 {
        return dbSizeProfile{}, fmt.Errorf("compute.vCPU and compute.memoryGB required")
    }
    // Phase 3 picks the smallest matching T-shirt size; this is the same
    // resolution path the cost estimator uses later. Find smallest matching
    // profile by VCPU + memory.
    for _, sz := range []string{"small", "medium", "large", "xlarge"} {
        p := dbSizes[sz]
        if p.VCPU >= vcpu && p.MemoryGB >= memGB {
            storage := intFromMap(spec["storage"], "sizeGB", p.StorageGB)
            p.StorageGB = storage
            return p, nil
        }
    }
    return dbSizeProfile{}, fmt.Errorf("no T-shirt size satisfies vCPU>=%d memoryGB>=%v", vcpu, memGB)
}

func subnetIDsFromRefs(refs cloud.ResolvedRefs, fallbackComp string) []any {
    if v, ok := refs["subnetIds"]; ok {
        switch t := v.(type) {
        case []string:
            out := make([]any, len(t))
            for i, s := range t {
                out[i] = s
            }
            return out
        case []any:
            return t
        }
    }
    // Fallback: assume an upstream component with the conventional name.
    // Phase 3 emits a literal interpolation that resolves at Tofu time.
    return []any{"${data.terraform_remote_state." + fallbackComp + ".outputs.subnet_ids}"}
}

func boolFromSpec(spec map[string]any, key string, def bool) bool {
    if v, ok := spec[key].(bool); ok {
        return v
    }
    return def
}

func intFromMap(m any, key string, def int) int {
    asMap, _ := m.(map[string]any)
    if asMap == nil {
        return def
    }
    if v, ok := asMap[key]; ok {
        switch t := v.(type) {
        case int:
            return t
        case int64:
            return int(t)
        case float64:
            return int(t)
        }
    }
    return def
}

func floatFromMap(m map[string]any, key string, def float64) float64 {
    if v, ok := m[key]; ok {
        switch t := v.(type) {
        case float64:
            return t
        case int:
            return float64(t)
        case int64:
            return float64(t)
        }
    }
    return def
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v -run TestEmitDatabase
git add internal/cloud/aws/database.go internal/cloud/aws/database_test.go
git commit -m "aws: emitDatabase -> db_subnet_group + db_instance with T-shirt sizing for postgres/mysql/mariadb"
```

---

## Task 7: Compute emit (security group + instances) + sizing

**Files:**
- Create: `internal/cloud/aws/compute.go`
- Create: `internal/cloud/aws/compute_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/cloud/aws/compute_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitCompute_BasicShape(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{
            "__type": "compute", "__component": "web",
            "size":     "medium",
            "replicas": 2,
        },
    }
    refs := cloud.ResolvedRefs{
        "vpcId":    "vpc-0abc",
        "subnetId": "subnet-1",
    }
    prims, err := a.Emit(context.Background(), target, refs)
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }
    var sg int
    var instances int
    for _, p := range prims {
        switch p.TofuType {
        case "aws_security_group":
            sg++
        case "aws_instance":
            instances++
        }
    }
    if sg != 1 {
        t.Errorf("security groups = %d, want 1", sg)
    }
    if instances != 2 {
        t.Errorf("instances = %d, want 2 (replicas)", instances)
    }
}

func TestEmitCompute_SizeMapping(t *testing.T) {
    cases := map[string]string{
        "small":  "t3.small",
        "medium": "t3.medium",
        "large":  "t3.large",
        "xlarge": "t3.xlarge",
    }
    a := aws.New()
    for size, want := range cases {
        prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
            Cloud: "aws", Region: "us-east-1",
            Spec: map[string]any{"__type": "compute", "__component": "x", "size": size},
        }, cloud.ResolvedRefs{"subnetId": "subnet-1"})
        for _, p := range prims {
            if p.TofuType == "aws_instance" {
                if p.Attributes["instance_type"] != want {
                    t.Errorf("%s: instance_type = %v, want %v", size, p.Attributes["instance_type"], want)
                }
            }
        }
    }
}

func TestEmitCompute_AMIPickedPerRegion(t *testing.T) {
    a := aws.New()
    for _, region := range []string{"us-east-1", "us-west-2", "eu-west-1"} {
        prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
            Cloud: "aws", Region: region,
            Spec: map[string]any{"__type": "compute", "__component": "x", "size": "small"},
        }, cloud.ResolvedRefs{"subnetId": "subnet-1"})
        for _, p := range prims {
            if p.TofuType == "aws_instance" {
                ami, _ := p.Attributes["ami"].(string)
                if ami == "" {
                    t.Errorf("%s: empty AMI", region)
                }
            }
        }
    }
}
```

- [ ] **Step 2: Implement `emitCompute`**

Create `internal/cloud/aws/compute.go`:

```go
package aws

import (
    "context"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

type computeSizeProfile struct {
    InstanceType string
    VCPU         int
    MemoryGB     float64
}

var computeSizes = map[string]computeSizeProfile{
    "small":  {"t3.small", 2, 2},
    "medium": {"t3.medium", 2, 4},
    "large":  {"t3.large", 2, 8},
    "xlarge": {"t3.xlarge", 4, 16},
}

// Default Amazon Linux 2023 AMI per region (Phase 3; refresh on AMI rotation).
// Values are deliberately stable strings rather than a runtime lookup so emit
// is pure and deterministic.
var amazonLinux2023AMI = map[string]string{
    "us-east-1":      "ami-0c80e2b6cccc3a73c",
    "us-east-2":      "ami-08d8b2eb8bc7e5d2c",
    "us-west-1":      "ami-0c5fa1d2afb39dabe",
    "us-west-2":      "ami-093a4ad9a8cc370f4",
    "eu-west-1":      "ami-0eed1c915ea891ace",
    "eu-central-1":   "ami-04a59bc910beb6f9d",
    "ap-northeast-1": "ami-0c0a44d3a8df36c0e",
    "ap-southeast-2": "ami-0e0aa808e23c2735c",
}

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "compute"
    }
    name := tofuIdentifier(component)
    replicas := intFromSpec(target.Spec, "replicas", 1)

    instanceType, err := resolveComputeSize(target.Spec)
    if err != nil {
        return nil, fmt.Errorf("aws.emitCompute: %w", err)
    }

    ami, _ := target.Spec["imageRef"].(string)
    if ami == "" {
        if v, ok := amazonLinux2023AMI[target.Region]; ok {
            ami = v
        } else {
            return nil, fmt.Errorf("aws.emitCompute: no default AMI for region %q (specify spec.imageRef)", target.Region)
        }
    }

    subnetID := stringFromRefs(refs, "subnetId", "${data.terraform_remote_state."+tofuIdentifier(component)+".outputs.subnet_ids[0]}")
    storageGB := intFromMap(target.Spec["storage"], "sizeGB", 30)

    out := []ir.ResourcePrimitive{
        {
            ID:       fmt.Sprintf("%s.aws-%s.sg", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_security_group",
            TofuName: name,
            Attributes: map[string]any{
                "name":        name + "-sg",
                "description": "Default SG for " + component,
                "vpc_id":      stringFromRefs(refs, "vpcId", "${data.terraform_remote_state."+tofuIdentifier(component)+".outputs.vpc_id}"),
                "egress": []any{
                    map[string]any{
                        "from_port":   0,
                        "to_port":     0,
                        "protocol":    "-1",
                        "cidr_blocks": []any{"0.0.0.0/0"},
                    },
                },
            },
        },
    }
    for i := 0; i < replicas; i++ {
        instanceName := fmt.Sprintf("%s_%d", name, i)
        out = append(out, ir.ResourcePrimitive{
            ID:       fmt.Sprintf("%s.aws-%s.instance_%d", component, target.Region, i),
            Cloud:    "aws",
            TofuType: "aws_instance",
            TofuName: instanceName,
            Attributes: map[string]any{
                "ami":                    ami,
                "instance_type":          instanceType,
                "subnet_id":              subnetID,
                "vpc_security_group_ids": []any{"${aws_security_group." + name + ".id}"},
                "root_block_device": []any{
                    map[string]any{
                        "volume_size": storageGB,
                        "volume_type": "gp3",
                        "encrypted":   true,
                    },
                },
            },
        })
    }
    return out, nil
}

func resolveComputeSize(spec map[string]any) (string, error) {
    if size, ok := spec["size"].(string); ok && size != "" {
        p, ok := computeSizes[size]
        if !ok {
            return "", fmt.Errorf("unknown size %q", size)
        }
        if _, hasC := spec["compute"]; hasC {
            return "", fmt.Errorf("size and compute are mutually exclusive")
        }
        return p.InstanceType, nil
    }
    compute, _ := spec["compute"].(map[string]any)
    if compute == nil {
        return "", fmt.Errorf("spec.size or spec.compute required")
    }
    vcpu := intFromMap(compute, "vCPU", 0)
    memGB := floatFromMap(compute, "memoryGB", 0)
    for _, sz := range []string{"small", "medium", "large", "xlarge"} {
        p := computeSizes[sz]
        if p.VCPU >= vcpu && p.MemoryGB >= memGB {
            return p.InstanceType, nil
        }
    }
    return "", fmt.Errorf("no T-shirt size satisfies vCPU>=%d memoryGB>=%v", vcpu, memGB)
}

func stringFromRefs(refs cloud.ResolvedRefs, key, fallback string) string {
    if v, ok := refs[key].(string); ok && v != "" {
        return v
    }
    return fallback
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v -run TestEmitCompute
git add internal/cloud/aws/compute.go internal/cloud/aws/compute_test.go
git commit -m "aws: emitCompute -> security_group + N aws_instances with T-shirt sizing and per-region AMIs"
```

---

## Task 8: Storage emit (S3 bucket + secure defaults)

**Files:**
- Create: `internal/cloud/aws/storage.go`
- Create: `internal/cloud/aws/storage_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/cloud/aws/storage_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitStorage_BasicShape(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "storage", "__component": "uploads"},
    }
    prims, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    if err != nil {
        t.Fatalf("Emit: %v", err)
    }
    var bucket, versioning, pab, enc int
    for _, p := range prims {
        switch p.TofuType {
        case "aws_s3_bucket":
            bucket++
        case "aws_s3_bucket_versioning":
            versioning++
        case "aws_s3_bucket_public_access_block":
            pab++
        case "aws_s3_bucket_server_side_encryption_configuration":
            enc++
        }
    }
    if bucket != 1 || versioning != 1 || pab != 1 || enc != 1 {
        t.Errorf("primitive counts: bucket=%d versioning=%d pab=%d enc=%d", bucket, versioning, pab, enc)
    }
}

func TestEmitStorage_BucketNameDeterministic(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "storage", "__component": "uploads"},
    }
    p1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    p2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    var n1, n2 string
    for _, p := range p1 {
        if p.TofuType == "aws_s3_bucket" {
            n1 = p.Attributes["bucket"].(string)
        }
    }
    for _, p := range p2 {
        if p.TofuType == "aws_s3_bucket" {
            n2 = p.Attributes["bucket"].(string)
        }
    }
    if n1 == "" || n1 != n2 {
        t.Errorf("bucket name non-deterministic: %q vs %q", n1, n2)
    }
}

func TestEmitStorage_ExplicitName(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "storage", "__component": "uploads", "name": "my-explicit-bucket"},
    }
    prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    for _, p := range prims {
        if p.TofuType == "aws_s3_bucket" {
            if p.Attributes["bucket"] != "my-explicit-bucket" {
                t.Errorf("bucket = %v, want my-explicit-bucket", p.Attributes["bucket"])
            }
        }
    }
}

func TestEmitStorage_PublicAccessAllowed(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "storage", "__component": "uploads", "publicAccess": "allowed"},
    }
    prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
    for _, p := range prims {
        if p.TofuType == "aws_s3_bucket_public_access_block" {
            if p.Attributes["block_public_acls"] != false {
                t.Errorf("publicAccess=allowed should set block_public_acls=false; got %v", p.Attributes["block_public_acls"])
            }
        }
    }
}
```

- [ ] **Step 2: Implement `emitStorage`**

Create `internal/cloud/aws/storage.go`:

```go
package aws

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "storage"
    }
    name := tofuIdentifier(component)

    bucketName, _ := target.Spec["name"].(string)
    if bucketName == "" {
        bucketName = deriveBucketName(component, target.Region)
    }

    versioning := boolFromSpec(target.Spec, "versioning", true)
    publicAccess, _ := target.Spec["publicAccess"].(string)
    blockPublic := publicAccess != "allowed"

    encAlgo := "AES256"
    if enc, ok := target.Spec["encryption"].(map[string]any); ok {
        if a, ok := enc["algorithm"].(string); ok && a != "" {
            encAlgo = a
        }
    }

    return []ir.ResourcePrimitive{
        {
            ID:       fmt.Sprintf("%s.aws-%s.bucket", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_s3_bucket",
            TofuName: name,
            Attributes: map[string]any{
                "bucket": bucketName,
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.versioning", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_s3_bucket_versioning",
            TofuName: name,
            Attributes: map[string]any{
                "bucket": "${aws_s3_bucket." + name + ".id}",
                "versioning_configuration": []any{map[string]any{
                    "status": versioningStatus(versioning),
                }},
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.public_access_block", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_s3_bucket_public_access_block",
            TofuName: name,
            Attributes: map[string]any{
                "bucket":                  "${aws_s3_bucket." + name + ".id}",
                "block_public_acls":       blockPublic,
                "block_public_policy":     blockPublic,
                "ignore_public_acls":      blockPublic,
                "restrict_public_buckets": blockPublic,
            },
        },
        {
            ID:       fmt.Sprintf("%s.aws-%s.encryption", component, target.Region),
            Cloud:    "aws",
            TofuType: "aws_s3_bucket_server_side_encryption_configuration",
            TofuName: name,
            Attributes: map[string]any{
                "bucket": "${aws_s3_bucket." + name + ".id}",
                "rule": []any{map[string]any{
                    "apply_server_side_encryption_by_default": []any{map[string]any{
                        "sse_algorithm": encAlgo,
                    }},
                }},
            },
        },
    }, nil
}

func versioningStatus(enabled bool) string {
    if enabled {
        return "Enabled"
    }
    return "Suspended"
}

// deriveBucketName produces a deterministic, S3-legal bucket name from the
// component name + region. Format: <component>-<8-char-sha256-prefix>.
// Total length is bounded; S3 requires <=63 chars, lowercase, no underscores.
func deriveBucketName(component, region string) string {
    sum := sha256.Sum256([]byte(component + ":" + region))
    suffix := hex.EncodeToString(sum[:])[:8]
    safe := tofuIdentifier(component) // replaces dashes; lowercase
    // For S3, dashes are allowed and preferred. Convert underscores back.
    out := ""
    for _, c := range safe {
        if c == '_' {
            out += "-"
        } else {
            out += string(c)
        }
    }
    if len(out) > 50 {
        out = out[:50]
    }
    return out + "-" + suffix
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v -run TestEmitStorage
git add internal/cloud/aws/storage.go internal/cloud/aws/storage_test.go
git commit -m "aws: emitStorage -> S3 bucket + versioning + public-access-block + SSE with secure defaults"
```

---

## Task 9: PricingKey + Profile implementations

**Files:**
- Create: `internal/cloud/aws/pricing.go`
- Create: `internal/cloud/aws/pricing_test.go`
- Create: `internal/cloud/aws/profile.go`
- Create: `internal/cloud/aws/profile_test.go`
- Modify: `internal/cloud/aws/adapter.go` (remove the `ErrNotImplementedYet` stubs for PricingKey / Profile)

- [ ] **Step 1: Move Profile + PricingKey out of adapter.go**

In `internal/cloud/aws/adapter.go`, delete the `Profile` and `PricingKey` methods — they're replaced by the new files.

- [ ] **Step 2: Implement pricing.go**

Create `internal/cloud/aws/pricing.go`:

```go
package aws

import (
    "context"

    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
    region := regionFromID(p.ID)
    switch p.TofuType {
    case "aws_instance":
        instanceType, _ := p.Attributes["instance_type"].(string)
        return map[string]any{
            "service":         "AmazonEC2",
            "instanceType":    instanceType,
            "region":          region,
            "tenancy":         "Shared",
            "operatingSystem": "Linux",
            "preInstalledSw":  "NA",
            "capacitystatus":  "Used",
        }, nil
    case "aws_db_instance":
        engine, _ := p.Attributes["engine"].(string)
        deployment := "Single-AZ"
        if mz, _ := p.Attributes["multi_az"].(bool); mz {
            deployment = "Multi-AZ"
        }
        instanceClass, _ := p.Attributes["instance_class"].(string)
        return map[string]any{
            "service":          "AmazonRDS",
            "instanceType":     instanceClass,
            "region":           region,
            "engineCode":       engine,
            "deploymentOption": deployment,
            "licenseModel":     "No license required",
        }, nil
    case "aws_s3_bucket":
        return map[string]any{
            "service":      "AmazonS3",
            "region":       region,
            "storageClass": "Standard",
        }, nil
    default:
        // Free primitives (vpc / subnet / igw / rt / sg / etc.).
        return nil, nil
    }
}

// regionFromID extracts the region from a primitive ID like
// "<component>.aws-<region>.<localname>".
func regionFromID(id string) string {
    // Defensive parsing; primitives without an embedded region get "".
    for i := 0; i < len(id); i++ {
        if i+4 < len(id) && id[i:i+5] == ".aws-" {
            rest := id[i+5:]
            for j := 0; j < len(rest); j++ {
                if rest[j] == '.' {
                    return rest[:j]
                }
            }
            return rest
        }
    }
    return ""
}
```

- [ ] **Step 3: Implement profile.go**

Create `internal/cloud/aws/profile.go`:

```go
package aws

import (
    "context"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/parity"
)

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
    switch p.TofuType {
    case "aws_vpc":
        cidr, _ := p.Attributes["cidr_block"].(string)
        return parity.ResourceProfile{
            Class:   "network",
            Network: &parity.NetworkProfile{CIDR: cidr, IPv6: false, NAT: false},
            SKU:     "aws_vpc",
        }, nil
    case "aws_instance":
        instanceType, _ := p.Attributes["instance_type"].(string)
        compute := lookupComputeProfile(instanceType)
        storage := storageProfileFromRootBlock(p.Attributes["root_block_device"])
        return parity.ResourceProfile{
            Class:   "compute",
            Compute: &compute,
            Storage: &storage,
            SKU:     instanceType,
        }, nil
    case "aws_db_instance":
        engine, _ := p.Attributes["engine"].(string)
        version, _ := p.Attributes["engine_version"].(string)
        instanceClass, _ := p.Attributes["instance_class"].(string)
        compute := lookupComputeProfile(stripDBPrefix(instanceClass))
        storageGB := 0
        switch t := p.Attributes["allocated_storage"].(type) {
        case int:
            storageGB = t
        case float64:
            storageGB = int(t)
        }
        multiAZ, _ := p.Attributes["multi_az"].(bool)
        backupRetention := 0
        switch t := p.Attributes["backup_retention_period"].(type) {
        case int:
            backupRetention = t
        case float64:
            backupRetention = int(t)
        }
        return parity.ResourceProfile{
            Class: "database",
            Database: &parity.DatabaseProfile{
                Engine:  engine,
                Version: version,
                Compute: compute,
                Storage: parity.StorageProfile{SizeGB: storageGB, Class: "ssd", Encrypted: true},
                HA:      multiAZ,
            },
            Features: map[string]bool{
                "pointInTimeRestore": backupRetention > 0,
                "multiAZ":            multiAZ,
            },
            SKU: instanceClass,
        }, nil
    case "aws_s3_bucket":
        return parity.ResourceProfile{
            Class:   "storage",
            Storage: &parity.StorageProfile{Class: "tiered", Encrypted: true},
            Features: map[string]bool{
                "versioning":   true,  // Phase 3 default
                "publicAccess": false, // Phase 3 default
            },
            SKU: "aws_s3_standard",
        }, nil
    default:
        return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
    }
}

// lookupComputeProfile turns "t3.medium" / "m6i.large" into ComputeProfile.
// Phase-3 hardcodes the EC2 instance types this adapter actually emits.
func lookupComputeProfile(instanceType string) parity.ComputeProfile {
    knownTypes := map[string]parity.ComputeProfile{
        "t3.small":   {VCPU: 2, MemoryGB: 2, Architecture: "x86_64", NetworkGbps: 5},
        "t3.medium":  {VCPU: 2, MemoryGB: 4, Architecture: "x86_64", NetworkGbps: 5},
        "t3.large":   {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 5},
        "t3.xlarge":  {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 5},
        "m6i.large":  {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 12.5},
        "m6i.xlarge": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
    }
    if p, ok := knownTypes[instanceType]; ok {
        return p
    }
    return parity.ComputeProfile{Architecture: "x86_64"}
}

func stripDBPrefix(instanceClass string) string {
    if len(instanceClass) > 3 && instanceClass[:3] == "db." {
        return instanceClass[3:]
    }
    return instanceClass
}

func storageProfileFromRootBlock(v any) parity.StorageProfile {
    blocks, _ := v.([]any)
    if len(blocks) == 0 {
        return parity.StorageProfile{Class: "ssd", Encrypted: true}
    }
    b, _ := blocks[0].(map[string]any)
    size := 0
    switch t := b["volume_size"].(type) {
    case int:
        size = t
    case float64:
        size = int(t)
    }
    encrypted, _ := b["encrypted"].(bool)
    return parity.StorageProfile{SizeGB: size, Class: "ssd", Encrypted: encrypted}
}
```

- [ ] **Step 4: Write tests**

Create `internal/cloud/aws/pricing_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPricingKey_EC2Instance(t *testing.T) {
    a := aws.New()
    target := ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "compute", "__component": "web", "size": "small"},
    }
    prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{"subnetId": "s-1"})
    for _, p := range prims {
        if p.TofuType == "aws_instance" {
            key, err := a.PricingKey(context.Background(), p)
            if err != nil {
                t.Fatalf("PricingKey: %v", err)
            }
            if key["service"] != "AmazonEC2" {
                t.Errorf("service = %v", key["service"])
            }
            if key["instanceType"] != "t3.small" {
                t.Errorf("instanceType = %v", key["instanceType"])
            }
            if key["region"] != "us-east-1" {
                t.Errorf("region = %v", key["region"])
            }
        }
    }
}

func TestPricingKey_FreePrimitive(t *testing.T) {
    a := aws.New()
    key, err := a.PricingKey(context.Background(), ir.ResourcePrimitive{
        TofuType: "aws_vpc", ID: "x.aws-us-east-1.vpc",
    })
    if err != nil {
        t.Fatalf("PricingKey: %v", err)
    }
    if key != nil {
        t.Errorf("VPC should return nil pricing key, got %v", key)
    }
}
```

Create `internal/cloud/aws/profile_test.go`:

```go
package aws_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestProfile_VPC(t *testing.T) {
    a := aws.New()
    prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
    }, cloud.ResolvedRefs{})
    for _, p := range prims {
        if p.TofuType == "aws_vpc" {
            prof, err := a.Profile(context.Background(), p)
            if err != nil {
                t.Fatalf("Profile: %v", err)
            }
            if prof.Class != "network" {
                t.Errorf("Class = %v", prof.Class)
            }
            if prof.Network == nil || prof.Network.CIDR != "10.0.0.0/16" {
                t.Errorf("Network: %+v", prof.Network)
            }
        }
    }
}

func TestProfile_DBInstance(t *testing.T) {
    a := aws.New()
    prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
        Cloud: "aws", Region: "us-east-1",
        Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": "medium"},
    }, cloud.ResolvedRefs{"subnetIds": []string{"s-1"}})
    for _, p := range prims {
        if p.TofuType == "aws_db_instance" {
            prof, err := a.Profile(context.Background(), p)
            if err != nil {
                t.Fatalf("Profile: %v", err)
            }
            if prof.Class != "database" {
                t.Errorf("Class = %v", prof.Class)
            }
            if prof.Database == nil || prof.Database.Engine != "postgres" {
                t.Errorf("Database: %+v", prof.Database)
            }
            if prof.Database.Compute.VCPU != 2 || prof.Database.Compute.MemoryGB != 4 {
                t.Errorf("Compute: %+v", prof.Database.Compute)
            }
        }
    }
}

func TestProfile_FreePrimitives(t *testing.T) {
    a := aws.New()
    _, err := a.Profile(context.Background(), ir.ResourcePrimitive{TofuType: "aws_subnet"})
    if err == nil {
        t.Error("expected ErrProfileUnavailable for subnet")
    }
}
```

- [ ] **Step 5: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/aws/ -v
git add internal/cloud/aws/
git commit -m "aws: PricingKey + Profile real implementations for the four v1 component types"
```

---

## Task 10: End-to-end fixture + final verification

**Files:**
- Create: `cmd/cli/testdata/full-stack-project/project.yaml`
- Create: `cmd/cli/testdata/full-stack-project/components/web-network.yaml`
- Create: `cmd/cli/testdata/full-stack-project/components/orders-db.yaml`
- Create: `cmd/cli/testdata/full-stack-project/components/web-app.yaml`
- Create: `cmd/cli/testdata/full-stack-project/components/uploads.yaml`
- Create: `cmd/cli/testdata/full-stack-project/stacks/dev/values.yaml`
- Create: `cmd/cli/full_stack_test.go`

- [ ] **Step 1: Write fixture**

```yaml
# project.yaml
apiVersion: infra.dev/v1alpha1
name: full-stack-demo
stacks:
  dev:
    stateBackend: { kind: local }
```

```yaml
# components/web-network.yaml
apiVersion: infra.dev/v1alpha1
name: web-network
type: network
spec:
  cidr: 10.0.0.0/16
  subnetCount: 2
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
```

```yaml
# components/orders-db.yaml
apiVersion: infra.dev/v1alpha1
name: orders-db
type: database
spec:
  engine: postgres
  version: "16"
  size: small
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
refs:
  - component: web-network
    output: subnet_ids
    as: subnetIds
  - component: web-network
    output: vpc_id
    as: vpcId
```

```yaml
# components/web-app.yaml
apiVersion: infra.dev/v1alpha1
name: web-app
type: compute
spec:
  size: small
  replicas: 1
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
refs:
  - component: web-network
    output: subnet_ids
    as: subnetId
  - component: web-network
    output: vpc_id
    as: vpcId
```

```yaml
# components/uploads.yaml
apiVersion: infra.dev/v1alpha1
name: uploads
type: storage
spec:
  versioning: true
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
```

```yaml
# stacks/dev/values.yaml
apiVersion: infra.dev/v1alpha1
vars: {}
```

- [ ] **Step 2: Write CLI smoke test**

Create `cmd/cli/full_stack_test.go`:

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
    "github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestPlanCommand_FullStackFixture(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    var stdout, stderr bytes.Buffer
    code := runPlan(context.Background(), planArgs{
        ProjectPath: "testdata/full-stack-project",
        Stack:       "dev",
        Adapters:    reg,
        Runner:      tofu.NewFakeRunner(),
        Inventory:   inventory.NewNullRepo(),
        WorkRoot:    t.TempDir(),
        Stdout:      &stdout,
        Stderr:      &stderr,
    })
    if code != 0 {
        t.Fatalf("exit %d stderr=%s", code, stderr.String())
    }
    out := stdout.String()
    // The fixture has 4 components × 1 target = 4 target plans.
    for _, name := range []string{"web-network", "orders-db", "web-app", "uploads"} {
        if !strings.Contains(out, name) {
            t.Errorf("plan output missing component %q:\n%s", name, out)
        }
    }
}
```

- [ ] **Step 3: Final verification**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH go vet ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l .
PATH=$HOME/.local/go/bin:$PATH go build -o bin/nimbusfab ./cmd/cli
```

All tests green; vet clean; no formatting drift; binary builds.

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/testdata/full-stack-project/ cmd/cli/full_stack_test.go
git commit -m "cli: full-stack fixture (network + database + compute + storage) end-to-end smoke"
```

---

## Task 11: README + CHANGELOG

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: README updates**

Update Status line + add component-types section:

```markdown
**Status:** pre-alpha. ... AWS Phase 3 adds the four v1 component types
(network, compute, database, storage) with real AWS primitives per type.

## Component types

Phase 3 ships four v1 types, registered automatically via
`components.DefaultRegistry()`:

| Type | AWS primitives | Outputs |
|---|---|---|
| `network` | VPC + IGW + RT + N subnets + RT associations | `vpc_id`, `subnet_ids`, `route_table_ids` |
| `compute` | Security group + N EC2 instances (T-shirt sized) | `instance_ids`, `private_ips`, `security_group_id` |
| `database` | DB subnet group + RDS instance (T-shirt sized; postgres/mysql/mariadb) | `endpoint`, `port`, `db_name` |
| `storage` | S3 bucket + versioning + public-access-block + SSE | `bucket_name`, `bucket_arn`, `bucket_url` |

See `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` for
per-type spec schemas, T-shirt size resolution, and the PricingKey /
Profile shapes the cost estimator + parity engine will consume.
```

- [ ] **Step 2: CHANGELOG entry**

```markdown
## Unreleased — AWS Adapter Expansion Phase 3

### Added

- Four concrete `components.Type` implementations: `network`, `compute`,
  `database`, `storage`. Each ships an embedded JSON Schema for its
  `spec` field and declares its output names + types.
- `components.DefaultRegistry()` returns all four registered;
  `engine.New` defaults `Config.ComponentTypes` to it.
- `Type.Outputs()` added to the `components.Type` interface.
- AWS adapter dispatches `Emit()` on `target.Spec["__type"]` (a new
  field the provisioner stuffs alongside `__component`).
- `internal/cloud/aws/network.go` — VPC + IGW + RT + N subnets + RT
  associations with deterministic /24 slicing and per-region AZ trios.
- `internal/cloud/aws/compute.go` — security group + N EC2 instances
  with T-shirt size → instance type resolution and per-region default AMIs.
- `internal/cloud/aws/database.go` — DB subnet group + RDS instance
  with T-shirt size → instance class + storage resolution for postgres
  / mysql / mariadb.
- `internal/cloud/aws/storage.go` — S3 bucket + versioning + public-
  access-block + server-side encryption with secure defaults and
  deterministic bucket-name derivation.
- `internal/cloud/aws/pricing.go` — `PricingKey()` real implementation
  with structured maps for AmazonEC2, AmazonRDS, AmazonS3 (free
  primitives return `nil, nil`).
- `internal/cloud/aws/profile.go` — `Profile()` real implementation
  populating `parity.ResourceProfile` per resource class (Compute,
  Storage, Database, Network).
- AWS adapter `SupportedComponentTypes()` now returns all four type
  names.
- Full-stack CLI fixture under `cmd/cli/testdata/full-stack-project/`
  exercising all four types in one project.

### Out of scope (deferred)

- Validator Phase 4 (per-type `SpecSchema` validation in the validator
  pipeline). Type schemas ship; wiring is its own phase.
- Cost estimator / parity engine consumption of the new data.
- Tier-1 `<cloud>:` escape-hatch schemas for AWS per-type fields.
- Tier-2 `raw:` block merging.
- Azure / GCP adapters.
- LocalStack integration tests.
- Auto-scaling groups, NAT gateways, RDS read replicas, S3 lifecycle.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: README + CHANGELOG for AWS Adapter Phase 3 expansion"
```

---

## Final state

End-state: a user with `tofu` installed + AWS creds can write a YAML
project with `network` + `compute` + `database` + `storage` components,
`nimbusfab plan --stack dev` produces a real Tofu workspace for each
target (each emitting multiple primitives), `nimbusfab apply` deploys
all of them respecting cross-component refs via
`data.terraform_remote_state`. PricingKey + Profile data populates per
primitive so cost estimator + parity engine can wire in cleanly when
those specs land.
