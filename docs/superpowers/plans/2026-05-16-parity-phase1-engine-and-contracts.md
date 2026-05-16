# Parity Engine Phase 1 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Land a working `pkg/parity` engine: contract-floor catalog for the four v1 component types (database / compute / storage / network), `Compare()` function that takes per-target `ResourceProfile`s and produces a `ParityReport` with weighted score, `EvaluateRules()` function that applies `parity.yaml` policies, parity report rendering, and integration into `engine.Plan` so every plan automatically computes parity. New `nimbusfab parity <deployment-id>` CLI command surfaces stored / fresh reports. Single-cloud (AWS-only today) reports score=1.0 trivially; multi-cloud activates when Azure / GCP land.

**Architecture:** `pkg/parity` already has the type contract (`ResourceProfile` and sub-profiles) shipped by Provisioner Phase 1. Phase 1 adds the engine, score function, rule evaluator, reporter, contracts catalog, and the `parity.yaml` loader. The engine is consumed by `pkg/provisioner.Plan()` (which already produces `TargetPlan.PrimitiveCount` and has `Profile` field reserved) and surfaced through `engine.Plan` results. The CLI's `plan` and `apply` commands print a parity summary; the new `parity` command produces detailed reports.

**Tech Stack:** No new dependencies. Pure Go + tests + embedded YAML.

**Conventions:**
- All paths relative to repo root `/home/kurt/git/nimbusfab-parity-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for all go commands.
- One commit per task.

**Out of scope (deferred):**
- REST API endpoints (`/api/v1/.../parity`) — web app phase.
- Inventory persistence of parity reports (`runs.parity_report_json`) — inventory Phase 2 or web app phase.
- Auto-balancing (adapter actively upgrading SKUs to maximize parity) — v2.
- Per-attribute weight tuning by users — v2.
- Cross-region equivalence mapping — v2.
- Historical parity tracking — v2.

---

## Task 1: Embed contract floors for the four v1 types

**Files:**
- Create: `pkg/parity/contracts/database.yaml`
- Create: `pkg/parity/contracts/compute.yaml`
- Create: `pkg/parity/contracts/storage.yaml`
- Create: `pkg/parity/contracts/network.yaml`
- Create: `pkg/parity/contracts.go`
- Create: `pkg/parity/contracts_test.go`

- [ ] **Step 1: Write the four contract YAML files**

Use the spec's database contract (`docs/superpowers/specs/2026-05-15-parity-design.md`) for shape. Each maps T-shirt sizes to minimum guarantees per cloud.

`pkg/parity/contracts/database.yaml`:

```yaml
apiVersion: parity.dev/v1alpha1
kind: ComponentContract
type: database
description: |
  Minimum guarantees for the `database` component type across all supported
  clouds. Adapters select the cheapest cloud SKU that satisfies these minimums.
sizes:
  small:
    compute:  { minVCPU: 2,  minMemoryGB: 2 }
    storage:  { minSizeGB: 100, minIOPS: 1000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
  medium:
    compute:  { minVCPU: 2,  minMemoryGB: 4 }
    storage:  { minSizeGB: 250, minIOPS: 3000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
  large:
    compute:  { minVCPU: 2,  minMemoryGB: 8 }
    storage:  { minSizeGB: 500, minIOPS: 5000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
  xlarge:
    compute:  { minVCPU: 4,  minMemoryGB: 16 }
    storage:  { minSizeGB: 1000, minIOPS: 10000, classIn: [ssd] }
    features: { pointInTimeRestore: required }
```

`pkg/parity/contracts/compute.yaml`:

```yaml
apiVersion: parity.dev/v1alpha1
kind: ComponentContract
type: compute
sizes:
  small:  { compute: { minVCPU: 2, minMemoryGB: 2 },  storage: { minSizeGB: 20, classIn: [ssd] } }
  medium: { compute: { minVCPU: 2, minMemoryGB: 4 },  storage: { minSizeGB: 20, classIn: [ssd] } }
  large:  { compute: { minVCPU: 2, minMemoryGB: 8 },  storage: { minSizeGB: 30, classIn: [ssd] } }
  xlarge: { compute: { minVCPU: 4, minMemoryGB: 16 }, storage: { minSizeGB: 50, classIn: [ssd] } }
```

`pkg/parity/contracts/storage.yaml`:

```yaml
apiVersion: parity.dev/v1alpha1
kind: ComponentContract
type: storage
sizes:
  small:  { storage: { classIn: [ssd, tiered], encrypted: required } }
  medium: { storage: { classIn: [ssd, tiered], encrypted: required } }
  large:  { storage: { classIn: [ssd, tiered], encrypted: required } }
  xlarge: { storage: { classIn: [ssd, tiered], encrypted: required } }
```

`pkg/parity/contracts/network.yaml`:

```yaml
apiVersion: parity.dev/v1alpha1
kind: ComponentContract
type: network
sizes:
  small:  { network: { minSubnets: 2 } }
  medium: { network: { minSubnets: 3 } }
  large:  { network: { minSubnets: 3 } }
  xlarge: { network: { minSubnets: 3 } }
```

- [ ] **Step 2: Write contracts.go**

Create `pkg/parity/contracts.go`:

```go
package parity

import (
    "embed"
    "fmt"

    "gopkg.in/yaml.v3"
)

//go:embed contracts/*.yaml
var contractFS embed.FS

// ContractFloor is the resolved minimum-guarantees record for one
// (component type, T-shirt size) pair.
type ContractFloor struct {
    Type     string
    Size     string
    Compute  *ComputeFloor
    Storage  *StorageFloor
    Network  *NetworkFloor
    Features map[string]string // keyed feature -> "required"
}

// ComputeFloor expresses minimums per the spec.
type ComputeFloor struct {
    MinVCPU     int     `yaml:"minVCPU"`
    MinMemoryGB float64 `yaml:"minMemoryGB"`
}

// StorageFloor expresses storage minimums.
type StorageFloor struct {
    MinSizeGB int      `yaml:"minSizeGB"`
    MinIOPS   int      `yaml:"minIOPS"`
    ClassIn   []string `yaml:"classIn"`
    Encrypted string   `yaml:"encrypted"`
}

// NetworkFloor expresses network minimums.
type NetworkFloor struct {
    MinSubnets int `yaml:"minSubnets"`
}

// contractDoc mirrors the YAML shape.
type contractDoc struct {
    APIVersion  string                 `yaml:"apiVersion"`
    Kind        string                 `yaml:"kind"`
    Type        string                 `yaml:"type"`
    Description string                 `yaml:"description"`
    Sizes       map[string]sizeEntry   `yaml:"sizes"`
}

type sizeEntry struct {
    Compute  *ComputeFloor   `yaml:"compute,omitempty"`
    Storage  *StorageFloor   `yaml:"storage,omitempty"`
    Network  *NetworkFloor   `yaml:"network,omitempty"`
    Features map[string]string `yaml:"features,omitempty"`
}

// Contracts holds the parsed catalog keyed by (type, size).
type Contracts struct {
    floors map[string]map[string]ContractFloor
}

// LoadContracts parses the embedded YAML files. Panics on malformed YAML
// (build-time failure; users shouldn't encounter it).
func LoadContracts() (*Contracts, error) {
    out := &Contracts{floors: map[string]map[string]ContractFloor{}}
    entries, err := contractFS.ReadDir("contracts")
    if err != nil {
        return nil, fmt.Errorf("parity.LoadContracts: %w", err)
    }
    for _, e := range entries {
        body, err := contractFS.ReadFile("contracts/" + e.Name())
        if err != nil {
            return nil, fmt.Errorf("parity.LoadContracts: read %s: %w", e.Name(), err)
        }
        var doc contractDoc
        if err := yaml.Unmarshal(body, &doc); err != nil {
            return nil, fmt.Errorf("parity.LoadContracts: parse %s: %w", e.Name(), err)
        }
        if doc.Type == "" {
            return nil, fmt.Errorf("parity.LoadContracts: %s missing 'type'", e.Name())
        }
        sizes := map[string]ContractFloor{}
        for sizeName, entry := range doc.Sizes {
            sizes[sizeName] = ContractFloor{
                Type: doc.Type, Size: sizeName,
                Compute: entry.Compute, Storage: entry.Storage, Network: entry.Network,
                Features: entry.Features,
            }
        }
        out.floors[doc.Type] = sizes
    }
    return out, nil
}

// Lookup returns the floor for (type, size). Both must be non-empty.
// Returns ok=false if either is unknown.
func (c *Contracts) Lookup(componentType, size string) (ContractFloor, bool) {
    if c == nil {
        return ContractFloor{}, false
    }
    sizes, ok := c.floors[componentType]
    if !ok {
        return ContractFloor{}, false
    }
    f, ok := sizes[size]
    return f, ok
}

// SizesFor returns the list of sizes defined for a type.
func (c *Contracts) SizesFor(componentType string) []string {
    sizes := c.floors[componentType]
    out := make([]string, 0, len(sizes))
    for k := range sizes {
        out = append(out, k)
    }
    return out
}
```

- [ ] **Step 3: Test**

Create `pkg/parity/contracts_test.go`:

```go
package parity_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestLoadContracts_AllFourTypes(t *testing.T) {
    c, err := parity.LoadContracts()
    if err != nil {
        t.Fatalf("LoadContracts: %v", err)
    }
    for _, typeName := range []string{"database", "compute", "storage", "network"} {
        if len(c.SizesFor(typeName)) == 0 {
            t.Errorf("no sizes for type %q", typeName)
        }
    }
}

func TestLookup_AllFourSizes(t *testing.T) {
    c, _ := parity.LoadContracts()
    for _, size := range []string{"small", "medium", "large", "xlarge"} {
        if _, ok := c.Lookup("database", size); !ok {
            t.Errorf("database/%s missing", size)
        }
        if _, ok := c.Lookup("compute", size); !ok {
            t.Errorf("compute/%s missing", size)
        }
    }
}

func TestLookup_UnknownTypeOrSize(t *testing.T) {
    c, _ := parity.LoadContracts()
    if _, ok := c.Lookup("nonexistent", "small"); ok {
        t.Error("unknown type should return ok=false")
    }
    if _, ok := c.Lookup("database", "tiny"); ok {
        t.Error("unknown size should return ok=false")
    }
}

func TestDatabaseSmall_HasExpectedFloors(t *testing.T) {
    c, _ := parity.LoadContracts()
    f, _ := c.Lookup("database", "small")
    if f.Compute == nil || f.Compute.MinVCPU != 2 || f.Compute.MinMemoryGB != 2 {
        t.Errorf("small compute floor: %+v", f.Compute)
    }
    if f.Storage == nil || f.Storage.MinSizeGB != 100 {
        t.Errorf("small storage floor: %+v", f.Storage)
    }
    if f.Features["pointInTimeRestore"] != "required" {
        t.Errorf("PITR feature: %v", f.Features)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v -run "TestLoadContracts|TestLookup|TestDatabaseSmall"
git add pkg/parity/contracts.go pkg/parity/contracts.yaml pkg/parity/contracts_test.go pkg/parity/contracts/
git commit -m "parity: embed contract floors for the four v1 types + Contracts catalog loader"
```

---

## Task 2: Score function

**Files:**
- Create: `pkg/parity/score.go`
- Create: `pkg/parity/score_test.go`

The score function takes a `ParityReport` (built from `[]TargetProfile`) and returns a weighted score in [0, 1]. Algorithm per the parity spec § "Parity score function".

- [ ] **Step 1: Define report-extending types**

In `pkg/parity/types.go` (existing file), ADD types the engine needs:

```go
// TargetProfile is one cloud target's profile data plus identity.
type TargetProfile struct {
    DeploymentTargetID string
    Cloud              string
    Region             string
    Profile            ResourceProfile
}

// AttrComparison is one attribute's cross-target comparison.
type AttrComparison struct {
    Attribute string         // dotted: "compute.vCPU", "features.pointInTimeRestore"
    Kind      string         // "exact" | "numeric" | "boolean"
    Values    map[string]any // keyed by "<cloud>/<region>"
    AllMatch  bool
    MinValue  any
    MaxValue  any
    Score     float64
}

// ParityReport is the complete comparison output for one component.
type ParityReport struct {
    Component   string
    Type        string
    Size        string
    Contract    ContractFloor
    Targets     []TargetProfile
    Comparisons []AttrComparison
    Score       float64
    Warnings    []string
    GeneratedAt time.Time
}

// Violation is one rule-evaluator result.
type Violation struct {
    Component string
    Attribute string
    Policy    string // "exact" | "maxRatio" | "requireAll" | "minScore"
    Detail    string
    Action    string // "warn" | "block"
}
```

(Add `"time"` to imports if not already there.)

- [ ] **Step 2: Implement score.go**

Create `pkg/parity/score.go`:

```go
package parity

import (
    "fmt"
)

// defaultWeights are the per-attribute weights documented in the parity spec.
// NOT user-tunable in v1.
var defaultWeights = map[string]float64{
    "compute.vCPU":         0.20,
    "compute.memoryGB":     0.15,
    "compute.architecture": 0.05,
    "storage.sizeGB":       0.10,
    "storage.iops":         0.10,
    "storage.class":        0.05,
    "features":             0.20, // averaged over declared features
    "database.engine":      0.10,
    "database.version":     0.05,
}

// BuildComparisons takes per-target profiles and returns AttrComparisons
// covering every attribute relevant to the profile's class.
func BuildComparisons(targets []TargetProfile) []AttrComparison {
    if len(targets) == 0 {
        return nil
    }
    class := targets[0].Profile.Class
    var out []AttrComparison
    switch class {
    case "compute":
        out = append(out, computeComparisons(targets)...)
        out = append(out, storageComparisons(targets, false)...)
        out = append(out, featuresComparisons(targets)...)
    case "database":
        out = append(out, databaseComparisons(targets)...)
        out = append(out, featuresComparisons(targets)...)
    case "storage":
        out = append(out, storageComparisons(targets, true)...)
        out = append(out, featuresComparisons(targets)...)
    case "network":
        out = append(out, networkComparisons(targets)...)
    }
    return out
}

// Score computes the weighted mean over the AttrComparisons.
// Weights for inapplicable attributes are dropped; remaining weights renormalize.
func Score(comparisons []AttrComparison) float64 {
    if len(comparisons) == 0 {
        return 1.0
    }
    totalWeight := 0.0
    weightedSum := 0.0
    seenFeatures := false
    var featuresScoreSum float64
    var featuresCount int
    for _, c := range comparisons {
        if isFeatureAttr(c.Attribute) {
            featuresScoreSum += c.Score
            featuresCount++
            seenFeatures = true
            continue
        }
        w, ok := defaultWeights[c.Attribute]
        if !ok {
            continue
        }
        totalWeight += w
        weightedSum += w * c.Score
    }
    if seenFeatures && featuresCount > 0 {
        avgFeatures := featuresScoreSum / float64(featuresCount)
        w := defaultWeights["features"]
        totalWeight += w
        weightedSum += w * avgFeatures
    }
    if totalWeight == 0 {
        return 1.0
    }
    return weightedSum / totalWeight
}

func isFeatureAttr(attr string) bool {
    return len(attr) > len("features.") && attr[:len("features.")] == "features."
}

func computeComparisons(targets []TargetProfile) []AttrComparison {
    var out []AttrComparison
    out = append(out, numericCmp("compute.vCPU", targets, func(p ResourceProfile) (float64, bool) {
        if p.Compute == nil {
            return 0, false
        }
        return float64(p.Compute.VCPU), true
    }))
    out = append(out, numericCmp("compute.memoryGB", targets, func(p ResourceProfile) (float64, bool) {
        if p.Compute == nil {
            return 0, false
        }
        return p.Compute.MemoryGB, true
    }))
    out = append(out, exactCmp("compute.architecture", targets, func(p ResourceProfile) (any, bool) {
        if p.Compute == nil {
            return nil, false
        }
        return p.Compute.Architecture, true
    }))
    return out
}

func storageComparisons(targets []TargetProfile, asPrimary bool) []AttrComparison {
    var out []AttrComparison
    pick := func(p ResourceProfile) *StorageProfile {
        if asPrimary {
            return p.Storage
        }
        return p.Storage
    }
    out = append(out, numericCmp("storage.sizeGB", targets, func(p ResourceProfile) (float64, bool) {
        s := pick(p)
        if s == nil {
            return 0, false
        }
        return float64(s.SizeGB), true
    }))
    out = append(out, numericCmp("storage.iops", targets, func(p ResourceProfile) (float64, bool) {
        s := pick(p)
        if s == nil {
            return 0, false
        }
        return float64(s.IOPS), true
    }))
    out = append(out, exactCmp("storage.class", targets, func(p ResourceProfile) (any, bool) {
        s := pick(p)
        if s == nil {
            return nil, false
        }
        return s.Class, true
    }))
    return out
}

func databaseComparisons(targets []TargetProfile) []AttrComparison {
    var out []AttrComparison
    out = append(out, exactCmp("database.engine", targets, func(p ResourceProfile) (any, bool) {
        if p.Database == nil {
            return nil, false
        }
        return p.Database.Engine, true
    }))
    out = append(out, exactCmp("database.version", targets, func(p ResourceProfile) (any, bool) {
        if p.Database == nil {
            return nil, false
        }
        return p.Database.Version, true
    }))
    out = append(out, numericCmp("compute.vCPU", targets, func(p ResourceProfile) (float64, bool) {
        if p.Database == nil {
            return 0, false
        }
        return float64(p.Database.Compute.VCPU), true
    }))
    out = append(out, numericCmp("compute.memoryGB", targets, func(p ResourceProfile) (float64, bool) {
        if p.Database == nil {
            return 0, false
        }
        return p.Database.Compute.MemoryGB, true
    }))
    out = append(out, numericCmp("storage.sizeGB", targets, func(p ResourceProfile) (float64, bool) {
        if p.Database == nil {
            return 0, false
        }
        return float64(p.Database.Storage.SizeGB), true
    }))
    return out
}

func networkComparisons(targets []TargetProfile) []AttrComparison {
    // Network parity is mostly informational; one numeric (CIDR-derived).
    return nil
}

func featuresComparisons(targets []TargetProfile) []AttrComparison {
    // Union all feature keys across targets.
    keys := map[string]bool{}
    for _, t := range targets {
        for k := range t.Profile.Features {
            keys[k] = true
        }
    }
    var out []AttrComparison
    for key := range keys {
        c := AttrComparison{
            Attribute: "features." + key,
            Kind:      "boolean",
            Values:    map[string]any{},
        }
        first := true
        var firstVal bool
        allMatch := true
        for _, t := range targets {
            v := t.Profile.Features[key]
            c.Values[targetKey(t)] = v
            if first {
                firstVal = v
                first = false
            } else if v != firstVal {
                allMatch = false
            }
        }
        c.AllMatch = allMatch
        if allMatch {
            c.Score = 1.0
        }
        out = append(out, c)
    }
    return out
}

// numericCmp builds a numeric comparison: score = 1 - (max-min)/max, clamped [0,1].
func numericCmp(attr string, targets []TargetProfile, get func(ResourceProfile) (float64, bool)) AttrComparison {
    c := AttrComparison{Attribute: attr, Kind: "numeric", Values: map[string]any{}}
    var min, max float64
    haveAny := false
    for _, t := range targets {
        v, ok := get(t.Profile)
        if !ok {
            continue
        }
        c.Values[targetKey(t)] = v
        if !haveAny {
            min, max = v, v
            haveAny = true
        }
        if v < min {
            min = v
        }
        if v > max {
            max = v
        }
    }
    if !haveAny {
        c.Score = 1.0
        c.AllMatch = true
        return c
    }
    c.MinValue, c.MaxValue = min, max
    c.AllMatch = min == max
    if max == 0 {
        c.Score = 1.0
    } else {
        c.Score = 1.0 - (max-min)/max
        if c.Score < 0 {
            c.Score = 0
        }
    }
    return c
}

func exactCmp(attr string, targets []TargetProfile, get func(ResourceProfile) (any, bool)) AttrComparison {
    c := AttrComparison{Attribute: attr, Kind: "exact", Values: map[string]any{}}
    var first any
    haveFirst := false
    allMatch := true
    for _, t := range targets {
        v, ok := get(t.Profile)
        if !ok {
            continue
        }
        c.Values[targetKey(t)] = v
        if !haveFirst {
            first = v
            haveFirst = true
        } else if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", first) {
            allMatch = false
        }
    }
    c.AllMatch = allMatch
    if allMatch {
        c.Score = 1.0
    }
    return c
}

func targetKey(t TargetProfile) string {
    return t.Cloud + "/" + t.Region
}
```

- [ ] **Step 3: Test**

Create `pkg/parity/score_test.go`:

```go
package parity_test

import (
    "math"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestScore_AllIdenticalTargets(t *testing.T) {
    targets := []parity.TargetProfile{
        {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
            Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
        }},
        {Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
            Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
        }},
    }
    cmps := parity.BuildComparisons(targets)
    score := parity.Score(cmps)
    if math.Abs(score-1.0) > 0.001 {
        t.Errorf("identical targets: score = %f, want 1.0", score)
    }
}

func TestScore_DivergentMemory(t *testing.T) {
    // AWS picks 4 GB, GCP picks 8 GB for the same size.
    targets := []parity.TargetProfile{
        {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
            Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
        }},
        {Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 8, Architecture: "x86_64"},
            Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
        }},
    }
    cmps := parity.BuildComparisons(targets)
    score := parity.Score(cmps)
    if score >= 1.0 || score <= 0.5 {
        t.Errorf("score for diverging memory = %f; want in (0.5, 1.0)", score)
    }
}

func TestScore_DifferentArchitecture_ZerosOutThatGroup(t *testing.T) {
    targets := []parity.TargetProfile{
        {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
        }},
        {Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "arm64"},
        }},
    }
    cmps := parity.BuildComparisons(targets)
    for _, c := range cmps {
        if c.Attribute == "compute.architecture" {
            if c.Score != 0 {
                t.Errorf("arch mismatch: score = %f, want 0", c.Score)
            }
            return
        }
    }
    t.Error("no architecture comparison in output")
}

func TestScore_SingleTargetIsTrivially1(t *testing.T) {
    targets := []parity.TargetProfile{
        {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
            Class:   "compute",
            Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
        }},
    }
    cmps := parity.BuildComparisons(targets)
    score := parity.Score(cmps)
    if score < 0.99 {
        t.Errorf("single target should be ~1.0, got %f", score)
    }
}

func TestScore_FeaturesGroup_AllMatchVsMismatch(t *testing.T) {
    matching := []parity.TargetProfile{
        {Cloud: "aws", Profile: parity.ResourceProfile{Class: "database",
            Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
            Features: map[string]bool{"multiAZ": true, "pointInTimeRestore": true}}},
        {Cloud: "gcp", Profile: parity.ResourceProfile{Class: "database",
            Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
            Features: map[string]bool{"multiAZ": true, "pointInTimeRestore": true}}},
    }
    mismatching := []parity.TargetProfile{
        matching[0],
        {Cloud: "gcp", Profile: parity.ResourceProfile{Class: "database",
            Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
            Features: map[string]bool{"multiAZ": false, "pointInTimeRestore": true}}},
    }
    matchScore := parity.Score(parity.BuildComparisons(matching))
    mismatchScore := parity.Score(parity.BuildComparisons(mismatching))
    if mismatchScore >= matchScore {
        t.Errorf("mismatch should score lower: match=%f mismatch=%f", matchScore, mismatchScore)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v
git add pkg/parity/score.go pkg/parity/score_test.go pkg/parity/types.go
git commit -m "parity: score function with weighted means + AttrComparison builders"
```

---

## Task 3: Engine + Compare

**Files:**
- Create: `pkg/parity/engine.go`
- Create: `pkg/parity/engine_test.go`

The Engine is the public surface — `Compare` builds a `ParityReport` from a `CompareInput`, `EvaluateRules` (Task 4) applies a `ProjectRules`.

- [ ] **Step 1: Implement engine.go**

Create `pkg/parity/engine.go`:

```go
package parity

import (
    "context"
    "fmt"
    "time"
)

// Engine is the public parity API. Implementations are stateless except for
// the contracts catalog.
type Engine interface {
    Compare(ctx context.Context, in CompareInput) (*ParityReport, error)
    EvaluateRules(ctx context.Context, report *ParityReport, rules ProjectRules) ([]Violation, error)
}

// CompareInput is what Compare takes.
type CompareInput struct {
    Component string
    Type      string         // "network" | "compute" | "database" | "storage"
    Size      string         // "small" | ... | "" if explicit dimensions
    Targets   []TargetProfile
}

// NewEngine returns a parity Engine seeded with the embedded contract catalog.
func NewEngine() (Engine, error) {
    contracts, err := LoadContracts()
    if err != nil {
        return nil, fmt.Errorf("parity.NewEngine: %w", err)
    }
    return &engine{contracts: contracts}, nil
}

type engine struct {
    contracts *Contracts
}

// Compare builds a ParityReport for the input.
func (e *engine) Compare(ctx context.Context, in CompareInput) (*ParityReport, error) {
    report := &ParityReport{
        Component:   in.Component,
        Type:        in.Type,
        Size:        in.Size,
        Targets:     in.Targets,
        GeneratedAt: time.Now().UTC(),
    }
    if in.Size != "" {
        floor, ok := e.contracts.Lookup(in.Type, in.Size)
        if ok {
            report.Contract = floor
        }
    }
    report.Comparisons = BuildComparisons(in.Targets)
    report.Score = Score(report.Comparisons)
    // Floor-satisfaction warnings: when a target falls below the contract floor,
    // we record but don't block.
    if in.Size != "" && report.Contract.Type != "" {
        for _, t := range in.Targets {
            if w := floorWarnings(report.Contract, t); len(w) > 0 {
                report.Warnings = append(report.Warnings, w...)
            }
        }
    }
    return report, nil
}

func floorWarnings(floor ContractFloor, t TargetProfile) []string {
    var out []string
    label := t.Cloud + "/" + t.Region
    if floor.Compute != nil && t.Profile.Compute != nil {
        if t.Profile.Compute.VCPU < floor.Compute.MinVCPU {
            out = append(out, fmt.Sprintf("%s: compute.vCPU=%d below floor %d", label, t.Profile.Compute.VCPU, floor.Compute.MinVCPU))
        }
        if t.Profile.Compute.MemoryGB < floor.Compute.MinMemoryGB {
            out = append(out, fmt.Sprintf("%s: compute.memoryGB=%v below floor %v", label, t.Profile.Compute.MemoryGB, floor.Compute.MinMemoryGB))
        }
    }
    if floor.Compute != nil && t.Profile.Database != nil {
        if t.Profile.Database.Compute.VCPU < floor.Compute.MinVCPU {
            out = append(out, fmt.Sprintf("%s: database.compute.vCPU=%d below floor %d", label, t.Profile.Database.Compute.VCPU, floor.Compute.MinVCPU))
        }
        if t.Profile.Database.Compute.MemoryGB < floor.Compute.MinMemoryGB {
            out = append(out, fmt.Sprintf("%s: database.compute.memoryGB=%v below floor %v", label, t.Profile.Database.Compute.MemoryGB, floor.Compute.MinMemoryGB))
        }
    }
    return out
}
```

- [ ] **Step 2: Test**

Create `pkg/parity/engine_test.go`:

```go
package parity_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestEngine_Compare_BuildsReport(t *testing.T) {
    e, err := parity.NewEngine()
    if err != nil {
        t.Fatalf("NewEngine: %v", err)
    }
    rep, err := e.Compare(context.Background(), parity.CompareInput{
        Component: "orders-db", Type: "database", Size: "small",
        Targets: []parity.TargetProfile{
            {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
                Class: "database",
                Database: &parity.DatabaseProfile{
                    Engine: "postgres", Version: "16",
                    Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 2},
                    Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"},
                },
                Features: map[string]bool{"pointInTimeRestore": true},
            }},
        },
    })
    if err != nil {
        t.Fatalf("Compare: %v", err)
    }
    if rep.Component != "orders-db" || rep.Type != "database" || rep.Size != "small" {
        t.Errorf("report identity: %+v", rep)
    }
    if rep.Contract.Type != "database" {
        t.Errorf("contract not populated: %+v", rep.Contract)
    }
    if rep.Score < 0.99 {
        t.Errorf("single-target score = %f, want ~1.0", rep.Score)
    }
}

func TestEngine_Compare_RecordsFloorWarning(t *testing.T) {
    e, _ := parity.NewEngine()
    rep, _ := e.Compare(context.Background(), parity.CompareInput{
        Component: "tiny-db", Type: "database", Size: "small",
        Targets: []parity.TargetProfile{
            {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
                Class: "database",
                Database: &parity.DatabaseProfile{
                    Engine: "postgres",
                    Compute: parity.ComputeProfile{VCPU: 1, MemoryGB: 1},  // Below small floor
                    Storage: parity.StorageProfile{SizeGB: 50, Class: "ssd"},
                },
            }},
        },
    })
    if len(rep.Warnings) == 0 {
        t.Error("expected floor warning for below-spec database")
    }
}

func TestEngine_Compare_ExplicitSize_NoContract(t *testing.T) {
    e, _ := parity.NewEngine()
    rep, _ := e.Compare(context.Background(), parity.CompareInput{
        Component: "custom", Type: "compute", Size: "", // explicit dims; no T-shirt
        Targets: []parity.TargetProfile{
            {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
                Class:   "compute",
                Compute: &parity.ComputeProfile{VCPU: 8, MemoryGB: 32},
            }},
        },
    })
    if rep.Contract.Type != "" {
        t.Errorf("expected empty contract for size=\"\", got %+v", rep.Contract)
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v -run "TestEngine_"
git add pkg/parity/engine.go pkg/parity/engine_test.go
git commit -m "parity: Engine with Compare; resolves contract floor; emits floor-warning notes"
```

---

## Task 4: Rule evaluator

**Files:**
- Create: `pkg/parity/rules.go`
- Create: `pkg/parity/rules_test.go`

Per the parity spec § "User parity rules", policies are `exact`, `maxRatio`, `requireAll`, `minScore`. Modes are `warn`, `block`, `off`.

- [ ] **Step 1: Define rule types in types.go**

In `pkg/parity/types.go` ADD:

```go
// ProjectRules is the parsed parity.yaml.
type ProjectRules struct {
    Default    ModeRules
    Components map[string]ComponentRules
}

// ModeRules applies when no per-component rule exists.
type ModeRules struct {
    Mode     string
    MinScore float64
}

// ComponentRules is one component's parity ruleset.
type ComponentRules struct {
    Mode       string
    MinScore   float64
    Attributes map[string]AttributePolicy
}

// AttributePolicy is one per-attribute rule.
type AttributePolicy struct {
    Policy   string  // "exact" | "maxRatio" | "requireAll"
    MaxRatio float64
}
```

- [ ] **Step 2: Implement rules.go**

Create `pkg/parity/rules.go`:

```go
package parity

import (
    "context"
    "fmt"
)

// EvaluateRules applies the project's parity rules to a report and returns
// any violations. Returns empty slice (no violations) when no rule fires.
func (e *engine) EvaluateRules(ctx context.Context, report *ParityReport, rules ProjectRules) ([]Violation, error) {
    if rules.Default.Mode == "" && len(rules.Components) == 0 {
        return nil, nil
    }
    compRule, hasCompRule := rules.Components[report.Component]
    mode := rules.Default.Mode
    minScore := rules.Default.MinScore
    if hasCompRule {
        if compRule.Mode != "" {
            mode = compRule.Mode
        }
        if compRule.MinScore > 0 {
            minScore = compRule.MinScore
        }
    }
    if mode == "off" {
        return nil, nil
    }
    var out []Violation
    // Component-level minScore.
    if minScore > 0 && report.Score < minScore {
        out = append(out, Violation{
            Component: report.Component, Policy: "minScore",
            Detail: fmt.Sprintf("score %.2f below minScore %.2f", report.Score, minScore),
            Action: actionFromMode(mode),
        })
    }
    // Per-attribute policies.
    if hasCompRule {
        for attrName, policy := range compRule.Attributes {
            v := violationForAttr(report, attrName, policy)
            if v != nil {
                v.Component = report.Component
                v.Action = actionFromMode(mode)
                out = append(out, *v)
            }
        }
    }
    return out, nil
}

func actionFromMode(mode string) string {
    if mode == "block" {
        return "block"
    }
    return "warn"
}

func violationForAttr(report *ParityReport, attrName string, policy AttributePolicy) *Violation {
    var cmp *AttrComparison
    for i := range report.Comparisons {
        if report.Comparisons[i].Attribute == attrName {
            cmp = &report.Comparisons[i]
            break
        }
    }
    if cmp == nil {
        return nil
    }
    switch policy.Policy {
    case "exact":
        if !cmp.AllMatch {
            return &Violation{Attribute: attrName, Policy: "exact",
                Detail: fmt.Sprintf("values differ: %v", cmp.Values)}
        }
    case "maxRatio":
        if cmp.Kind != "numeric" {
            return &Violation{Attribute: attrName, Policy: "maxRatio",
                Detail: "maxRatio only applies to numeric attributes"}
        }
        max, okMax := cmp.MaxValue.(float64)
        min, okMin := cmp.MinValue.(float64)
        if okMax && okMin && min > 0 {
            ratio := max / min
            if ratio > policy.MaxRatio {
                return &Violation{Attribute: attrName, Policy: "maxRatio",
                    Detail: fmt.Sprintf("ratio %.2f exceeds maxRatio %.2f", ratio, policy.MaxRatio)}
            }
        }
    case "requireAll":
        // For boolean features: every target must have value true.
        for cloud, v := range cmp.Values {
            if b, ok := v.(bool); !ok || !b {
                return &Violation{Attribute: attrName, Policy: "requireAll",
                    Detail: fmt.Sprintf("%s: %v (requireAll wants true everywhere)", cloud, v)}
            }
        }
    }
    return nil
}
```

- [ ] **Step 3: Test**

Create `pkg/parity/rules_test.go`:

```go
package parity_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func sampleReport() *parity.ParityReport {
    return &parity.ParityReport{
        Component: "db", Type: "database", Size: "small",
        Score: 0.6,
        Comparisons: []parity.AttrComparison{
            {Attribute: "compute.memoryGB", Kind: "numeric", MinValue: 4.0, MaxValue: 8.0, Score: 0.5},
            {Attribute: "compute.vCPU", Kind: "numeric", MinValue: 2.0, MaxValue: 2.0, AllMatch: true, Score: 1.0},
            {Attribute: "features.multiAZ", Kind: "boolean",
                Values: map[string]any{"aws/us-east-1": true, "gcp/us-central1": false}, AllMatch: false, Score: 0},
        },
    }
}

func TestRules_NoRules_NoViolations(t *testing.T) {
    e, _ := parity.NewEngine()
    v, err := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{})
    if err != nil {
        t.Fatalf("EvaluateRules: %v", err)
    }
    if len(v) != 0 {
        t.Errorf("no rules should yield no violations, got %d", len(v))
    }
}

func TestRules_DefaultMinScore_BelowThreshold(t *testing.T) {
    e, _ := parity.NewEngine()
    v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
        Default: parity.ModeRules{Mode: "warn", MinScore: 0.8},
    })
    if len(v) != 1 || v[0].Policy != "minScore" {
        t.Errorf("expected one minScore violation, got %+v", v)
    }
    if v[0].Action != "warn" {
        t.Errorf("action = %q", v[0].Action)
    }
}

func TestRules_PerComponent_ExactPolicy(t *testing.T) {
    e, _ := parity.NewEngine()
    v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
        Components: map[string]parity.ComponentRules{
            "db": {Mode: "block", Attributes: map[string]parity.AttributePolicy{
                "compute.memoryGB": {Policy: "exact"},
            }},
        },
    })
    if len(v) != 1 || v[0].Policy != "exact" || v[0].Action != "block" {
        t.Errorf("expected one block exact violation, got %+v", v)
    }
}

func TestRules_PerComponent_MaxRatio(t *testing.T) {
    e, _ := parity.NewEngine()
    v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
        Components: map[string]parity.ComponentRules{
            "db": {Mode: "warn", Attributes: map[string]parity.AttributePolicy{
                "compute.memoryGB": {Policy: "maxRatio", MaxRatio: 1.5},
            }},
        },
    })
    // memoryGB max=8, min=4, ratio=2.0; exceeds 1.5.
    if len(v) != 1 || v[0].Policy != "maxRatio" {
        t.Errorf("expected one maxRatio violation, got %+v", v)
    }
}

func TestRules_PerComponent_RequireAll(t *testing.T) {
    e, _ := parity.NewEngine()
    v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
        Components: map[string]parity.ComponentRules{
            "db": {Mode: "block", Attributes: map[string]parity.AttributePolicy{
                "features.multiAZ": {Policy: "requireAll"},
            }},
        },
    })
    if len(v) != 1 || v[0].Policy != "requireAll" {
        t.Errorf("expected one requireAll violation, got %+v", v)
    }
}

func TestRules_OffMode_NoViolations(t *testing.T) {
    e, _ := parity.NewEngine()
    v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
        Default: parity.ModeRules{Mode: "block", MinScore: 0.9},
        Components: map[string]parity.ComponentRules{
            "db": {Mode: "off"},
        },
    })
    if len(v) != 0 {
        t.Errorf("off mode should suppress: %+v", v)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v -run TestRules_
git add pkg/parity/rules.go pkg/parity/rules_test.go pkg/parity/types.go
git commit -m "parity: rule evaluator (exact/maxRatio/requireAll/minScore policies × warn/block/off modes)"
```

---

## Task 5: parity.yaml loader

**Files:**
- Create: `pkg/parity/rules_loader.go`
- Create: `pkg/parity/rules_loader_test.go`

- [ ] **Step 1: Implement loader**

Create `pkg/parity/rules_loader.go`:

```go
package parity

import (
    "fmt"
    "os"

    "gopkg.in/yaml.v3"
)

// ParityFileDoc mirrors the parity.yaml top-level shape.
type ParityFileDoc struct {
    APIVersion string         `yaml:"apiVersion"`
    Kind       string         `yaml:"kind"`
    Parity     ParityRulesDoc `yaml:"parity"`
}

// ParityRulesDoc mirrors the `parity:` block.
type ParityRulesDoc struct {
    Default    ModeRulesDoc                  `yaml:"default,omitempty"`
    Components map[string]ComponentRulesDoc  `yaml:"components,omitempty"`
}

// ModeRulesDoc is the yaml shape for default rules.
type ModeRulesDoc struct {
    Mode     string  `yaml:"mode"`
    MinScore float64 `yaml:"minScore"`
}

// ComponentRulesDoc is the yaml shape for per-component rules.
type ComponentRulesDoc struct {
    Mode       string                          `yaml:"mode,omitempty"`
    MinScore   float64                         `yaml:"minScore,omitempty"`
    Attributes map[string]AttributePolicyDoc   `yaml:"attributes,omitempty"`
}

// AttributePolicyDoc is the yaml shape for an attribute policy.
type AttributePolicyDoc struct {
    Policy   string  `yaml:"policy"`
    MaxRatio float64 `yaml:"maxRatio,omitempty"`
}

// LoadRulesFromFile parses parity.yaml from disk. Returns empty ProjectRules
// when the file is missing; that is the parity-default "informative-only" mode.
func LoadRulesFromFile(path string) (ProjectRules, error) {
    body, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return ProjectRules{}, nil
        }
        return ProjectRules{}, fmt.Errorf("parity.LoadRulesFromFile: %w", err)
    }
    return LoadRules(body)
}

// LoadRules parses parity.yaml from bytes.
func LoadRules(body []byte) (ProjectRules, error) {
    var doc ParityFileDoc
    if err := yaml.Unmarshal(body, &doc); err != nil {
        return ProjectRules{}, fmt.Errorf("parity.LoadRules: %w", err)
    }
    return convertRulesDoc(doc.Parity), nil
}

func convertRulesDoc(doc ParityRulesDoc) ProjectRules {
    rules := ProjectRules{
        Default: ModeRules{Mode: doc.Default.Mode, MinScore: doc.Default.MinScore},
    }
    if len(doc.Components) > 0 {
        rules.Components = map[string]ComponentRules{}
        for name, c := range doc.Components {
            cr := ComponentRules{Mode: c.Mode, MinScore: c.MinScore}
            if len(c.Attributes) > 0 {
                cr.Attributes = map[string]AttributePolicy{}
                for attr, pol := range c.Attributes {
                    cr.Attributes[attr] = AttributePolicy{Policy: pol.Policy, MaxRatio: pol.MaxRatio}
                }
            }
            rules.Components[name] = cr
        }
    }
    return rules
}
```

- [ ] **Step 2: Test**

Create `pkg/parity/rules_loader_test.go`:

```go
package parity_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestLoadRules_FullExample(t *testing.T) {
    body := []byte(`
apiVersion: parity.dev/v1alpha1
kind: ProjectParityRules
parity:
  default:
    mode: warn
    minScore: 0.7
  components:
    orders-db:
      mode: block
      minScore: 0.9
      attributes:
        compute.vCPU:
          policy: exact
        compute.memoryGB:
          policy: maxRatio
          maxRatio: 2.0
        features.pointInTimeRestore:
          policy: requireAll
    analytics-warehouse:
      mode: off
`)
    rules, err := parity.LoadRules(body)
    if err != nil {
        t.Fatalf("LoadRules: %v", err)
    }
    if rules.Default.Mode != "warn" || rules.Default.MinScore != 0.7 {
        t.Errorf("default: %+v", rules.Default)
    }
    db := rules.Components["orders-db"]
    if db.Mode != "block" || db.MinScore != 0.9 {
        t.Errorf("orders-db: %+v", db)
    }
    if db.Attributes["compute.vCPU"].Policy != "exact" {
        t.Errorf("vCPU policy: %+v", db.Attributes)
    }
    if db.Attributes["compute.memoryGB"].MaxRatio != 2.0 {
        t.Errorf("memoryGB ratio: %+v", db.Attributes)
    }
    if rules.Components["analytics-warehouse"].Mode != "off" {
        t.Errorf("analytics-warehouse mode: %+v", rules.Components["analytics-warehouse"])
    }
}

func TestLoadRulesFromFile_MissingFileIsNoRules(t *testing.T) {
    rules, err := parity.LoadRulesFromFile("/nonexistent-path-deliberately/parity.yaml")
    if err != nil {
        t.Fatalf("LoadRulesFromFile: %v", err)
    }
    if rules.Default.Mode != "" || len(rules.Components) != 0 {
        t.Errorf("missing file should yield empty rules, got %+v", rules)
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v -run TestLoadRules
git add pkg/parity/rules_loader.go pkg/parity/rules_loader_test.go
git commit -m "parity: parity.yaml loader (missing file = informative-only mode)"
```

---

## Task 6: Reporter (terminal output)

**Files:**
- Create: `pkg/parity/reporter.go`
- Create: `pkg/parity/reporter_test.go`

- [ ] **Step 1: Implement reporter**

Create `pkg/parity/reporter.go`:

```go
package parity

import (
    "fmt"
    "io"
    "sort"
    "strings"
)

// RenderText prints a human-readable parity report to w.
func RenderText(w io.Writer, report *ParityReport) {
    fmt.Fprintf(w, "Component: %s  (%s", report.Component, report.Type)
    if report.Size != "" {
        fmt.Fprintf(w, ", size=%s", report.Size)
    }
    fmt.Fprintln(w, ")")
    if report.Contract.Type != "" {
        fmt.Fprintln(w, contractLine(report.Contract))
    }
    if len(report.Targets) > 1 {
        renderComparisonTable(w, report)
    } else if len(report.Targets) == 1 {
        renderSingleTarget(w, report.Targets[0])
    }
    fmt.Fprintf(w, "\nParity score: %.2f", report.Score)
    if report.Score >= 0.95 {
        fmt.Fprint(w, "  (excellent)")
    } else if report.Score >= 0.7 {
        fmt.Fprint(w, "  (good)")
    } else {
        fmt.Fprint(w, "  (divergent)")
    }
    fmt.Fprintln(w)
    if len(report.Warnings) > 0 {
        fmt.Fprintln(w, "Warnings:")
        for _, w2 := range report.Warnings {
            fmt.Fprintf(w, "  - %s\n", w2)
        }
    }
}

// RenderViolations prints violations one per line.
func RenderViolations(w io.Writer, violations []Violation) {
    if len(violations) == 0 {
        fmt.Fprintln(w, "Rule violations: none")
        return
    }
    fmt.Fprintf(w, "Rule violations (%d):\n", len(violations))
    for _, v := range violations {
        attr := v.Attribute
        if attr == "" {
            attr = "(component)"
        }
        fmt.Fprintf(w, "  [%s] %s %s/%s: %s\n", v.Action, v.Component, attr, v.Policy, v.Detail)
    }
}

func contractLine(f ContractFloor) string {
    parts := []string{}
    if f.Compute != nil {
        parts = append(parts, fmt.Sprintf("vCPU>=%d, RAM>=%v GiB", f.Compute.MinVCPU, f.Compute.MinMemoryGB))
    }
    if f.Storage != nil && f.Storage.MinSizeGB > 0 {
        parts = append(parts, fmt.Sprintf("storage>=%d GiB", f.Storage.MinSizeGB))
    }
    if len(f.Features) > 0 {
        feats := []string{}
        for k := range f.Features {
            feats = append(feats, k)
        }
        sort.Strings(feats)
        parts = append(parts, "requires: "+strings.Join(feats, ", "))
    }
    return "Contract floor: " + strings.Join(parts, "; ")
}

func renderSingleTarget(w io.Writer, t TargetProfile) {
    fmt.Fprintf(w, "\nTarget %s/%s  SKU=%s\n", t.Cloud, t.Region, t.Profile.SKU)
}

func renderComparisonTable(w io.Writer, r *ParityReport) {
    targets := r.Targets
    fmt.Fprintln(w)
    // Header: attribute + one col per target.
    headers := []string{"Attribute"}
    for _, t := range targets {
        headers = append(headers, t.Cloud+"/"+t.Region)
    }
    rows := [][]string{headers}
    for _, c := range r.Comparisons {
        row := []string{c.Attribute}
        for _, t := range targets {
            v := c.Values[t.Cloud+"/"+t.Region]
            row = append(row, fmt.Sprintf("%v", v))
        }
        rows = append(rows, row)
    }
    printAlignedRows(w, rows)
}

func printAlignedRows(w io.Writer, rows [][]string) {
    if len(rows) == 0 {
        return
    }
    widths := make([]int, len(rows[0]))
    for _, row := range rows {
        for i, cell := range row {
            if len(cell) > widths[i] {
                widths[i] = len(cell)
            }
        }
    }
    for _, row := range rows {
        for i, cell := range row {
            fmt.Fprintf(w, "%-*s  ", widths[i], cell)
        }
        fmt.Fprintln(w)
    }
}
```

- [ ] **Step 2: Test**

Create `pkg/parity/reporter_test.go`:

```go
package parity_test

import (
    "bytes"
    "strings"
    "testing"

    "github.com/klehmer/nimbusfab/pkg/parity"
)

func TestRenderText_BasicShape(t *testing.T) {
    rep := &parity.ParityReport{
        Component: "orders-db", Type: "database", Size: "small",
        Score: 0.85,
        Targets: []parity.TargetProfile{
            {Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
                Class: "database", SKU: "db.t3.medium",
                Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 250}},
            }},
        },
        Warnings: []string{"aws/us-east-1: example warning"},
    }
    var buf bytes.Buffer
    parity.RenderText(&buf, rep)
    out := buf.String()
    if !strings.Contains(out, "orders-db") {
        t.Errorf("missing component: %s", out)
    }
    if !strings.Contains(out, "Parity score:") {
        t.Errorf("missing score line: %s", out)
    }
    if !strings.Contains(out, "Warnings:") || !strings.Contains(out, "example warning") {
        t.Errorf("missing warnings: %s", out)
    }
}

func TestRenderViolations_Empty(t *testing.T) {
    var buf bytes.Buffer
    parity.RenderViolations(&buf, nil)
    if !strings.Contains(buf.String(), "none") {
        t.Errorf("expected 'none' for empty: %s", buf.String())
    }
}

func TestRenderViolations_Populated(t *testing.T) {
    var buf bytes.Buffer
    parity.RenderViolations(&buf, []parity.Violation{
        {Component: "db", Attribute: "compute.vCPU", Policy: "exact", Detail: "differ", Action: "warn"},
    })
    out := buf.String()
    if !strings.Contains(out, "[warn]") || !strings.Contains(out, "compute.vCPU") {
        t.Errorf("missing violation: %s", out)
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/parity/ -v -run "TestRender"
git add pkg/parity/reporter.go pkg/parity/reporter_test.go
git commit -m "parity: terminal reporter (RenderText for reports, RenderViolations for rules output)"
```

---

## Task 7: Engine integration — compute parity per Plan

**Files:**
- Modify: `pkg/provisioner/types.go` (add `Profile []parity.TargetProfile` to TargetPlan; was reserved already)
- Modify: `pkg/provisioner/plan.go` (after Emit, call adapter.Profile per primitive and stuff into TargetPlan)
- Create: `pkg/provisioner/parity_test.go`
- Modify: `pkg/engine/engine.go` (add ParityReports field to PlanResult — but PlanResult is aliased to provisioner.PlanResult, so it goes there)

Actually, since `PlanResult = provisioner.PlanResult`, the engine change is just adding the field to provisioner's PlanResult. The CLI then renders.

- [ ] **Step 1: Extend PlanResult and TargetPlan**

In `pkg/provisioner/types.go`, ADD to `TargetPlan` (next to the existing fields):

```go
type TargetPlan struct {
    // ... existing fields ...

    // PrimitiveProfiles holds parity.TargetProfile-shaped data — one entry
    // per emitted primitive that has a meaningful profile. Adapters that
    // return ErrProfileUnavailable for a primitive get nothing here.
    PrimitiveProfiles []parity.TargetProfile
}
```

Add `"github.com/klehmer/nimbusfab/pkg/parity"` to imports.

In `PlanResult`, ADD:

```go
type PlanResult struct {
    // ... existing fields ...

    // ParityReports holds one report per component (across its targets).
    // Empty when no parity engine is configured or when components don't have
    // multi-target deployments worth comparing.
    ParityReports []parity.ParityReport
}
```

- [ ] **Step 2: Plan-time profile collection + parity aggregation**

In `pkg/provisioner/plan.go`'s `planOne`, AFTER the `injectFrameworkTags` loop, ADD:

```go
// Collect parity profiles per primitive (drop unavailable ones).
var profiles []parity.TargetProfile
for _, p := range primitives {
    prof, perr := adapter.Profile(ctx, p)
    if perr != nil {
        continue
    }
    profiles = append(profiles, parity.TargetProfile{
        DeploymentTargetID: "", // filled in by inventory layer
        Cloud:              target.Cloud,
        Region:             target.Region,
        Profile:            prof,
    })
}
```

And modify the returned TargetPlan to include `PrimitiveProfiles: profiles`.

Then in `Plan()` itself, AFTER the per-target loop completes, aggregate parity per component:

```go
// Aggregate parity reports per component.
if engine, perr := parity.NewEngine(); perr == nil {
    res.ParityReports = aggregateParityReports(ctx, engine, in.Project, res.Targets)
}
```

Helper at the bottom of `plan.go`:

```go
func aggregateParityReports(ctx context.Context, e parity.Engine, project *ir.Project, targets []TargetPlan) []parity.ParityReport {
    // Group target plans by component.
    byComp := map[string][]TargetPlan{}
    for _, tp := range targets {
        byComp[tp.Component] = append(byComp[tp.Component], tp)
    }
    var out []parity.ParityReport
    for compName, comps := range byComp {
        // Find the IR component to get type + size.
        var compType, size string
        for _, c := range project.Components {
            if c.Name == compName {
                compType = c.Type
                if sz, ok := c.Spec["size"].(string); ok {
                    size = sz
                }
                break
            }
        }
        // Build TargetProfile list across all targets of this component.
        // Where multiple primitives have profiles, pick the first non-empty
        // one with a matching class (the "primary" primitive — e.g., the
        // aws_db_instance for a database component).
        var perTarget []parity.TargetProfile
        for _, tp := range comps {
            if prof, ok := pickPrimaryProfile(tp.PrimitiveProfiles, compType); ok {
                perTarget = append(perTarget, prof)
            }
        }
        if len(perTarget) == 0 {
            continue
        }
        rep, err := e.Compare(ctx, parity.CompareInput{
            Component: compName, Type: compType, Size: size, Targets: perTarget,
        })
        if err == nil && rep != nil {
            out = append(out, *rep)
        }
    }
    return out
}

// pickPrimaryProfile finds the profile whose Class matches the component type.
// E.g., for a "database" component, prefers the profile with Class="database"
// (the aws_db_instance, not the aws_db_subnet_group).
func pickPrimaryProfile(profiles []parity.TargetProfile, compType string) (parity.TargetProfile, bool) {
    for _, p := range profiles {
        if p.Profile.Class == compType {
            return p, true
        }
    }
    // Fallback: any with a populated profile.
    for _, p := range profiles {
        if p.Profile.Class != "" {
            return p, true
        }
    }
    return parity.TargetProfile{}, false
}
```

Add `"github.com/klehmer/nimbusfab/pkg/parity"` to plan.go's imports.

- [ ] **Step 3: Test**

Create `pkg/provisioner/parity_test.go`:

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

func TestPlan_AggregatesParityReports(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    p, _ := provisioner.New(provisioner.Config{
        WorkRoot: t.TempDir(),
        Adapters: reg,
        Runner:   tofu.NewFakeRunner(),
    })
    project := &ir.Project{
        APIVersion: ir.APIVersionV1Alpha1, Name: "x",
        Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
        Components: []ir.Component{{
            Name: "web", Type: "compute",
            Spec: map[string]any{"size": "small"},
            Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
        }},
    }
    res, err := p.Plan(context.Background(), provisioner.PlanInput{
        Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-p",
    })
    if err != nil {
        t.Fatalf("Plan: %v", err)
    }
    if len(res.ParityReports) != 1 {
        t.Fatalf("ParityReports len = %d, want 1", len(res.ParityReports))
    }
    rep := res.ParityReports[0]
    if rep.Component != "web" || rep.Type != "compute" {
        t.Errorf("report identity: %+v", rep)
    }
    // Single target = trivial 1.0 score.
    if rep.Score < 0.99 {
        t.Errorf("score = %f, want ~1.0", rep.Score)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ -v -run "TestPlan_Aggregates"
git add pkg/provisioner/types.go pkg/provisioner/plan.go pkg/provisioner/parity_test.go
git commit -m "provisioner: collect Profile() per primitive at plan time; aggregate parity reports per component"
```

---

## Task 8: CLI integration

**Files:**
- Modify: `cmd/cli/plan.go` (print parity summary in plan output)
- Create: `cmd/cli/parity.go` (new `nimbusfab parity` command)
- Modify: `cmd/cli/main.go` (register parity command)
- Create: `cmd/cli/parity_test.go`

- [ ] **Step 1: Print parity summary in plan output**

In `cmd/cli/plan.go`'s `runPlan`, after the existing target-summary loop, ADD:

```go
if len(result.ParityReports) > 0 {
    fmt.Fprintln(in.Stdout)
    fmt.Fprintln(in.Stdout, "Parity:")
    for _, rep := range result.ParityReports {
        marker := "OK"
        if rep.Score < 0.7 {
            marker = "DIVERGENT"
        } else if rep.Score < 0.95 {
            marker = "MINOR"
        }
        fmt.Fprintf(in.Stdout, "  [%s] %s  score=%.2f\n", marker, rep.Component, rep.Score)
        for _, w := range rep.Warnings {
            fmt.Fprintf(in.Stdout, "      ! %s\n", w)
        }
    }
}
```

- [ ] **Step 2: New parity command**

Create `cmd/cli/parity.go`:

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
    "github.com/klehmer/nimbusfab/pkg/inventory"
    "github.com/klehmer/nimbusfab/pkg/parity"
)

type parityArgs struct {
    ProjectPath    string
    Stack          string
    Component      string  // optional; restricts output to one component
    Adapters       cloud.Registry
    Runner         tofu.Runner
    Inventory      inventory.Repo
    WorkRoot       string
    Stdout, Stderr io.Writer
}

func newParityCommand() *cobra.Command {
    var stack, component string
    cmd := &cobra.Command{
        Use:   "parity [path]",
        Short: "Show parity report for a project's components across cloud targets",
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
            code := runParity(cmd.Context(), parityArgs{
                ProjectPath: projectPath, Stack: stack, Component: component,
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
    cmd.Flags().StringVar(&component, "component", "", "limit output to one component")
    _ = cmd.MarkFlagRequired("stack")
    return cmd
}

func runParity(ctx context.Context, in parityArgs) int {
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
    // Optional: load parity.yaml from project root and evaluate rules.
    rules, _ := parity.LoadRulesFromFile(in.ProjectPath + "/parity.yaml")
    pEngine, perr := parity.NewEngine()
    if perr != nil {
        fmt.Fprintf(in.Stderr, "parity engine: %v\n", perr)
        return 1
    }
    for i := range plan.ParityReports {
        rep := &plan.ParityReports[i]
        if in.Component != "" && rep.Component != in.Component {
            continue
        }
        parity.RenderText(in.Stdout, rep)
        violations, _ := pEngine.EvaluateRules(ctx, rep, rules)
        parity.RenderViolations(in.Stdout, violations)
        fmt.Fprintln(in.Stdout)
    }
    return 0
}
```

In `cmd/cli/main.go`, register: `root.AddCommand(newParityCommand())`.

- [ ] **Step 3: Test**

Create `cmd/cli/parity_test.go`:

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

func TestParityCommand_FullStackFixture(t *testing.T) {
    reg := cloud.NewRegistry()
    _ = reg.Register(aws.New())
    var stdout, stderr bytes.Buffer
    code := runParity(context.Background(), parityArgs{
        ProjectPath: "testdata/full-stack-project",
        Stack:       "dev",
        Adapters:    reg, Runner: tofu.NewFakeRunner(), Inventory: inventory.NewNullRepo(),
        WorkRoot: t.TempDir(),
        Stdout:   &stdout, Stderr: &stderr,
    })
    if code != 0 {
        t.Errorf("exit %d stderr=%s", code, stderr.String())
    }
    out := stdout.String()
    if !strings.Contains(out, "Parity score:") {
        t.Errorf("no parity score in output: %s", out)
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./cmd/cli/ -v -run "TestParityCommand"
git add cmd/cli/plan.go cmd/cli/parity.go cmd/cli/parity_test.go cmd/cli/main.go
git commit -m "cli: nimbusfab parity command + parity summary in plan output"
```

---

## Task 9: README + CHANGELOG

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: README**

Add to the Status line + new section:

```markdown
**Status:** ... Parity Engine Phase 1 lands the parity scorer + contract floors;
every plan emits a parity report.

### `nimbusfab parity --stack <stack> [path]`

Renders a parity report per component: contract floor, per-cloud values,
weighted parity score, rule-violation summary (parity.yaml when present).
Single-cloud reports score 1.0 trivially; once Azure / GCP land, real
divergence surfaces here.
```

- [ ] **Step 2: CHANGELOG**

```markdown
## Unreleased — Parity Engine Phase 1

### Added

- `pkg/parity.NewEngine` — public parity surface: `Compare()` builds
  per-component reports; `EvaluateRules()` applies parity.yaml policies.
- Embedded contract-floor catalog (`pkg/parity/contracts/*.yaml`) for the
  four v1 types (database / compute / storage / network).
- Score function: per-attribute numeric / exact / boolean comparisons
  with weighted mean and feature-group averaging.
- Rule evaluator: per-component minScore + per-attribute exact / maxRatio /
  requireAll policies; per-component warn / block / off modes.
- `parity.LoadRulesFromFile` for parity.yaml; missing file = informative-only.
- `parity.RenderText` + `RenderViolations` terminal reporters.
- Provisioner integration: every `Plan()` collects `Profile()` per primitive
  and aggregates `ParityReport`s per component into `PlanResult`.
- CLI: `nimbusfab plan` prints per-component parity summary; new
  `nimbusfab parity --stack <stack>` command surfaces detailed reports.
```

- [ ] **Step 3: Final verification**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH go vet ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l .
```

All pass, vet clean, no formatting drift.

- [ ] **Step 4: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: README + CHANGELOG for Parity Engine Phase 1"
```

---

## Final state

`nimbusfab plan` against any project now emits a parity report per
component. With AWS only, scores are trivially 1.0 (single target per
component). When Azure or GCP adapters land, real cross-cloud divergence
surfaces — contract floors validate adapter choices satisfy minimums;
maxRatio rules catch hidden cost / capacity drift; requireAll rules
enforce features like Point-In-Time Restore across all clouds.

`nimbusfab parity <stack>` provides on-demand per-component detail with
optional rule evaluation against `parity.yaml`. Cost estimator and web
app phases consume these reports without further engine changes.
