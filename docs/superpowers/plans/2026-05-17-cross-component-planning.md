# Cross-Component Planning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `data.terraform_remote_state` cross-component wiring with provisioner-bound tofu variables, toposort components and targets at plan/apply/destroy/drift, and make `nimbusfab plan` produce a usable artifact for multi-component projects in one CLI invocation.

**Architecture:** Approach B from `docs/superpowers/specs/2026-05-17-cross-component-planning-design.md` — `${var.upstream_<component>_<output>}` interpolations in dependent workspaces, `output {}` blocks in upstream workspaces (emitted via new `Adapter.OutputBindings`), typed placeholders at plan time, real values from upstream `terraform.tfstate` at apply time.

**Tech Stack:** Go 1.22; OpenTofu 1.7+ via `internal/tofu.ExecRunner`; existing `pkg/provisioner` / `pkg/components` / `pkg/cloud` packages.

**Working spec:** `docs/superpowers/specs/2026-05-17-cross-component-planning-design.md`

---

## Pre-flight

Build/test commands used throughout. Run from repo root.

```bash
export PATH=$HOME/.local/go/bin:$HOME/.local/bin:$PATH
go test ./...                                # unit tests (FakeRunner only)
go test -tags=integration ./cmd/cli/...      # integration tests (real tofu; needs ~/.local/bin/tofu)
go build ./...                               # compile check
```

`Type.Outputs()` already returns `map[string]OutputType{Kind, Description}` per `pkg/components/registry.go:46`. We extend this — we don't redefine it. Existing `Kind` strings: `"string"`, `"list<string>"`, `"integer"`. We map these to tofu types in Task 1.

---

### Task 1: Add `OutputType.TofuType()` mapping

**Files:**
- Modify: `pkg/components/registry.go` (extend `OutputType` struct)
- Create: `pkg/components/output_type_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/components/output_type_test.go`:

```go
package components

import "testing"

func TestOutputType_TofuType(t *testing.T) {
	tests := []struct {
		kind, want string
	}{
		{"string", "string"},
		{"list<string>", "list(string)"},
		{"integer", "number"},
		{"number", "number"},
		{"bool", "bool"},
		{"", "string"}, // default
		{"unknown_kind", "string"},
	}
	for _, tc := range tests {
		got := OutputType{Kind: tc.kind}.TofuType()
		if got != tc.want {
			t.Errorf("Kind=%q: got %q, want %q", tc.kind, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/components/ -run TestOutputType_TofuType
```

Expected: FAIL — `OutputType.TofuType undefined`.

- [ ] **Step 3: Add the method**

Append to `pkg/components/registry.go` (after the existing `OutputType` struct definition):

```go
// TofuType returns the OpenTofu HCL type expression for this output's Kind.
// Unknown / empty kinds default to "string" because tofu can coerce many
// values into strings safely.
func (o OutputType) TofuType() string {
	switch o.Kind {
	case "string", "":
		return "string"
	case "list<string>":
		return "list(string)"
	case "integer", "number":
		return "number"
	case "bool":
		return "bool"
	default:
		return "string"
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./pkg/components/ -run TestOutputType_TofuType
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/components/registry.go pkg/components/output_type_test.go
git commit -m "components: OutputType.TofuType() maps Kind to tofu HCL type"
```

---

### Task 2: New `pkg/provisioner/upstream` package — `VarName` + `Toposort`

**Files:**
- Create: `pkg/provisioner/upstream/upstream.go`
- Create: `pkg/provisioner/upstream/varname_test.go`
- Create: `pkg/provisioner/upstream/toposort_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/provisioner/upstream/varname_test.go`:

```go
package upstream

import "testing"

func TestVarName(t *testing.T) {
	tests := []struct {
		comp, output, want string
	}{
		{"web-network", "vpc_id", "upstream_web_network_vpc_id"},
		{"WebNet", "subnet_ids", "upstream_webnet_subnet_ids"},
		{"3net", "x", "upstream__3net_x"},
		{"orders-db", "endpoint", "upstream_orders_db_endpoint"},
	}
	for _, tc := range tests {
		if got := VarName(tc.comp, tc.output); got != tc.want {
			t.Errorf("VarName(%q,%q)=%q want %q", tc.comp, tc.output, got, tc.want)
		}
	}
}
```

Create `pkg/provisioner/upstream/toposort_test.go`:

```go
package upstream

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func mkComp(name string, refs ...string) ir.Component {
	out := ir.Component{Name: name}
	for _, r := range refs {
		out.Refs = append(out.Refs, ir.ComponentRef{Component: r, Output: "x", As: "x"})
	}
	return out
}

func names(cs []ir.Component) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func TestToposort_Empty(t *testing.T) {
	got, err := Toposort(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestToposort_Single(t *testing.T) {
	got, err := Toposort([]ir.Component{mkComp("a")})
	if err != nil || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %v, err=%v", names(got), err)
	}
}

func TestToposort_Linear(t *testing.T) {
	// b depends on a, c depends on b. Source order is reversed.
	in := []ir.Component{mkComp("c", "b"), mkComp("b", "a"), mkComp("a")}
	got, err := Toposort(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if strings.Join(names(got), ",") != "a,b,c" {
		t.Fatalf("got %v want a,b,c", names(got))
	}
}

func TestToposort_Diamond(t *testing.T) {
	// d depends on b,c; b,c each depend on a. a must be first, d last.
	in := []ir.Component{mkComp("d", "b", "c"), mkComp("b", "a"), mkComp("c", "a"), mkComp("a")}
	got, err := Toposort(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	pos := map[string]int{}
	for i, c := range got {
		pos[c.Name] = i
	}
	if pos["a"] >= pos["b"] || pos["a"] >= pos["c"] || pos["b"] >= pos["d"] || pos["c"] >= pos["d"] {
		t.Fatalf("bad order: %v", names(got))
	}
}

func TestToposort_StableSecondary(t *testing.T) {
	// Two independent components: should sort alphabetically by name.
	in := []ir.Component{mkComp("z"), mkComp("a"), mkComp("m")}
	got, _ := Toposort(in)
	if strings.Join(names(got), ",") != "a,m,z" {
		t.Fatalf("got %v want a,m,z", names(got))
	}
}

func TestToposort_CycleIsInternalError(t *testing.T) {
	// Validator Phase 5 should prevent this in practice; toposort returns an
	// error rather than looping.
	in := []ir.Component{mkComp("a", "b"), mkComp("b", "a")}
	if _, err := Toposort(in); err == nil {
		t.Fatalf("expected cycle error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/provisioner/upstream/...
```

Expected: FAIL — no such package.

- [ ] **Step 3: Implement the package**

Create `pkg/provisioner/upstream/upstream.go`:

```go
// Package upstream owns cross-component planning machinery: variable
// naming, topological ordering, dependent-to-upstream pairing, and
// extraction of upstream output values from tofu state.
package upstream

import (
	"fmt"
	"sort"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// VarName builds the tofu variable name for one (component, output) pair.
// Component names are sanitized with the same rules as workspace tofu local
// names (lowercase, alnum + underscore; leading non-alpha prefixed with '_').
func VarName(component, output string) string {
	return "upstream_" + sanitizeIdent(component) + "_" + output
}

func sanitizeIdent(s string) string {
	out := make([]byte, 0, len(s)+1)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 || (out[0] >= '0' && out[0] <= '9') {
		out = append([]byte{'_'}, out...)
	}
	return string(out)
}

// Toposort returns components in dependency-first order: for any ref A→B,
// B appears before A. Stable secondary sort on component name for
// determinism. Returns an error on cycle (validator Phase 5 should
// prevent that; this is an internal-error guard).
func Toposort(components []ir.Component) ([]ir.Component, error) {
	if len(components) == 0 {
		return nil, nil
	}

	byName := make(map[string]ir.Component, len(components))
	names := make([]string, 0, len(components))
	for _, c := range components {
		if _, exists := byName[c.Name]; exists {
			return nil, fmt.Errorf("upstream.Toposort: duplicate component name %q", c.Name)
		}
		byName[c.Name] = c
		names = append(names, c.Name)
	}
	sort.Strings(names)

	indegree := make(map[string]int, len(components))
	dependents := make(map[string][]string, len(components))
	for _, name := range names {
		indegree[name] = 0
	}
	for _, name := range names {
		comp := byName[name]
		for _, ref := range comp.Refs {
			if _, ok := byName[ref.Component]; !ok {
				continue // unknown ref: validator should have caught
			}
			indegree[name]++
			dependents[ref.Component] = append(dependents[ref.Component], name)
		}
	}

	var ready []string
	for _, name := range names {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)

	out := make([]ir.Component, 0, len(components))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		out = append(out, byName[next])
		deps := append([]string{}, dependents[next]...)
		sort.Strings(deps)
		for _, d := range deps {
			indegree[d]--
			if indegree[d] == 0 {
				ready = append(ready, d)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(components) {
		return nil, fmt.Errorf("upstream.Toposort: cycle detected (placed %d of %d components)", len(out), len(components))
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/upstream/...
```

Expected: PASS (all 5 toposort tests + VarName test).

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/upstream/
git commit -m "provisioner/upstream: VarName + Toposort with cycle detection"
```

---

### Task 3: `PlanPlaceholders` — typed placeholders per ref

**Files:**
- Modify: `pkg/provisioner/upstream/upstream.go` (add `PlanPlaceholders`)
- Create: `pkg/provisioner/upstream/placeholders_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/provisioner/upstream/placeholders_test.go`:

```go
package upstream

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPlanPlaceholders_NetworkRef(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{
		{Component: "web-network", Output: "vpc_id", As: "vpcId"},
		{Component: "web-network", Output: "subnet_ids", As: "subnetIds"},
	}
	netComp := ir.Component{Name: "web-network", Type: "network"}
	got, err := PlanPlaceholders(refs, []ir.Component{netComp}, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if want := `"__nimbusfab_placeholder_upstream_web_network_vpc_id__"`; got["upstream_web_network_vpc_id"] != want {
		t.Errorf("vpc_id: got %q want %q", got["upstream_web_network_vpc_id"], want)
	}
	v := got["upstream_web_network_subnet_ids"]
	if !strings.HasPrefix(v, `["__nimbusfab_placeholder_`) || !strings.HasSuffix(v, `__"]`) {
		t.Errorf("subnet_ids placeholder shape unexpected: %q", v)
	}
}

func TestPlanPlaceholders_DatabaseRef(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{{Component: "db", Output: "port", As: "port"}}
	dbComp := ir.Component{Name: "db", Type: "database"}
	got, err := PlanPlaceholders(refs, []ir.Component{dbComp}, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["upstream_db_port"] != "0" {
		t.Errorf("port: got %q want 0", got["upstream_db_port"])
	}
}

func TestPlanPlaceholders_UnknownComponentIgnored(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{{Component: "ghost", Output: "x", As: "x"}}
	got, err := PlanPlaceholders(refs, nil, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map; got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/upstream/ -run TestPlanPlaceholders
```

Expected: FAIL — `PlanPlaceholders undefined`.

- [ ] **Step 3: Implement `PlanPlaceholders`**

Append to `pkg/provisioner/upstream/upstream.go`:

```go
import (
	// ... existing imports ...
	"github.com/klehmer/nimbusfab/pkg/components"
)

// PlanPlaceholders builds {varName: hcl-formatted-placeholder} for each ref
// declared on a dependent component. The component's upstream Type is looked
// up via the registry to determine each output's TofuType, then a structural
// placeholder of that type is encoded as an HCL literal suitable for a
// `tofu plan -var name=value` flag.
//
// Returned values are HCL literals: strings are double-quoted; lists are
// JSON-arrays-of-strings (which tofu accepts as list(string)); numbers and
// bools are bare tokens. Refs pointing at components not in `all` are
// silently dropped; the validator's Phase 5 catches structural errors.
func PlanPlaceholders(refs []ir.ComponentRef, all []ir.Component, reg components.Registry) (map[string]string, error) {
	out := map[string]string{}
	byName := map[string]ir.Component{}
	for _, c := range all {
		byName[c.Name] = c
	}
	for _, r := range refs {
		upstream, ok := byName[r.Component]
		if !ok {
			continue
		}
		typ, ok := reg.Type(upstream.Type)
		if !ok {
			continue
		}
		outDecl, ok := typ.Outputs()[r.Output]
		if !ok {
			continue
		}
		name := VarName(r.Component, r.Output)
		out[name] = placeholderFor(name, outDecl.TofuType())
	}
	return out, nil
}

func placeholderFor(varName, tofuType string) string {
	switch tofuType {
	case "string":
		return `"__nimbusfab_placeholder_` + varName + `__"`
	case "list(string)":
		return `["__nimbusfab_placeholder_` + varName + `_0__"]`
	case "number":
		return "0"
	case "bool":
		return "false"
	default:
		return `"__nimbusfab_placeholder_` + varName + `__"`
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/upstream/...
```

Expected: PASS (all tests including new ones).

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/upstream/
git commit -m "provisioner/upstream: PlanPlaceholders generates typed HCL placeholders"
```

---

### Task 4: `Pair` + `ToposortTargets` for apply-time ordering

**Files:**
- Modify: `pkg/provisioner/upstream/upstream.go` (add `Pair` + `ToposortTargets` + `ErrCrossTargetRefUnsupported`)
- Create: `pkg/provisioner/upstream/pairing_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/provisioner/upstream/pairing_test.go`:

```go
package upstream

import (
	"errors"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

type tplan struct{ Component, Cloud, Region string }

func TestPair_ExactMatch(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "net", Cloud: "aws", Region: "us-west-2"},
		{Component: "app", Cloud: "aws", Region: "us-east-1"},
	}
	got, err := Pair(TargetIdent{Component: "app", Cloud: "aws", Region: "us-east-1"}, "net", all)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.Region != "us-east-1" {
		t.Fatalf("got region %s", got.Region)
	}
}

func TestPair_CrossRegionFails(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "aws", Region: "us-west-2"},
	}
	_, err := Pair(TargetIdent{Component: "app", Cloud: "aws", Region: "us-west-2"}, "net", all)
	if !errors.Is(err, ErrCrossTargetRefUnsupported) {
		t.Fatalf("got %v, want ErrCrossTargetRefUnsupported", err)
	}
}

func TestPair_CrossCloudFails(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "azure", Region: "us-east-1"},
	}
	_, err := Pair(TargetIdent{Component: "app", Cloud: "azure", Region: "us-east-1"}, "net", all)
	if !errors.Is(err, ErrCrossTargetRefUnsupported) {
		t.Fatalf("got %v, want ErrCrossTargetRefUnsupported", err)
	}
}

func TestToposortTargets(t *testing.T) {
	// net→app via component-toposort; each component has 2 targets.
	components := []ir.Component{
		{Name: "app", Refs: []ir.ComponentRef{{Component: "net", Output: "vpc_id", As: "v"}}},
		{Name: "net"},
	}
	targets := []TargetIdent{
		{Component: "app", Cloud: "aws", Region: "us-east-1"},
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "azure", Region: "eastus"},
		{Component: "net", Cloud: "azure", Region: "eastus"},
	}
	got, err := ToposortTargets(targets, components)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	// All net targets must appear before all app targets.
	netIdx, appIdx := -1, -1
	for i, ti := range got {
		switch ti.Component {
		case "net":
			netIdx = i
		case "app":
			if appIdx == -1 {
				appIdx = i
			}
		}
	}
	if netIdx >= appIdx {
		var lines []string
		for _, ti := range got {
			lines = append(lines, ti.Component+"/"+ti.Cloud)
		}
		t.Fatalf("expected all net targets before app targets, got %s", strings.Join(lines, ","))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/upstream/ -run TestPair
```

Expected: FAIL — `Pair / TargetIdent / ErrCrossTargetRefUnsupported undefined`.

- [ ] **Step 3: Implement**

Append to `pkg/provisioner/upstream/upstream.go`:

```go
import (
	// ... existing imports ...
	"errors"
)

// ErrCrossTargetRefUnsupported fires when a dependent target has no upstream
// target in the same (cloud, region). v1.1 explicitly does not support
// cross-cloud or cross-region refs.
var ErrCrossTargetRefUnsupported = errors.New("cross-target ref unsupported (no matching upstream target in same cloud/region)")

// TargetIdent is the (component, cloud, region) tuple uniquely identifying a
// deployment target for ordering and pairing purposes.
type TargetIdent struct {
	Component string
	Cloud     string
	Region    string
}

// Pair finds the upstream target matching dep's (cloud, region). Returns
// ErrCrossTargetRefUnsupported if no exact match exists.
func Pair(dep TargetIdent, upstream string, all []TargetIdent) (TargetIdent, error) {
	for _, t := range all {
		if t.Component == upstream && t.Cloud == dep.Cloud && t.Region == dep.Region {
			return t, nil
		}
	}
	return TargetIdent{}, fmt.Errorf("%w: %s in %s/%s needs %s",
		ErrCrossTargetRefUnsupported, dep.Component, dep.Cloud, dep.Region, upstream)
}

// ToposortTargets orders targets by (component-toposort-rank, cloud, region).
// All targets of an upstream component appear before any target of a
// downstream component, regardless of (cloud, region).
func ToposortTargets(targets []TargetIdent, comps []ir.Component) ([]TargetIdent, error) {
	ordered, err := Toposort(comps)
	if err != nil {
		return nil, err
	}
	rank := map[string]int{}
	for i, c := range ordered {
		rank[c.Name] = i
	}
	out := make([]TargetIdent, len(targets))
	copy(out, targets)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Component], rank[out[j].Component]
		if ri != rj {
			return ri < rj
		}
		if out[i].Cloud != out[j].Cloud {
			return out[i].Cloud < out[j].Cloud
		}
		return out[i].Region < out[j].Region
	})
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/upstream/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/upstream/
git commit -m "provisioner/upstream: Pair + ToposortTargets + ErrCrossTargetRefUnsupported"
```

---

### Task 5: `Adapter.OutputBindings` interface + AWS implementation

**Files:**
- Modify: `pkg/cloud/adapter.go` (add `OutputBindings` to `Adapter` interface)
- Modify: `pkg/cloud/fake_adapter.go` (implement on fake)
- Create: `internal/cloud/aws/outputs.go`
- Create: `internal/cloud/aws/outputs_test.go`
- Modify: any callsite of `cloud.Adapter` that needs the new method on every concrete adapter

- [ ] **Step 1: Write the failing test**

Create `internal/cloud/aws/outputs_test.go`:

```go
package aws

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_Network(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2},
	}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, err := a.OutputBindings(context.Background(), target, prim)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["vpc_id"] != "${aws_vpc.web_network.id}" {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	if got["subnet_ids"] != "[${aws_subnet.web_network_0.id}, ${aws_subnet.web_network_1.id}]" {
		t.Errorf("subnet_ids: got %q", got["subnet_ids"])
	}
}

func TestOutputBindings_Compute(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "web-app", "__type": "compute",
			"size": "small", "instanceCount": 1,
			"subnetId": "${var.upstream_web_network_subnet_ids[0]}"},
	}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, err := a.OutputBindings(context.Background(), target, prim)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["security_group_id"] == "" {
		t.Errorf("security_group_id missing: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/cloud/aws/ -run TestOutputBindings
```

Expected: FAIL — `OutputBindings undefined`.

- [ ] **Step 3: Add the interface method, then implement on AWS**

Append to `pkg/cloud/adapter.go` (after the existing `Adapter` interface):

```go
// OutputBindings returns the tofu expressions for each output declared by
// the component's Type.Outputs(). The provisioner writes these as
// `output "<name>" { value = <expr> }` blocks in the upstream's workspace
// so the values land in terraform.tfstate after apply, where the dependent
// can read them.
//
// Adapters that don't implement this return an empty map; the provisioner
// treats absence-of-bindings as "this adapter has no cross-component outputs."
// All v1 adapters MUST implement this for all v1 component types.
//
// Required since v1.1.
func _outputBindingsMarker() {} // doc anchor; not called.
```

Modify the `Adapter` interface in `pkg/cloud/adapter.go` (immediately after `ProviderBlock`, lines ~70):

```go
	// OutputBindings returns the tofu expressions for outputs declared by
	// this component's Type. Keyed by output name (e.g., "vpc_id"); values
	// are tofu HCL expressions written verbatim into output blocks.
	OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error)
```

Implement on the fake adapter in `pkg/cloud/fake_adapter.go` (add method):

```go
func (a *FakeAdapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	return map[string]string{}, nil
}
```

Create `internal/cloud/aws/outputs.go`:

```go
package aws

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// OutputBindings returns the tofu expression per component-type output that
// the workspace renderer turns into an `output {}` block. Keys must match
// the component Type.Outputs() declaration.
func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	t, _ := target.Spec["__type"].(string)
	switch t {
	case "network":
		return outputsNetwork(primitives), nil
	case "compute":
		return outputsCompute(primitives), nil
	case "database":
		return outputsDatabase(primitives), nil
	case "storage":
		return outputsStorage(primitives), nil
	}
	return map[string]string{}, nil
}

func outputsNetwork(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var subnetNames []string
	var routeTableNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "aws_vpc":
			out["vpc_id"] = fmt.Sprintf("${aws_vpc.%s.id}", p.TofuName)
		case "aws_subnet":
			subnetNames = append(subnetNames, p.TofuName)
		case "aws_route_table":
			routeTableNames = append(routeTableNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	sort.Strings(routeTableNames)
	out["subnet_ids"] = listExpr("aws_subnet", subnetNames, "id")
	out["route_table_ids"] = listExpr("aws_route_table", routeTableNames, "id")
	return out
}

func outputsCompute(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var instanceNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "aws_instance":
			instanceNames = append(instanceNames, p.TofuName)
		case "aws_security_group":
			out["security_group_id"] = fmt.Sprintf("${aws_security_group.%s.id}", p.TofuName)
		}
	}
	sort.Strings(instanceNames)
	out["instance_ids"] = listExpr("aws_instance", instanceNames, "id")
	out["private_ips"] = listExpr("aws_instance", instanceNames, "private_ip")
	return out
}

func outputsDatabase(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "aws_db_instance" {
			out["endpoint"] = fmt.Sprintf("${aws_db_instance.%s.address}", p.TofuName)
			out["port"] = fmt.Sprintf("${aws_db_instance.%s.port}", p.TofuName)
			out["db_name"] = fmt.Sprintf("${aws_db_instance.%s.db_name}", p.TofuName)
		}
	}
	return out
}

func outputsStorage(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "aws_s3_bucket" {
			out["bucket_name"] = fmt.Sprintf("${aws_s3_bucket.%s.bucket}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${aws_s3_bucket.%s.arn}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${aws_s3_bucket.%s.bucket_regional_domain_name}", p.TofuName)
		}
	}
	return out
}

func listExpr(resourceType string, names []string, attr string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/cloud/aws/ -run TestOutputBindings
go build ./...
```

Expected: tests PASS; `go build` PASS (the new interface method is now implemented across in-tree adapters).

- [ ] **Step 5: Commit**

```bash
git add pkg/cloud/adapter.go pkg/cloud/fake_adapter.go internal/cloud/aws/outputs.go internal/cloud/aws/outputs_test.go
git commit -m "cloud: Adapter.OutputBindings interface + AWS implementation"
```

---

### Task 6: Azure + GCP `OutputBindings` implementations

**Files:**
- Create: `internal/cloud/azure/outputs.go`
- Create: `internal/cloud/azure/outputs_test.go`
- Create: `internal/cloud/gcp/outputs.go`
- Create: `internal/cloud/gcp/outputs_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cloud/azure/outputs_test.go`:

```go
package azure

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_AzureNetwork(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2}}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, _ := a.OutputBindings(context.Background(), target, prim)
	if !strings.HasPrefix(got["vpc_id"], "${azurerm_virtual_network.") {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	if !strings.HasPrefix(got["subnet_ids"], "[${azurerm_subnet.") {
		t.Errorf("subnet_ids: got %q", got["subnet_ids"])
	}
}
```

Create `internal/cloud/gcp/outputs_test.go`:

```go
package gcp

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_GCPNetwork(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2}}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, _ := a.OutputBindings(context.Background(), target, prim)
	if !strings.HasPrefix(got["vpc_id"], "${google_compute_network.") {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	if !strings.HasPrefix(got["subnet_ids"], "[${google_compute_subnetwork.") {
		t.Errorf("subnet_ids: got %q", got["subnet_ids"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/cloud/azure/ ./internal/cloud/gcp/ -run TestOutputBindings
```

Expected: FAIL — `OutputBindings undefined` AND the package no longer compiles because `cloud.Adapter` requires the method (left over from Task 5).

- [ ] **Step 3: Implement**

Create `internal/cloud/azure/outputs.go`:

```go
package azure

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	t, _ := target.Spec["__type"].(string)
	switch t {
	case "network":
		return azureOutputsNetwork(primitives), nil
	case "compute":
		return azureOutputsCompute(primitives), nil
	case "database":
		return azureOutputsDatabase(primitives), nil
	case "storage":
		return azureOutputsStorage(primitives), nil
	}
	return map[string]string{}, nil
}

func azureOutputsNetwork(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var subnetNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_virtual_network":
			out["vpc_id"] = fmt.Sprintf("${azurerm_virtual_network.%s.id}", p.TofuName)
		case "azurerm_subnet":
			subnetNames = append(subnetNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	out["subnet_ids"] = azureListExpr("azurerm_subnet", subnetNames, "id")
	// Azure has no direct route-table-per-subnet primitive; map to empty list.
	out["route_table_ids"] = "[]"
	return out
}

func azureOutputsCompute(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var vmNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_linux_virtual_machine":
			vmNames = append(vmNames, p.TofuName)
		case "azurerm_network_security_group":
			out["security_group_id"] = fmt.Sprintf("${azurerm_network_security_group.%s.id}", p.TofuName)
		}
	}
	sort.Strings(vmNames)
	out["instance_ids"] = azureListExpr("azurerm_linux_virtual_machine", vmNames, "id")
	out["private_ips"] = azureListExpr("azurerm_linux_virtual_machine", vmNames, "private_ip_address")
	return out
}

func azureOutputsDatabase(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_postgresql_flexible_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_postgresql_flexible_server.%s.fqdn}", p.TofuName)
			out["port"] = "5432"
			out["db_name"] = fmt.Sprintf("${azurerm_postgresql_flexible_server.%s.name}", p.TofuName)
		case "azurerm_mysql_flexible_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_mysql_flexible_server.%s.fqdn}", p.TofuName)
			out["port"] = "3306"
			out["db_name"] = fmt.Sprintf("${azurerm_mysql_flexible_server.%s.name}", p.TofuName)
		case "azurerm_mariadb_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_mariadb_server.%s.fqdn}", p.TofuName)
			out["port"] = "3306"
			out["db_name"] = fmt.Sprintf("${azurerm_mariadb_server.%s.name}", p.TofuName)
		}
	}
	return out
}

func azureOutputsStorage(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "azurerm_storage_account" {
			out["bucket_name"] = fmt.Sprintf("${azurerm_storage_account.%s.name}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${azurerm_storage_account.%s.id}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${azurerm_storage_account.%s.primary_blob_endpoint}", p.TofuName)
		}
	}
	return out
}

func azureListExpr(resourceType string, names []string, attr string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
```

Create `internal/cloud/gcp/outputs.go`:

```go
package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	t, _ := target.Spec["__type"].(string)
	switch t {
	case "network":
		return gcpOutputsNetwork(primitives), nil
	case "compute":
		return gcpOutputsCompute(primitives), nil
	case "database":
		return gcpOutputsDatabase(primitives), nil
	case "storage":
		return gcpOutputsStorage(primitives), nil
	}
	return map[string]string{}, nil
}

func gcpOutputsNetwork(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var subnetNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "google_compute_network":
			out["vpc_id"] = fmt.Sprintf("${google_compute_network.%s.id}", p.TofuName)
		case "google_compute_subnetwork":
			subnetNames = append(subnetNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	out["subnet_ids"] = gcpListExpr("google_compute_subnetwork", subnetNames, "id")
	// GCP routes are auto-managed by the VPC; no per-subnet route table.
	out["route_table_ids"] = "[]"
	return out
}

func gcpOutputsCompute(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var instNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "google_compute_instance":
			instNames = append(instNames, p.TofuName)
		case "google_compute_firewall":
			// Firewalls are GCP's SG analogue; use first declared.
			if _, set := out["security_group_id"]; !set {
				out["security_group_id"] = fmt.Sprintf("${google_compute_firewall.%s.id}", p.TofuName)
			}
		}
	}
	sort.Strings(instNames)
	out["instance_ids"] = gcpListExpr("google_compute_instance", instNames, "id")
	out["private_ips"] = gcpListExpr("google_compute_instance", instNames, "network_interface.0.network_ip")
	return out
}

func gcpOutputsDatabase(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "google_sql_database_instance" {
			out["endpoint"] = fmt.Sprintf("${google_sql_database_instance.%s.public_ip_address}", p.TofuName)
			out["port"] = "5432" // overridden by adapter for mysql, but cloud sql default
			out["db_name"] = fmt.Sprintf("${google_sql_database_instance.%s.name}", p.TofuName)
		}
	}
	return out
}

func gcpOutputsStorage(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "google_storage_bucket" {
			out["bucket_name"] = fmt.Sprintf("${google_storage_bucket.%s.name}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${google_storage_bucket.%s.id}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${google_storage_bucket.%s.url}", p.TofuName)
		}
	}
	return out
}

func gcpListExpr(resourceType string, names []string, attr string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/cloud/azure/ ./internal/cloud/gcp/
go build ./...
```

Expected: tests PASS; build PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cloud/azure/outputs.go internal/cloud/azure/outputs_test.go internal/cloud/gcp/outputs.go internal/cloud/gcp/outputs_test.go
git commit -m "azure+gcp: OutputBindings for network/compute/database/storage"
```

---

### Task 7: Workspace renderer — `variable` + `output` blocks; drop `data.terraform_remote_state`

**Files:**
- Modify: `pkg/provisioner/workspace.go`
- Modify: `pkg/provisioner/workspace_test.go` (or extend; check existing tests)

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/workspace_test.go` (or create if missing — check first with `ls pkg/provisioner/workspace_test.go`):

```go
func TestWriteWorkspace_EmitsVariableAndOutputBlocks(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			ID: "x.aws.vpc", Cloud: "aws", TofuType: "aws_vpc", TofuName: "net",
			Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
		}},
		Variables: []UpstreamVariable{
			{Name: "upstream_net_vpc_id", TofuType: "string"},
			{Name: "upstream_net_subnet_ids", TofuType: "list(string)"},
		},
		OutputBindings: map[string]string{
			"vpc_id":     "${aws_vpc.net.id}",
			"subnet_ids": "[${aws_subnet.net_0.id}]",
		},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	if err != nil {
		t.Fatalf("read main.tf.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	variables, ok := parsed["variable"].(map[string]any)
	if !ok || len(variables) != 2 {
		t.Errorf("variable block: %v", parsed["variable"])
	}
	outputs, ok := parsed["output"].(map[string]any)
	if !ok || len(outputs) != 2 {
		t.Errorf("output block: %v", parsed["output"])
	}
	// Ensure data.terraform_remote_state is GONE.
	if _, present := parsed["data"]; present {
		t.Errorf("data block should be absent in v1.1 workspaces")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestWriteWorkspace_Emits
```

Expected: FAIL — `UpstreamVariable / OutputBindings undefined`.

- [ ] **Step 3: Update `pkg/provisioner/workspace.go`**

Replace the `WorkspaceLayout` struct (lines 11-29 of current file):

```go
type WorkspaceLayout struct {
	Dir string

	ProviderName            string
	ProviderSource          string
	ProviderVersion         string
	ProviderRequiredVersion string
	ProviderConfig          map[string]any

	Backend ir.StateBackend

	Primitives []ir.ResourcePrimitive

	// Variables declares the typed tofu input variables this workspace
	// expects. The provisioner passes values via `tofu plan -var name=...`;
	// at plan time these are placeholders, at apply time real values from
	// upstream state.
	Variables []UpstreamVariable

	// OutputBindings declares the tofu expressions for outputs this
	// workspace publishes so its terraform.tfstate contains them after
	// apply. Keys are output names per components.Type.Outputs(); values
	// are HCL expressions written verbatim inside an `output {}` block.
	OutputBindings map[string]string
}

// UpstreamVariable is one tofu `variable` block declaration.
type UpstreamVariable struct {
	Name     string
	TofuType string
}
```

Delete the existing `UpstreamStateRef` type (top of `pkg/provisioner/refs.go`) — it's only used here, and that usage is being removed:

```go
// DELETE the UpstreamStateRef type from refs.go.
// (The buildResolvedRefs body is updated in Task 8.)
```

Replace `WriteWorkspace` in `pkg/provisioner/workspace.go`:

```go
func WriteWorkspace(layout WorkspaceLayout) error {
	if err := os.MkdirAll(layout.Dir, 0o700); err != nil {
		return fmt.Errorf("workspace mkdir: %w", err)
	}

	mainBlock := buildMain(layout.Primitives)
	if len(layout.Variables) > 0 {
		vars := map[string]any{}
		for _, v := range layout.Variables {
			vars[v.Name] = map[string]any{"type": v.TofuType}
		}
		mainBlock["variable"] = vars
	}
	if len(layout.OutputBindings) > 0 {
		outs := map[string]any{}
		for name, expr := range layout.OutputBindings {
			outs[name] = map[string]any{"value": expr}
		}
		mainBlock["output"] = outs
	}

	files := map[string]any{
		"versions.tf.json": buildVersions(layout),
		"provider.tf.json": map[string]any{"provider": layout.ProviderConfig},
		"backend.tf.json":  buildBackend(layout.Backend),
		"main.tf.json":     mainBlock,
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
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/ -run TestWriteWorkspace
go build ./...
```

Expected: new test PASS. Build will FAIL temporarily because `plan.go` still references `UpstreamRefs` — that's fixed in Task 9. To verify just the workspace change in isolation, comment out the `UpstreamRefs` field-use in `plan.go` temporarily, or proceed straight to Task 9.

For commit safety, **do not commit this task in isolation**; bundle it with Tasks 8 and 9 (refs.go rewrite + plan.go integration) into a single commit at the end of Task 9.

- [ ] **Step 5: Hold commit**

No commit yet — bundles with Task 9.

---

### Task 8: Rewrite `buildResolvedRefs` to emit `${var.*}`

**Files:**
- Modify: `pkg/provisioner/refs.go`
- Modify: `pkg/provisioner/plan_refs_test.go`

- [ ] **Step 1: Update the existing test**

Open `pkg/provisioner/plan_refs_test.go`. Change all expected interpolation strings from
`${data.terraform_remote_state.<X>.outputs.<Y>}` to `${var.upstream_<X>_<Y>}` (with appropriate `[0]` for the singular-of-list case). The structural assertions stay the same.

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestBuildResolvedRefs
```

Expected: FAIL — current implementation still emits `data.terraform_remote_state` strings.

- [ ] **Step 3: Rewrite `buildResolvedRefs` body**

Replace the body of `buildResolvedRefs` in `pkg/provisioner/refs.go`:

```go
func buildResolvedRefs(refs []ir.ComponentRef) cloud.ResolvedRefs {
	out := cloud.ResolvedRefs{}
	for _, r := range refs {
		if r.As == "" || r.Component == "" || r.Output == "" {
			continue
		}
		varName := upstream.VarName(r.Component, r.Output)
		base := "var." + varName
		if camelToSnake(r.As)+"s" == r.Output {
			out[r.As] = "${" + base + "[0]}"
		} else {
			out[r.As] = "${" + base + "}"
		}
	}
	return out
}
```

Add the import at the top of `pkg/provisioner/refs.go`:

```go
import (
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)
```

Delete the now-orphan `tofuIdentForComponent` helper (its only callsite was inside the old `buildResolvedRefs`) — `upstream.VarName` handles sanitization internally.

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/ -run TestBuildResolvedRefs
```

Expected: PASS.

- [ ] **Step 5: Hold commit**

No commit yet — bundles with Task 9.

---

### Task 9: Wire toposort + placeholders into `provisioner.Plan` + Vars on Workspace

**Files:**
- Modify: `internal/tofu/exec_runner.go` (use `Workspace.Vars` for `-var` flags on Plan/Destroy)
- Modify: `internal/tofu/runner.go` (no change to `Workspace.Vars` field; just confirm it's `map[string]any`)
- Modify: `pkg/provisioner/plan.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/plan_test.go`:

```go
func TestProvisionerPlan_ToposortsAndPassesPlaceholders(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			// Dependent declared FIRST in source order — toposort must reorder.
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1, "subnetId": "${refs.subnetId}"}}}},
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}

	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake})

	res, err := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Targets) != 2 {
		t.Fatalf("want 2 targets; got %d", len(res.Targets))
	}
	// Toposort: web-network must come first.
	if res.Targets[0].Component != "web-network" {
		t.Errorf("expected web-network first, got %s then %s", res.Targets[0].Component, res.Targets[1].Component)
	}
	// Dependent target's Plan call should have received -var values.
	var appCall *tofu.PlanCall
	for i, pc := range fake.PlanCalls {
		if filepath.Base(filepath.Dir(pc.Opts.OutFile)) == "web-app" {
			appCall = &fake.PlanCalls[i]
		}
	}
	if appCall == nil {
		t.Fatalf("no Plan call against web-app workspace")
	}
	if v, ok := appCall.Workspace.Vars["upstream_web_network_subnet_ids"]; !ok || v == nil {
		t.Errorf("missing upstream_web_network_subnet_ids in vars: %v", appCall.Workspace.Vars)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestProvisionerPlan_Toposorts
```

Expected: FAIL — current plan iterates source order and doesn't populate `Workspace.Vars`.

- [ ] **Step 3: Implement**

In `pkg/provisioner/plan.go`, change the body of `Plan` (lines 17-65 of current file) to toposort and pass placeholders:

```go
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

	ordered, err := upstream.Toposort(in.Project.Components)
	if err != nil {
		return nil, fmt.Errorf("provisioner.Plan: %w", err)
	}

	res := &PlanResult{
		DeploymentID:   in.DeploymentID,
		Stack:          in.Stack,
		PartialFailure: in.PartialFailure,
		GeneratedAt:    time.Now().UTC(),
	}

	for _, comp := range ordered {
		placeholders, perr := upstream.PlanPlaceholders(comp.Refs, in.Project.Components, rp.cfg.Components)
		if perr != nil {
			return nil, fmt.Errorf("provisioner.Plan: placeholders: %w", perr)
		}
		for _, target := range comp.Targets {
			if !matchesFilter(in.Targets, comp.Name, target.Cloud, target.Region) {
				continue
			}
			tp, err := rp.planOne(ctx, in, stack, comp, target, placeholders)
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
	if pEngine, perr := parity.NewEngine(); perr == nil {
		res.ParityReports = aggregateParityReports(ctx, pEngine, in.Project, res.Targets)
	}
	return res, nil
}
```

Update `planOne` signature to accept `placeholders map[string]string` and pass `Variables` + `OutputBindings` to `WorkspaceLayout`. Inside `planOne`, after `primitives := ...`:

```go
	// Declare variables for each placeholder so tofu accepts the -var flags.
	var varDecls []UpstreamVariable
	for name := range placeholders {
		varDecls = append(varDecls, UpstreamVariable{Name: name, TofuType: tofuTypeFromPlaceholder(placeholders[name])})
	}
	outputBindings, err := adapter.OutputBindings(ctx, target, primitives)
	if err != nil {
		return TargetPlan{}, fmt.Errorf("OutputBindings: %w", err)
	}
```

Replace the existing `layout := WorkspaceLayout{...}` construction in `planOne`:

```go
	layout := WorkspaceLayout{
		Dir:             workspaceDir,
		ProviderName:    providerLocalName,
		ProviderVersion: providerVersion,
		ProviderConfig:  providerBlock,
		Backend:         backend,
		Primitives:      primitives,
		Variables:       varDecls,
		OutputBindings:  outputBindings,
	}
```

Update the `tofu.Workspace` construction (current line ~196):

```go
	ws := tofu.Workspace{Dir: workspaceDir, Vars: placeholdersAsAny(placeholders)}
```

Add helper functions at the bottom of `plan.go`:

```go
func placeholdersAsAny(p map[string]string) map[string]any {
	if len(p) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range p {
		out[k] = v // value is a pre-formatted HCL literal string
	}
	return out
}

func tofuTypeFromPlaceholder(literal string) string {
	switch {
	case literal == "" || literal == `""`:
		return "string"
	case literal[0] == '"':
		return "string"
	case literal[0] == '[':
		return "list(string)"
	case literal == "true" || literal == "false":
		return "bool"
	default:
		return "number"
	}
}
```

Update `pkg/provisioner/provisioner.go` (the `Config` struct) to add `Components components.Registry` if it's not already present. If the field doesn't exist:

```go
type Config struct {
	// ... existing fields ...
	Components components.Registry
}
```

And in `provisioner.New`, default it:

```go
	if cfg.Components == nil {
		cfg.Components = components.DefaultRegistry()
	}
```

Update `internal/tofu/exec_runner.go` Plan method (line 76 area) — add the `-var` flag construction:

```go
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
	if opts.RefreshOnly {
		args = append(args, "-refresh-only")
	}
	for k, v := range ws.Vars {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("tofu Plan: ws.Vars[%q] is %T; must be a pre-formatted HCL literal string", k, v)
		}
		args = append(args, "-var", k+"="+s)
	}
	// ... rest unchanged ...
```

Mirror the same `-var` handling in `ExecRunner.Destroy`.

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/...
go build ./...
```

Expected: all tests PASS; build PASS.

- [ ] **Step 5: Commit (bundles Tasks 7, 8, 9)**

```bash
git add pkg/provisioner/workspace.go pkg/provisioner/workspace_test.go \
        pkg/provisioner/refs.go pkg/provisioner/plan_refs_test.go \
        pkg/provisioner/plan.go pkg/provisioner/plan_test.go \
        pkg/provisioner/provisioner.go \
        internal/tofu/exec_runner.go
git commit -m "provisioner: toposort + var placeholders replace data.terraform_remote_state"
```

---

### Task 10: `upstream.ExtractOutputs` from tofu state JSON

**Files:**
- Modify: `pkg/provisioner/upstream/upstream.go` (add `ExtractOutputs` + `ErrUpstreamStateUnreadable` + `ErrUpstreamOutputMissing`)
- Create: `pkg/provisioner/upstream/state_test.go`
- Create: `pkg/provisioner/upstream/testdata/sample_state.json`

- [ ] **Step 1: Write the failing test**

Create `pkg/provisioner/upstream/testdata/sample_state.json` (truncated tofu state shape):

```json
{
  "version": 4,
  "terraform_version": "1.7.0",
  "outputs": {
    "vpc_id": { "value": "vpc-abc123", "type": "string" },
    "subnet_ids": { "value": ["subnet-1", "subnet-2"], "type": ["list","string"] },
    "port": { "value": 5432, "type": "number" }
  },
  "resources": []
}
```

Create `pkg/provisioner/upstream/state_test.go`:

```go
package upstream

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractOutputs_WellFormed(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "sample_state.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := ExtractOutputs(body)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["vpc_id"] != "vpc-abc123" {
		t.Errorf("vpc_id=%v", got["vpc_id"])
	}
	subnets, ok := got["subnet_ids"].([]any)
	if !ok || len(subnets) != 2 || subnets[0] != "subnet-1" {
		t.Errorf("subnet_ids=%v", got["subnet_ids"])
	}
	if got["port"] != float64(5432) {
		t.Errorf("port=%v (%T)", got["port"], got["port"])
	}
}

func TestExtractOutputs_MalformedJSON(t *testing.T) {
	_, err := ExtractOutputs([]byte("not json"))
	if !errors.Is(err, ErrUpstreamStateUnreadable) {
		t.Fatalf("got %v", err)
	}
}

func TestExtractOutputs_NoOutputsField(t *testing.T) {
	got, err := ExtractOutputs([]byte(`{"version":4}`))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/provisioner/upstream/ -run TestExtractOutputs
```

Expected: FAIL — `ExtractOutputs / ErrUpstreamStateUnreadable / ErrUpstreamOutputMissing undefined`.

- [ ] **Step 3: Implement**

Append to `pkg/provisioner/upstream/upstream.go`:

```go
import (
	// ... existing imports ...
	"encoding/json"
)

// ErrUpstreamStateUnreadable wraps any malformed-JSON or missing-file
// error reading an upstream workspace's terraform.tfstate.
var ErrUpstreamStateUnreadable = errors.New("upstream state unreadable")

// ErrUpstreamOutputMissing fires when the upstream applied successfully but
// the expected output name isn't present in its state.
var ErrUpstreamOutputMissing = errors.New("upstream output missing from state")

// ExtractOutputs parses a tofu state JSON byte slice and returns the top-level
// `outputs` map, keyed by output name with the raw value (string / []any /
// number / bool / nested map).
func ExtractOutputs(state []byte) (map[string]any, error) {
	var parsed struct {
		Outputs map[string]struct {
			Value any `json:"value"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(state, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamStateUnreadable, err)
	}
	out := make(map[string]any, len(parsed.Outputs))
	for name, o := range parsed.Outputs {
		out[name] = o.Value
	}
	return out, nil
}

// FormatHCLValue turns a Go value (string / []any / number / bool) into an
// HCL literal suitable for `tofu plan -var name=value`. Tofu accepts JSON
// for complex types, so we round-trip through json.Marshal for lists/maps
// and use bare/quoted forms for scalars.
func FormatHCLValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		b, err := json.Marshal(x)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		// json numbers come in as float64; format integers cleanly.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x)), nil
		}
		return fmt.Sprintf("%g", x), nil
	case int, int64:
		return fmt.Sprintf("%d", x), nil
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/upstream/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/upstream/
git commit -m "provisioner/upstream: ExtractOutputs + FormatHCLValue + state errors"
```

---

### Task 11: `provisioner.Apply` — toposort + read upstream state + re-plan with real vars + blocked status

**Files:**
- Modify: `pkg/provisioner/apply.go`
- Modify: `pkg/provisioner/provisioner.go` (Status constants if missing)
- Modify: `pkg/provisioner/apply_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/apply_test.go`:

```go
func TestProvisionerApply_TopoOrderAndRealVarsRebound(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}

	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	// Hook: after network applies, drop a fake state file so app's apply
	// can ExtractOutputs from it.
	fake.OnApply = func(ws tofu.Workspace, planFile string) {
		if strings.HasSuffix(ws.Dir, "/web-network") {
			state := `{"version":4,"outputs":{"subnet_ids":{"value":["subnet-real"],"type":["list","string"]},"vpc_id":{"value":"vpc-real","type":"string"},"route_table_ids":{"value":[],"type":["list","string"]}}}`
			_ = os.WriteFile(filepath.Join(ws.Dir, "terraform.tfstate"), []byte(state), 0o600)
		}
	}

	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	workRoot := t.TempDir()
	p, _ := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: reg, Runner: fake})

	planRes, err := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := p.Apply(ctx, provisioner.ApplyInput{Project: project, Stack: "dev",
		DeploymentID: planRes.DeploymentID, Targets: planRes.Targets}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Network apply must precede compute apply.
	netIdx, appIdx := -1, -1
	for i, ac := range fake.ApplyCalls {
		switch {
		case strings.HasSuffix(ac.Workspace.Dir, "/web-network"):
			netIdx = i
		case strings.HasSuffix(ac.Workspace.Dir, "/web-app"):
			appIdx = i
		}
	}
	if netIdx == -1 || appIdx == -1 || netIdx >= appIdx {
		t.Fatalf("ordering: netIdx=%d appIdx=%d", netIdx, appIdx)
	}
	// The compute target's pre-apply Plan call (the re-plan) must have
	// received the REAL subnet_ids value, not a placeholder.
	var rebindCall *tofu.PlanCall
	for i, pc := range fake.PlanCalls {
		if strings.HasSuffix(pc.Workspace.Dir, "/web-app") {
			rebindCall = &fake.PlanCalls[i]
		}
	}
	if rebindCall == nil {
		t.Fatalf("no Plan call against web-app workspace")
	}
	v, _ := rebindCall.Workspace.Vars["upstream_web_network_subnet_ids"].(string)
	if !strings.Contains(v, "subnet-real") {
		t.Errorf("re-plan did not get real value: got %q", v)
	}
}

func TestProvisionerApply_DownstreamBlockedOnUpstreamFailure(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	fake.ApplyError = errors.New("simulated apply failure")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake,
		PartialFailure: provisioner.PartialFailureLeave})

	planRes, _ := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	appRes, _ := p.Apply(ctx, provisioner.ApplyInput{Project: project, Stack: "dev",
		DeploymentID: planRes.DeploymentID, Targets: planRes.Targets, PartialFailure: provisioner.PartialFailureLeave})

	// The web-network apply failed; web-app must be marked "blocked", not "failed".
	var appStatus string
	for _, t := range appRes.Targets {
		if t.Component == "web-app" {
			appStatus = t.Status
		}
	}
	if appStatus != "blocked" {
		t.Errorf("expected web-app blocked, got %q", appStatus)
	}
}
```

Add the `OnApply` hook to FakeRunner (per FakeRunner.go — its Apply method is one place where we want to fire a side-effect for testing). Open `internal/tofu/fake_runner.go` and inside `Apply`, immediately after `f.ApplyCalls = append(...)`:

```go
	if f.OnApply != nil {
		f.OnApply(ws, planFile)
	}
```

And add the field:

```go
	// OnApply, if non-nil, is invoked synchronously inside Apply for tests that
	// need to materialize state files between calls.
	OnApply func(ws Workspace, planFile string)
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestProvisionerApply_TopoOrder
```

Expected: FAIL — apply doesn't toposort, doesn't re-plan, doesn't read upstream state, doesn't propagate "blocked" status.

- [ ] **Step 3: Implement**

Open `pkg/provisioner/apply.go`. Replace the per-target loop with:

```go
func (rp *runtimeProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	// ... existing validation ...

	// Build TargetIdent list for toposort.
	idents := make([]upstream.TargetIdent, len(in.Targets))
	for i, t := range in.Targets {
		idents[i] = upstream.TargetIdent{Component: t.Component, Cloud: t.Cloud, Region: t.Region}
	}
	ordered, err := upstream.ToposortTargets(idents, in.Project.Components)
	if err != nil {
		return nil, fmt.Errorf("provisioner.Apply: toposort: %w", err)
	}
	identToTarget := indexTargetsByIdent(in.Targets)

	res := &ApplyResult{DeploymentID: in.DeploymentID}
	statusByIdent := map[upstream.TargetIdent]string{}

	for _, id := range ordered {
		target := identToTarget[id]
		comp := findComponent(in.Project, target.Component)

		// Check: any upstream of this target failed?
		blocked := false
		for _, r := range comp.Refs {
			upIdent := upstream.TargetIdent{Component: r.Component, Cloud: target.Cloud, Region: target.Region}
			if statusByIdent[upIdent] == "failed" || statusByIdent[upIdent] == "blocked" {
				blocked = true
				break
			}
		}
		if blocked {
			target.Status = "blocked"
			statusByIdent[id] = "blocked"
			res.Targets = append(res.Targets, target)
			continue
		}

		// Resolve real var values from upstream state.
		vars := map[string]any{}
		for _, r := range comp.Refs {
			upIdent := upstream.TargetIdent{Component: r.Component, Cloud: target.Cloud, Region: target.Region}
			upTarget := identToTarget[upIdent]
			stateBytes, rerr := os.ReadFile(filepath.Join(upTarget.WorkspaceDir, "terraform.tfstate"))
			if rerr != nil {
				target.Status = "failed"
				target.ErrorMessage = fmt.Errorf("%w: %v", upstream.ErrUpstreamStateUnreadable, rerr).Error()
				statusByIdent[id] = "failed"
				res.Targets = append(res.Targets, target)
				goto NextTarget
			}
			outs, oerr := upstream.ExtractOutputs(stateBytes)
			if oerr != nil {
				target.Status = "failed"
				target.ErrorMessage = oerr.Error()
				statusByIdent[id] = "failed"
				res.Targets = append(res.Targets, target)
				goto NextTarget
			}
			val, ok := outs[r.Output]
			if !ok {
				target.Status = "failed"
				target.ErrorMessage = fmt.Errorf("%w: %s.%s", upstream.ErrUpstreamOutputMissing, r.Component, r.Output).Error()
				statusByIdent[id] = "failed"
				res.Targets = append(res.Targets, target)
				goto NextTarget
			}
			hcl, herr := upstream.FormatHCLValue(val)
			if herr != nil {
				target.Status = "failed"
				target.ErrorMessage = herr.Error()
				statusByIdent[id] = "failed"
				res.Targets = append(res.Targets, target)
				goto NextTarget
			}
			vars[upstream.VarName(r.Component, r.Output)] = hcl
		}

		// Re-plan with real values, then apply.
		ws := tofu.Workspace{Dir: target.WorkspaceDir, Vars: vars}
		planFile := filepath.Join(target.WorkspaceDir, "plan.bin")
		if _, perr := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{OutFile: planFile}); perr != nil {
			target.Status = "failed"
			target.ErrorMessage = perr.Error()
			statusByIdent[id] = "failed"
			res.Targets = append(res.Targets, target)
			continue
		}
		if aerr := rp.cfg.Runner.Apply(ctx, ws, planFile, tofu.ApplyOpts{AutoApprove: in.AutoApprove}); aerr != nil {
			target.Status = "failed"
			target.ErrorMessage = aerr.Error()
			statusByIdent[id] = "failed"
			res.Targets = append(res.Targets, target)
			continue
		}
		target.Status = "applied"
		statusByIdent[id] = "applied"
		res.Targets = append(res.Targets, target)

	NextTarget:
	}
	return res, nil
}

func indexTargetsByIdent(targets []TargetPlan) map[upstream.TargetIdent]TargetPlan {
	out := map[upstream.TargetIdent]TargetPlan{}
	for _, t := range targets {
		out[upstream.TargetIdent{Component: t.Component, Cloud: t.Cloud, Region: t.Region}] = t
	}
	return out
}

func findComponent(p *ir.Project, name string) ir.Component {
	for _, c := range p.Components {
		if c.Name == name {
			return c
		}
	}
	return ir.Component{}
}
```

Confirm the `TargetPlan` struct in `pkg/provisioner/provisioner.go` (or wherever defined) has a `Status` string field. If not, add it:

```go
type TargetPlan struct {
	// ... existing fields ...
	Status       string
	ErrorMessage string
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/ -run TestProvisionerApply
```

Expected: PASS for both new tests; existing apply tests still pass (the `Status` field is additive).

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/apply.go pkg/provisioner/apply_test.go pkg/provisioner/provisioner.go internal/tofu/fake_runner.go
git commit -m "provisioner: Apply toposorts targets and re-plans dependents with real upstream vars"
```

---

### Task 12: `provisioner.Destroy` — reverse-toposort

**Files:**
- Modify: `pkg/provisioner/destroy.go`
- Modify: `pkg/provisioner/destroy_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/destroy_test.go`:

```go
func TestProvisionerDestroy_ReverseTopoOrder(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake})
	planRes, _ := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	_, _ = p.Destroy(ctx, provisioner.DestroyInput{Project: project, Stack: "dev",
		DeploymentID: planRes.DeploymentID, Targets: planRes.Targets})

	// Destroy: app first, then network.
	if len(fake.DestroyCalls) < 2 {
		t.Fatalf("got %d destroy calls", len(fake.DestroyCalls))
	}
	first := fake.DestroyCalls[0]
	last := fake.DestroyCalls[len(fake.DestroyCalls)-1]
	if !strings.HasSuffix(first.Dir, "/web-app") || !strings.HasSuffix(last.Dir, "/web-network") {
		t.Errorf("destroy order: first=%s last=%s", first.Dir, last.Dir)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestProvisionerDestroy_ReverseTopo
```

Expected: FAIL.

- [ ] **Step 3: Implement**

In `pkg/provisioner/destroy.go`, replace the iteration with:

```go
	idents := make([]upstream.TargetIdent, len(in.Targets))
	for i, t := range in.Targets {
		idents[i] = upstream.TargetIdent{Component: t.Component, Cloud: t.Cloud, Region: t.Region}
	}
	ordered, err := upstream.ToposortTargets(idents, in.Project.Components)
	if err != nil {
		return nil, fmt.Errorf("provisioner.Destroy: toposort: %w", err)
	}
	// Reverse for destroy.
	for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	}
	identToTarget := indexTargetsByIdent(in.Targets)
	for _, id := range ordered {
		target := identToTarget[id]
		// For destroy, we don't need real upstream values — but we DO need to
		// pass placeholders so tofu's variable declarations don't fail to resolve.
		comp := findComponent(in.Project, target.Component)
		placeholders, _ := upstream.PlanPlaceholders(comp.Refs, in.Project.Components, rp.cfg.Components)
		ws := tofu.Workspace{Dir: target.WorkspaceDir, Vars: stringsToAnys(placeholders)}
		if err := rp.cfg.Runner.Destroy(ctx, ws, tofu.DestroyOpts{AutoApprove: in.AutoApprove}); err != nil {
			// ... existing error handling ...
		}
	}
```

Add helper:

```go
func stringsToAnys(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/ -run TestProvisionerDestroy
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/destroy.go pkg/provisioner/destroy_test.go
git commit -m "provisioner: Destroy reverse-toposorts targets"
```

---

### Task 13: `provisioner.DetectDrift` — forward toposort with real-or-skip

**Files:**
- Modify: `pkg/provisioner/drift.go`
- Modify: `pkg/provisioner/drift_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/drift_test.go`:

```go
func TestProvisionerDrift_ForwardTopoAndUsesRealVars(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			{Name: "web-network", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
			{Name: "web-app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "web-network", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
		},
	}
	fake := tofu.NewFakeRunner()
	fake.PlanFileContents = []byte("FAKE")
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	workRoot := t.TempDir()
	p, _ := provisioner.New(provisioner.Config{WorkRoot: workRoot, Adapters: reg, Runner: fake})
	planRes, _ := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})

	// Materialize network's state so drift can read real outputs.
	for _, t := range planRes.Targets {
		if t.Component == "web-network" {
			_ = os.WriteFile(filepath.Join(t.WorkspaceDir, "terraform.tfstate"),
				[]byte(`{"version":4,"outputs":{"subnet_ids":{"value":["subnet-x"],"type":["list","string"]},"vpc_id":{"value":"vpc-x","type":"string"},"route_table_ids":{"value":[],"type":["list","string"]}}}`),
				0o600)
		}
	}

	_, err := p.DetectDrift(ctx, provisioner.DriftInput{Project: project, Stack: "dev",
		DeploymentID: planRes.DeploymentID, Targets: planRes.Targets})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	// web-app drift plan should have received real subnet_ids.
	var appCall *tofu.PlanCall
	for i, pc := range fake.PlanCalls {
		if strings.HasSuffix(pc.Workspace.Dir, "/web-app") && pc.Opts.RefreshOnly {
			appCall = &fake.PlanCalls[i]
		}
	}
	if appCall == nil {
		t.Fatalf("no refresh-only Plan call against web-app")
	}
	if v, _ := appCall.Workspace.Vars["upstream_web_network_subnet_ids"].(string); !strings.Contains(v, "subnet-x") {
		t.Errorf("drift did not use real var: %v", appCall.Workspace.Vars)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestProvisionerDrift_ForwardTopo
```

Expected: FAIL.

- [ ] **Step 3: Implement**

In `pkg/provisioner/drift.go`, replace the per-target loop with toposorted iteration that reads upstream state. Same shape as Apply (Task 11) but with `RefreshOnly: true`. If upstream state file is missing, mark the dependent's drift status as `"skipped"` rather than failing:

```go
	idents := make([]upstream.TargetIdent, len(in.Targets))
	for i, t := range in.Targets {
		idents[i] = upstream.TargetIdent{Component: t.Component, Cloud: t.Cloud, Region: t.Region}
	}
	ordered, err := upstream.ToposortTargets(idents, in.Project.Components)
	if err != nil {
		return nil, fmt.Errorf("provisioner.DetectDrift: toposort: %w", err)
	}
	identToTarget := indexTargetsByIdent(in.Targets)
	for _, id := range ordered {
		target := identToTarget[id]
		comp := findComponent(in.Project, target.Component)

		vars := map[string]any{}
		skipped := false
		for _, r := range comp.Refs {
			upIdent := upstream.TargetIdent{Component: r.Component, Cloud: target.Cloud, Region: target.Region}
			upTarget := identToTarget[upIdent]
			stateBytes, rerr := os.ReadFile(filepath.Join(upTarget.WorkspaceDir, "terraform.tfstate"))
			if rerr != nil {
				skipped = true
				break
			}
			outs, _ := upstream.ExtractOutputs(stateBytes)
			val, ok := outs[r.Output]
			if !ok {
				skipped = true
				break
			}
			hcl, _ := upstream.FormatHCLValue(val)
			vars[upstream.VarName(r.Component, r.Output)] = hcl
		}
		if skipped {
			res.Targets = append(res.Targets, DriftTargetResult{
				Component: target.Component, Cloud: target.Cloud, Region: target.Region,
				Status: "skipped", Message: "upstream state unavailable",
			})
			continue
		}
		ws := tofu.Workspace{Dir: target.WorkspaceDir, Vars: vars}
		planFile := filepath.Join(target.WorkspaceDir, "drift.bin")
		artifact, derr := rp.cfg.Runner.Plan(ctx, ws, tofu.PlanOpts{OutFile: planFile, RefreshOnly: true})
		// ... existing summarize / append code ...
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/ -run TestProvisionerDrift
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/drift.go pkg/provisioner/drift_test.go
git commit -m "provisioner: drift forward-toposorts and uses real vars or skips"
```

---

### Task 14: Integration test — real `tofu plan` against full-stack-project AWS targets

**Files:**
- Modify: `cmd/cli/integration_validate_test.go` (extend to call `tofu plan`)
- Create or extend: a sibling test that exercises the toposorted apply via FakeRunner

- [ ] **Step 1: Write the failing test**

Extend `cmd/cli/integration_validate_test.go` — after the existing `tofu validate` loop, add:

```go
func TestFullStack_TofuPlan_AWSOnly(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH; skipping integration test")
	}
	// AWS plan works with placeholder env via skip_credentials_validation.
	t.Setenv("AWS_ACCESS_KEY_ID", "fake")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-1")

	ctx := context.Background()
	project, err := loader.New().Load(ctx, "testdata/full-stack-project")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())

	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: tofu.NewExecRunner()})
	res, err := p.Plan(ctx, provisioner.PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-int"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	awsTargets := 0
	for _, tp := range res.Targets {
		if tp.Cloud != "aws" {
			continue
		}
		awsTargets++
		if !tp.HasChanges {
			t.Errorf("%s/%s: expected changes; HasChanges=false", tp.Component, tp.Cloud)
		}
	}
	if awsTargets == 0 {
		t.Fatal("no AWS targets in result")
	}
}
```

Also add a CLI-shape regression test in `cmd/cli/full_stack_test.go`:

```go
func TestCLI_PlanApplyFullStack_FakeRunner(t *testing.T) {
	// FakeRunner-based 12-target apply: confirms toposorted apply + re-plan
	// + blocked-status propagation work end-to-end through the CLI runner.
	// ... shape mirrors TestProvisionerApply_TopoOrder; assert all 12 targets
	// reach Status="applied" with OnApply hook materializing realistic state.
}
```

- [ ] **Step 2: Run integration test to verify it fails or skips**

```
go test -tags=integration ./cmd/cli/ -run TestFullStack_TofuPlan_AWSOnly -v
```

Expected: with `tofu` installed and AWS-only fixture filtering, this should now PASS once Tasks 1-13 are in. If `tofu` not present, the test skips. If the test FAILS at this stage, that's the bug surface to fix.

- [ ] **Step 3: Triage surfaced issues**

Integration testing against real tofu may surface issues that pure-Go unit tests miss. The most likely failure modes and where to fix:

- **"variable X not declared"** — Task 9 didn't add a `UpstreamVariable` entry for every ref. Check the `varDecls` loop in `planOne`. Fix in `pkg/provisioner/plan.go`.
- **"Reference to undeclared resource"** in tofu plan output — adapter `OutputBindings` references a `TofuName` that doesn't match what `Emit` produces. Compare `outputs.go` `TofuName` references against `Emit` outputs in the same adapter package. Fix in `internal/cloud/<cloud>/outputs.go`.
- **"Invalid value for variable: a list of string is required"** — `placeholderFor` produced a string placeholder for an output declared as `list<string>` (or vice versa). Check `pkg/components/registry.go` Kind values against `upstream.placeholderFor` mapping. Fix in `pkg/provisioner/upstream/upstream.go`.
- **"Unknown -var argument"** — `ExecRunner.Plan` `-var` flag formatting broke. Print `args` before `e.run` and re-run the test. Fix in `internal/tofu/exec_runner.go`.
- **`HasChanges=false` on a target that should have changes** — adapter emitted no resources or the placeholder values produced no diff. Inspect the generated `main.tf.json` in the test's `WorkRoot` (the test temp dir).

For each fix, add a focused unit test reproducing the issue before fixing, then re-run integration.

- [ ] **Step 4: Run all tests once more**

```
go test ./...
go test -tags=integration ./cmd/cli/...
```

Expected: all tests PASS (integration tests for AWS-only paths; Azure/GCP need real credentials so those continue to be gated on env-var presence).

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/integration_validate_test.go cmd/cli/full_stack_test.go
git commit -m "test: integration tofu plan + CLI full-stack apply over toposort"
```

---

## Self-Review Checklist

After all 14 tasks complete, verify:

- [ ] `git log --oneline` shows 13 commits (Tasks 1-6, 9 [bundled 7-9], 10-14).
- [ ] `go test ./...` passes.
- [ ] `go test -tags=integration ./cmd/cli/...` passes (AWS targets at minimum; Azure/GCP gated on credentials).
- [ ] `nimbusfab plan cmd/cli/testdata/full-stack-project --stack dev` produces a `PlanResult` with all 12 targets in dependency-first order, AWS targets have `HasChanges=true`, dependent targets show placeholder-typed values in their diff.
- [ ] The single-cloud demo from user-test session #1 (`nimbusfab plan cmd/cli/testdata/network-only-project`) still works — regression check.
- [ ] No reference to `data.terraform_remote_state` remains in `pkg/provisioner/` or `internal/cloud/`.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-17-cross-component-planning.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Good for this plan because it has 14 self-contained tasks with clear test gates.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints. Slower context-window-wise but I stay engaged with implementation details.

Which approach?
