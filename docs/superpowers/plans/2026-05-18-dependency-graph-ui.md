# Dependency Graph UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a server-rendered DAG view for nimbusfab projects and deployments, a `nimbusfab graph` CLI subcommand for offline preview, and a `upstream.PreflightPairing` fail-fast check that closes the cross-target ref gap left by Cross-Component Planning Phase 1.

**Architecture:** A pure-Go `pkg/graph` package owns layout + SVG rendering. The webapi consumes it for `/ui/projects/{id}/graph` and `/ui/deployments/{id}/graph`; the new CLI subcommand consumes it directly. Hybrid node granularity (one node per component, click to expand per-target panel). Top-down (default) and left-right layouts, persisted per-browser via cookie. Refs are already in `inventory.Component.IRJSON` — no migration needed.

**Tech Stack:** Go 1.22; server-rendered HTML (`html/template`); vanilla JS, no framework; OpenTofu state untouched by this change. Reuses `pkg/provisioner/upstream` (Toposort, Pair, TargetIdent) from Phase 1.

**Working spec:** `docs/superpowers/specs/2026-05-18-dependency-graph-ui-design.md`

---

## Pre-flight

```bash
export PATH=$HOME/.local/go/bin:$HOME/.local/bin:$PATH
go test ./...                                # full unit suite
go test -tags=integration ./cmd/cli/...      # CLI integration (needs tofu for some tests; graph CLI test doesn't)
go build ./...                               # compile check
```

Useful spec / code refs:
- `pkg/ir/types.go:63` — `ir.Component{Name, Type, Refs []ComponentRef, Targets []DeploymentTarget}`
- `pkg/ir/types.go:83` — `ir.ComponentRef{Component, Output, As}`
- `pkg/inventory/repo.go:136` — `inventory.Component{IRJSON []byte}` (refs already inside)
- `pkg/engine/inventory.go:71` — `persistPlan` serializes the IR; no change needed there
- `pkg/provisioner/upstream/upstream.go` — `Toposort`, `Pair`, `TargetIdent`, `ErrCrossTargetRefUnsupported`
- `internal/webapi/router.go:115-119` — `/ui/...` route registration pattern
- `internal/webapi/ui/pages.go` — `Renderer` type; existing `ProjectDetail`, `DeploymentDetail` handlers
- `internal/webapi/ui/templates/` — `layout.html`, `project_detail.html`, `deployment_detail.html`

---

### Task 1: `pkg/graph` package skeleton + types

**Files:**
- Create: `pkg/graph/types.go`
- Create: `pkg/graph/types_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/graph/types_test.go`:

```go
package graph

import "testing"

func TestDirectionDefault(t *testing.T) {
	in := Input{}
	if got := in.DirectionOrDefault(); got != "tb" {
		t.Errorf("DirectionOrDefault: got %q, want \"tb\"", got)
	}
	if got := (Input{Direction: "lr"}).DirectionOrDefault(); got != "lr" {
		t.Errorf("DirectionOrDefault: got %q, want \"lr\"", got)
	}
	if got := (Input{Direction: "bogus"}).DirectionOrDefault(); got != "tb" {
		t.Errorf("DirectionOrDefault on bogus value: got %q, want fallback \"tb\"", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/graph/...
```

Expected: FAIL — `package graph not found`.

- [ ] **Step 3: Implement the types**

Create `pkg/graph/types.go`:

```go
// Package graph builds positioned node + edge layouts for nimbusfab
// component dependency graphs and renders them as inline SVG. Used by
// the webapi (/ui/.../graph pages) and the `nimbusfab graph` CLI.
//
// The layout algorithm is a Sugiyama-lite: longest-path rank assignment +
// stable alphabetical column ordering + orthogonal edge routing. Adequate
// for typical nimbusfab project sizes (<50 components); crossing-reduction
// is deferred to a future revision.
package graph

// Input describes one graph the renderer should lay out.
type Input struct {
	// Components carries each component's name, type, and refs.
	Components []Component
	// Targets, if non-empty, drives per-component status badges.
	Targets []TargetSnapshot
	// PairingErrors, if non-empty, causes the matching refs to render
	// as red dashed edges.
	PairingErrors []PairingError
	// Direction is "tb" (top-down, default) or "lr" (left-right).
	Direction string
}

// DirectionOrDefault returns "tb" or "lr"; any other value (including
// empty) falls back to "tb".
func (i Input) DirectionOrDefault() string {
	if i.Direction == "lr" {
		return "lr"
	}
	return "tb"
}

// Component is one node in the graph.
type Component struct {
	Name string
	Type string
	Refs []Ref
}

// Ref is a typed reference from a dependent component to an upstream
// output. Same shape as ir.ComponentRef but kept local so pkg/graph has
// no import cycle with ir.
type Ref struct {
	Component string
	Output    string
	As        string
}

// TargetSnapshot is one (component, cloud, region, status) tuple used
// for status overlays. Status values match the inventory convention
// ("applied" / "failed" / "blocked" / "pending" / "destroyed" / etc.).
type TargetSnapshot struct {
	Component string
	Cloud     string
	Region    string
	Status    string
}

// PairingError marks one ref that has no matching upstream target in
// the dependent's (cloud, region). The renderer draws it as a red
// dashed edge; the provisioner uses the same struct for fail-fast.
type PairingError struct {
	Component string
	Ref       Ref
	Cloud     string
	Region    string
	Reason    string
}

// Output is what Layout produces and RenderSVG consumes.
type Output struct {
	Width      int
	Height     int
	Direction  string
	Nodes      []NodeBox
	Edges      []EdgePath
	HasErrors  bool
}

// NodeBox is one positioned component card.
type NodeBox struct {
	Name        string
	Type        string
	X, Y, W, H  int
	Badges      []TargetBadge
}

// TargetBadge is one cloud-status indicator under a node label.
type TargetBadge struct {
	Cloud  string
	Status string
}

// EdgePath is one ref edge as an SVG path data string.
type EdgePath struct {
	From  string // source component name
	To    string // dependent component name
	D     string // SVG <path d="…"> value
	Kind  string // "ok" or "unmatched"
}
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./pkg/graph/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/graph/types.go pkg/graph/types_test.go
git commit -m "graph: package skeleton + Input/Output/Component/Edge types"
```

---

### Task 2: `pkg/graph.Layout` top-down mode

**Files:**
- Create: `pkg/graph/layout.go`
- Create: `pkg/graph/layout_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/graph/layout_test.go`:

```go
package graph

import "testing"

func TestLayout_Empty(t *testing.T) {
	out, err := Layout(Input{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 0 || len(out.Edges) != 0 {
		t.Errorf("expected empty layout; got %d nodes, %d edges", len(out.Nodes), len(out.Edges))
	}
}

func TestLayout_SingleNode_TB(t *testing.T) {
	out, err := Layout(Input{Components: []Component{{Name: "net", Type: "network"}}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 1 || out.Nodes[0].Name != "net" {
		t.Fatalf("nodes: %+v", out.Nodes)
	}
	if out.Nodes[0].X < 0 || out.Nodes[0].Y < 0 {
		t.Errorf("negative coordinates: %+v", out.Nodes[0])
	}
}

func TestLayout_LinearChain_TB(t *testing.T) {
	// app -> net (app refs net); rendered top-down with net above app.
	in := Input{Components: []Component{
		{Name: "app", Type: "compute", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
		{Name: "net", Type: "network"},
	}}
	out, err := Layout(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 2 || len(out.Edges) != 1 {
		t.Fatalf("nodes=%d edges=%d", len(out.Nodes), len(out.Edges))
	}
	byName := map[string]NodeBox{}
	for _, n := range out.Nodes {
		byName[n.Name] = n
	}
	if byName["net"].Y >= byName["app"].Y {
		t.Errorf("net.Y=%d should be above app.Y=%d", byName["net"].Y, byName["app"].Y)
	}
}

func TestLayout_Diamond_TB(t *testing.T) {
	// d depends on b,c; both depend on a. a must be on top.
	in := Input{Components: []Component{
		{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
		{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "a"},
	}}
	out, _ := Layout(in)
	rank := map[string]int{}
	for _, n := range out.Nodes {
		rank[n.Name] = n.Y
	}
	if !(rank["a"] < rank["b"] && rank["a"] < rank["c"] && rank["b"] < rank["d"] && rank["c"] < rank["d"]) {
		t.Errorf("bad y-order: %+v", rank)
	}
	if len(out.Edges) != 4 {
		t.Errorf("want 4 edges, got %d", len(out.Edges))
	}
}

func TestLayout_PairingErrorEdgeKind(t *testing.T) {
	in := Input{
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
		PairingErrors: []PairingError{
			{Component: "app", Ref: Ref{Component: "net", Output: "vpc_id", As: "v"}, Cloud: "aws", Region: "us-east-2", Reason: "no match"},
		},
	}
	out, _ := Layout(in)
	if len(out.Edges) != 1 || out.Edges[0].Kind != "unmatched" {
		t.Errorf("edges: %+v", out.Edges)
	}
	if !out.HasErrors {
		t.Error("HasErrors should be true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/graph/ -run TestLayout
```

Expected: FAIL — `Layout undefined`.

- [ ] **Step 3: Implement `Layout` for top-down direction**

Create `pkg/graph/layout.go`:

```go
package graph

import (
	"fmt"
	"sort"
)

const (
	nodeW   = 140
	nodeH   = 56
	gapX    = 32
	gapY    = 64
	marginX = 24
	marginY = 24
)

// Layout produces positioned nodes and routed edges for the given input.
// It accepts only "tb" or "lr" directions; an empty or unknown direction
// is treated as "tb". Returns an error if the input's refs form a cycle
// (validator Phase 5 should have caught that upstream).
func Layout(in Input) (*Output, error) {
	dir := in.DirectionOrDefault()
	if len(in.Components) == 0 {
		return &Output{Direction: dir}, nil
	}

	// Rank assignment via longest-path layering.
	ranks, err := assignRanks(in.Components)
	if err != nil {
		return nil, err
	}

	// Bucket components by rank, then sort within each rank by name for
	// deterministic columns.
	byRank := map[int][]Component{}
	for _, c := range in.Components {
		r := ranks[c.Name]
		byRank[r] = append(byRank[r], c)
	}
	maxRank := 0
	for r := range byRank {
		if r > maxRank {
			maxRank = r
		}
	}
	for r := range byRank {
		sort.Slice(byRank[r], func(i, j int) bool { return byRank[r][i].Name < byRank[r][j].Name })
	}

	// Coordinates: index within rank → column position; rank → row position.
	// For "lr" we swap rows and columns at the end.
	pos := map[string]NodeBox{}
	maxCols := 0
	for r := 0; r <= maxRank; r++ {
		col := byRank[r]
		if len(col) > maxCols {
			maxCols = len(col)
		}
		for i, c := range col {
			b := NodeBox{Name: c.Name, Type: c.Type, W: nodeW, H: nodeH,
				X: marginX + i*(nodeW+gapX),
				Y: marginY + r*(nodeH+gapY),
				Badges: badgesFor(c.Name, in.Targets),
			}
			pos[c.Name] = b
		}
	}

	// Apply direction transform if "lr".
	if dir == "lr" {
		for name, b := range pos {
			b.X, b.Y = b.Y, b.X
			pos[name] = b
		}
	}

	// Build nodes slice (deterministic order: by name).
	names := make([]string, 0, len(pos))
	for n := range pos {
		names = append(names, n)
	}
	sort.Strings(names)
	nodes := make([]NodeBox, 0, len(names))
	for _, n := range names {
		nodes = append(nodes, pos[n])
	}

	// Build edges, applying pairing-error annotation.
	unmatched := unmatchedEdges(in.PairingErrors)
	edges := make([]EdgePath, 0)
	for _, c := range in.Components {
		dst, ok := pos[c.Name]
		if !ok {
			continue
		}
		for _, r := range c.Refs {
			src, ok := pos[r.Component]
			if !ok {
				continue
			}
			kind := "ok"
			if _, bad := unmatched[edgeKey{From: r.Component, To: c.Name}]; bad {
				kind = "unmatched"
			}
			edges = append(edges, EdgePath{
				From: r.Component, To: c.Name,
				D:    routePath(src, dst, dir),
				Kind: kind,
			})
		}
	}

	// Bounding box.
	width := marginX*2 + maxCols*(nodeW+gapX) - gapX
	height := marginY*2 + (maxRank+1)*(nodeH+gapY) - gapY
	if dir == "lr" {
		width, height = height, width
	}

	return &Output{
		Width:     width,
		Height:    height,
		Direction: dir,
		Nodes:     nodes,
		Edges:     edges,
		HasErrors: len(in.PairingErrors) > 0,
	}, nil
}

func assignRanks(components []Component) (map[string]int, error) {
	byName := map[string]Component{}
	for _, c := range components {
		if _, dup := byName[c.Name]; dup {
			return nil, fmt.Errorf("graph.Layout: duplicate component %q", c.Name)
		}
		byName[c.Name] = c
	}

	rank := map[string]int{}
	visiting := map[string]bool{}

	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		if _, done := rank[name]; done {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("graph.Layout: cycle: %v", append(path, name))
		}
		visiting[name] = true
		max := -1
		comp, ok := byName[name]
		if ok {
			for _, r := range comp.Refs {
				if _, exists := byName[r.Component]; !exists {
					continue
				}
				if err := visit(r.Component, append(path, name)); err != nil {
					return err
				}
				if rank[r.Component] > max {
					max = rank[r.Component]
				}
			}
		}
		visiting[name] = false
		rank[name] = max + 1
		return nil
	}

	// Walk in stable order so test fixtures are deterministic.
	order := make([]string, 0, len(byName))
	for n := range byName {
		order = append(order, n)
	}
	sort.Strings(order)
	for _, n := range order {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return rank, nil
}

func badgesFor(component string, targets []TargetSnapshot) []TargetBadge {
	var out []TargetBadge
	for _, t := range targets {
		if t.Component == component {
			out = append(out, TargetBadge{Cloud: t.Cloud, Status: t.Status})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cloud < out[j].Cloud })
	return out
}

type edgeKey struct{ From, To string }

func unmatchedEdges(errors []PairingError) map[edgeKey]struct{} {
	out := map[edgeKey]struct{}{}
	for _, e := range errors {
		out[edgeKey{From: e.Ref.Component, To: e.Component}] = struct{}{}
	}
	return out
}

// routePath returns an SVG "d" attribute for an orthogonal edge from
// src to dst, given the layout direction.
func routePath(src, dst NodeBox, dir string) string {
	if dir == "lr" {
		// Source right-edge → dest left-edge.
		x1, y1 := src.X+src.W, src.Y+src.H/2
		x2, y2 := dst.X, dst.Y+dst.H/2
		midX := (x1 + x2) / 2
		return fmt.Sprintf("M %d %d L %d %d L %d %d L %d %d", x1, y1, midX, y1, midX, y2, x2, y2)
	}
	// "tb": source bottom-center → dest top-center.
	x1, y1 := src.X+src.W/2, src.Y+src.H
	x2, y2 := dst.X+dst.W/2, dst.Y
	midY := (y1 + y2) / 2
	return fmt.Sprintf("M %d %d L %d %d L %d %d L %d %d", x1, y1, x1, midY, x2, midY, x2, y2)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/graph/...
```

Expected: PASS (4 layout tests + DirectionOrDefault test).

- [ ] **Step 5: Commit**

```bash
git add pkg/graph/layout.go pkg/graph/layout_test.go
git commit -m "graph: Layout (top-down) with longest-path rank assignment"
```

---

### Task 3: `pkg/graph.Layout` left-right mode

The Layout function already supports `Direction="lr"` (the `dir == "lr"` swap at the end of Layout, plus the LR branch of routePath). This task adds tests proving it works.

**Files:**
- Modify: `pkg/graph/layout_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/graph/layout_test.go`:

```go
func TestLayout_LinearChain_LR(t *testing.T) {
	// app -> net; LR should put net LEFT of app.
	in := Input{
		Direction: "lr",
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
	}
	out, err := Layout(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	byName := map[string]NodeBox{}
	for _, n := range out.Nodes {
		byName[n.Name] = n
	}
	if byName["net"].X >= byName["app"].X {
		t.Errorf("net.X=%d should be left of app.X=%d", byName["net"].X, byName["app"].X)
	}
	// All Y values should be in the same column when there's one node per rank.
	if byName["net"].Y != byName["app"].Y {
		t.Errorf("net.Y=%d app.Y=%d should be equal (one-per-rank LR layout)", byName["net"].Y, byName["app"].Y)
	}
	if out.Direction != "lr" {
		t.Errorf("Direction: got %q", out.Direction)
	}
}

func TestLayout_Diamond_LR(t *testing.T) {
	in := Input{
		Direction: "lr",
		Components: []Component{
			{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
			{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
			{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
			{Name: "a"},
		},
	}
	out, _ := Layout(in)
	x := map[string]int{}
	for _, n := range out.Nodes {
		x[n.Name] = n.X
	}
	if !(x["a"] < x["b"] && x["a"] < x["c"] && x["b"] < x["d"] && x["c"] < x["d"]) {
		t.Errorf("bad x-order: %+v", x)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

```
go test ./pkg/graph/ -run TestLayout_.*_LR
```

Expected: PASS (Layout already supports LR per Task 2's implementation).

If a test fails, the bug is in `routePath` or the swap at the end of `Layout`; fix the implementation there.

- [ ] **Step 3: Commit**

```bash
git add pkg/graph/layout_test.go
git commit -m "graph: regression tests for left-right layout"
```

---

### Task 4: `pkg/graph.RenderSVG`

**Files:**
- Create: `pkg/graph/render.go`
- Create: `pkg/graph/render_test.go`
- Create: `pkg/graph/testdata/diamond_tb.golden.svg` (will be created by golden-file flow)

- [ ] **Step 1: Write the failing test**

Create `pkg/graph/render_test.go`:

```go
package graph

import (
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "regenerate testdata/*.golden.svg files")

func TestRenderSVG_ValidXML(t *testing.T) {
	out, _ := Layout(Input{Components: []Component{{Name: "net"}, {Name: "app",
		Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}}}})
	svg := RenderSVG(out)
	var v any
	if err := xml.Unmarshal(svg, &v); err != nil {
		t.Fatalf("RenderSVG produced invalid XML: %v\nbody:\n%s", err, svg)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Errorf("RenderSVG output missing <svg> root")
	}
}

func TestRenderSVG_DiamondGolden(t *testing.T) {
	in := Input{Components: []Component{
		{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
		{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "a"},
	}}
	out, _ := Layout(in)
	got := RenderSVG(out)
	goldenPath := filepath.Join("testdata", "diamond_tb.golden.svg")
	if *updateGolden {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (rerun with -update-golden to create it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered SVG differs from golden.\nGot:\n%s\nWant:\n%s", got, want)
	}
}

func TestRenderSVG_UnmatchedEdgeIsDashed(t *testing.T) {
	out, _ := Layout(Input{
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
		PairingErrors: []PairingError{
			{Component: "app", Ref: Ref{Component: "net", Output: "vpc_id", As: "v"}, Cloud: "aws", Region: "us-east-2"},
		},
	})
	svg := RenderSVG(out)
	if !strings.Contains(string(svg), `stroke-dasharray`) {
		t.Errorf("unmatched edge should use stroke-dasharray, got:\n%s", svg)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/graph/ -run TestRenderSVG
```

Expected: FAIL — `RenderSVG undefined`.

- [ ] **Step 3: Implement RenderSVG**

Create `pkg/graph/render.go`:

```go
package graph

import (
	"bytes"
	"fmt"
	"html"
)

// RenderSVG returns a self-contained SVG document with embedded styles.
// Suitable for inline-in-HTML (the styles use scoped selectors) and for
// standalone files (the document is valid by itself).
func RenderSVG(out *Output) []byte {
	if out == nil {
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0"></svg>`)
	}
	w := out.Width
	h := out.Height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 60
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" class="nimbusfab-graph">`, w, h)
	b.WriteString(`<style>
.nimbusfab-graph .graph-node rect { fill: #1e4d2b; stroke: #5cb85c; stroke-width: 1.5; rx: 4; }
.nimbusfab-graph .graph-node text { fill: #e8e8e8; font-family: system-ui, sans-serif; font-size: 12px; }
.nimbusfab-graph .graph-node .type { fill: #9cc; font-size: 10px; }
.nimbusfab-graph .edge-ok { stroke: #888; stroke-width: 1.5; fill: none; }
.nimbusfab-graph .edge-unmatched { stroke: #cc4444; stroke-width: 1.5; stroke-dasharray: 5,3; fill: none; }
.nimbusfab-graph .badge { font-size: 9px; }
.nimbusfab-graph .badge-applied { fill: #5cb85c; }
.nimbusfab-graph .badge-failed { fill: #cc4444; }
.nimbusfab-graph .badge-blocked { fill: #d9a73d; }
.nimbusfab-graph .badge-pending { fill: #888; }
</style>`)

	// Edges first so nodes draw on top.
	for _, e := range out.Edges {
		class := "edge-ok"
		if e.Kind == "unmatched" {
			class = "edge-unmatched"
		}
		fmt.Fprintf(&b, `<path d="%s" class="%s"/>`, html.EscapeString(e.D), class)
	}

	for _, n := range out.Nodes {
		fmt.Fprintf(&b, `<g class="graph-node" data-component="%s">`, html.EscapeString(n.Name))
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d"/>`, n.X, n.Y, n.W, n.H)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle">%s</text>`,
			n.X+n.W/2, n.Y+18, html.EscapeString(n.Name))
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" class="type">%s</text>`,
			n.X+n.W/2, n.Y+32, html.EscapeString(n.Type))
		for i, badge := range n.Badges {
			fmt.Fprintf(&b, `<text x="%d" y="%d" class="badge badge-%s">%s</text>`,
				n.X+10+i*38, n.Y+n.H-10,
				html.EscapeString(badge.Status), html.EscapeString(badge.Cloud))
		}
		b.WriteString(`</g>`)
	}

	b.WriteString(`</svg>`)
	return b.Bytes()
}
```

- [ ] **Step 4: Generate the golden file, then verify tests pass**

```
go test ./pkg/graph/ -run TestRenderSVG -update-golden
go test ./pkg/graph/ -run TestRenderSVG
```

Expected: first run generates `pkg/graph/testdata/diamond_tb.golden.svg`; second run passes.

Quickly eyeball the golden file — it should be a single line of XML starting with `<svg xmlns="http://www.w3.org/2000/svg"`. If it's not, fix the renderer.

- [ ] **Step 5: Commit**

```bash
git add pkg/graph/render.go pkg/graph/render_test.go pkg/graph/testdata/
git commit -m "graph: RenderSVG with golden-file regression test"
```

---

### Task 5: `upstream.PreflightPairing`

**Files:**
- Modify: `pkg/provisioner/upstream/upstream.go`
- Create: `pkg/provisioner/upstream/preflight_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/provisioner/upstream/preflight_test.go`:

```go
package upstream

import (
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPreflightPairing_SameCloudRegionOK(t *testing.T) {
	comps := []ir.Component{
		{Name: "app", Type: "compute",
			Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		{Name: "net", Type: "network",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 0 {
		t.Errorf("expected no errors, got %+v", got)
	}
}

func TestPreflightPairing_CrossRegion(t *testing.T) {
	comps := []ir.Component{
		{Name: "app", Type: "compute",
			Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-west-2"}}},
		{Name: "net", Type: "network",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(got), got)
	}
	if got[0].Component != "app" || got[0].Cloud != "aws" || got[0].Region != "us-west-2" {
		t.Errorf("error fields: %+v", got[0])
	}
}

func TestPreflightPairing_CrossCloud(t *testing.T) {
	comps := []ir.Component{
		{Name: "app",
			Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "azure", Region: "eastus"}}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(got), got)
	}
}

func TestPreflightPairing_MultipleTargetsMixed(t *testing.T) {
	// app exists in aws/east + azure/eastus. net exists only in aws/east.
	// Expect ONE pairing error (for the azure target only).
	comps := []ir.Component{
		{Name: "app",
			Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{
				{Cloud: "aws", Region: "us-east-1"},
				{Cloud: "azure", Region: "eastus"},
			}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d", len(got))
	}
	if got[0].Cloud != "azure" {
		t.Errorf("expected error for azure target, got %+v", got[0])
	}
}

func TestPreflightPairing_NoRefsNoErrors(t *testing.T) {
	comps := []ir.Component{
		{Name: "isolated", Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	if got := PreflightPairing(comps); len(got) != 0 {
		t.Errorf("expected no errors, got %+v", got)
	}
}

func TestPreflightPairing_ErrorWrapsSentinel(t *testing.T) {
	comps := []ir.Component{
		{Name: "app",
			Refs: []ir.ComponentRef{{Component: "net", Output: "x", As: "x"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "azure", Region: "eastus"}}},
	}
	got := PreflightPairing(comps)
	if len(got) == 0 {
		t.Fatal("expected at least one error")
	}
	wrapped := got[0].AsError()
	if !errors.Is(wrapped, ErrCrossTargetRefUnsupported) {
		t.Errorf("AsError() does not wrap ErrCrossTargetRefUnsupported: %v", wrapped)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/provisioner/upstream/ -run TestPreflight
```

Expected: FAIL — `PreflightPairing undefined`, `PairingError.AsError undefined`.

- [ ] **Step 3: Implement**

Append to `pkg/provisioner/upstream/upstream.go`:

```go
// PairingError describes one ref→target pair where no upstream target
// exists in the dependent's (cloud, region). Distinct from the typed
// sentinel ErrCrossTargetRefUnsupported because callers (provisioner +
// graph renderer) want structured fields, not just an error string.
type PairingError struct {
	Component string
	Ref       ir.ComponentRef
	Cloud     string
	Region    string
	Reason    string
}

// AsError returns a wrapped sentinel suitable for errors.Is checks.
func (p PairingError) AsError() error {
	return fmt.Errorf("%w: %s in %s/%s needs %s.%s",
		ErrCrossTargetRefUnsupported, p.Component, p.Cloud, p.Region, p.Ref.Component, p.Ref.Output)
}

// PreflightPairing iterates every (component, ref, target) triple in the
// project and accumulates a PairingError for each (dependent target,
// upstream component) pair where no upstream target exists in the same
// (cloud, region). Used by provisioner.Plan as fail-fast pre-flight and
// by the graph renderer to draw unmatched edges as red dashed paths.
//
// Pure function: no I/O.
func PreflightPairing(components []ir.Component) []PairingError {
	// Build flat slice of all targets keyed by their owning component.
	type compTarget struct {
		Component string
		Target    ir.DeploymentTarget
	}
	all := make([]TargetIdent, 0)
	for _, c := range components {
		for _, t := range c.Targets {
			all = append(all, TargetIdent{Component: c.Name, Cloud: t.Cloud, Region: t.Region})
		}
	}

	var errors []PairingError
	for _, c := range components {
		if len(c.Refs) == 0 || len(c.Targets) == 0 {
			continue
		}
		for _, target := range c.Targets {
			depIdent := TargetIdent{Component: c.Name, Cloud: target.Cloud, Region: target.Region}
			for _, ref := range c.Refs {
				if _, err := Pair(depIdent, ref.Component, all); err != nil {
					errors = append(errors, PairingError{
						Component: c.Name,
						Ref:       ref,
						Cloud:     target.Cloud,
						Region:    target.Region,
						Reason:    "no upstream target in same (cloud, region)",
					})
				}
			}
		}
	}
	return errors
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/upstream/
```

Expected: PASS (all 6 new tests + prior tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/upstream/upstream.go pkg/provisioner/upstream/preflight_test.go
git commit -m "provisioner/upstream: PairingError + PreflightPairing for cross-target ref preflight"
```

---

### Task 6: Wire `PreflightPairing` into `provisioner.Plan` (fail-fast)

**Files:**
- Modify: `pkg/provisioner/plan.go`
- Modify: `pkg/provisioner/plan_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/provisioner/plan_test.go`:

```go
func TestProvisionerPlan_FailsFastOnCrossTargetRef(t *testing.T) {
	ctx := context.Background()
	project := &ir.Project{
		APIVersion: "infra.dev/v1alpha1", Name: "p",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{
			// app's only target is us-west-2; net's only target is us-east-1.
			{Name: "app", Type: "compute",
				Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "subnetId"}},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-west-2",
					Spec: map[string]any{"size": "small", "instanceCount": 1}}}},
			{Name: "net", Type: "network",
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1",
					Spec: map[string]any{"cidr": "10.0.0.0/16", "subnetCount": 1}}}},
		},
	}

	fake := tofu.NewFakeRunner()
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	p, _ := New(Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: fake})

	_, err := p.Plan(ctx, PlanInput{Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-x"})
	if err == nil {
		t.Fatal("expected Plan to fail on cross-target ref")
	}
	if !errors.Is(err, upstream.ErrCrossTargetRefUnsupported) {
		t.Errorf("error should wrap ErrCrossTargetRefUnsupported, got: %v", err)
	}
}
```

The test imports a few extras: `errors`, `github.com/klehmer/nimbusfab/pkg/provisioner/upstream`. If they're not already in the file's import block, add them.

- [ ] **Step 2: Run test to verify it fails**

```
go test ./pkg/provisioner/ -run TestProvisionerPlan_FailsFast
```

Expected: FAIL — Plan currently doesn't run the preflight; it returns success and lets the apply path discover the issue later.

- [ ] **Step 3: Add the preflight to Plan**

In `pkg/provisioner/plan.go`, immediately after the existing input-validation block (`if in.Project == nil { … }` etc.) and BEFORE the `upstream.Toposort` call, add:

```go
	if pairErrors := upstream.PreflightPairing(in.Project.Components); len(pairErrors) > 0 {
		// Fail fast: cross-target refs make any plan output structurally
		// unreliable. Report the first error wrapped with the typed sentinel
		// so callers can errors.Is it.
		return nil, pairErrors[0].AsError()
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/provisioner/...
go build ./...
```

Expected: new test PASS, all other Plan tests still PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/provisioner/plan.go pkg/provisioner/plan_test.go
git commit -m "provisioner: Plan fails fast on cross-target refs via PreflightPairing"
```

---

### Task 7: `inventory.Component.UnmarshalIR` helper

**Files:**
- Modify: `pkg/inventory/repo.go`
- Create: `pkg/inventory/component_ir_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/inventory/component_ir_test.go`:

```go
package inventory_test

import (
	"encoding/json"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestComponent_UnmarshalIR_RefsRoundTrip(t *testing.T) {
	original := ir.Component{
		Name: "app", Type: "compute",
		Refs: []ir.ComponentRef{
			{Component: "net", Output: "subnet_ids", As: "subnetId"},
			{Component: "db", Output: "endpoint", As: "dbHost"},
		},
	}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	comp := inventory.Component{IRJSON: body}
	got, err := comp.UnmarshalIR()
	if err != nil {
		t.Fatalf("UnmarshalIR: %v", err)
	}
	if got.Name != "app" || got.Type != "compute" {
		t.Errorf("basic fields: %+v", got)
	}
	if len(got.Refs) != 2 {
		t.Fatalf("expected 2 refs; got %d", len(got.Refs))
	}
	if got.Refs[0].Component != "net" || got.Refs[0].Output != "subnet_ids" {
		t.Errorf("ref[0]=%+v", got.Refs[0])
	}
}

func TestComponent_UnmarshalIR_EmptyJSON(t *testing.T) {
	got, err := (&inventory.Component{}).UnmarshalIR()
	if err != nil {
		t.Errorf("empty IRJSON should not error: %v", err)
	}
	if got.Name != "" || len(got.Refs) != 0 {
		t.Errorf("expected zero-value ir.Component, got %+v", got)
	}
}

func TestComponent_UnmarshalIR_Malformed(t *testing.T) {
	comp := inventory.Component{IRJSON: []byte("not json")}
	_, err := comp.UnmarshalIR()
	if err == nil {
		t.Error("expected error on malformed JSON")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/inventory/ -run TestComponent_UnmarshalIR
```

Expected: FAIL — `UnmarshalIR undefined`.

- [ ] **Step 3: Add the method**

Open `pkg/inventory/repo.go`. Add an import for `encoding/json` and `github.com/klehmer/nimbusfab/pkg/ir` if not present. After the `type Component struct {…}` definition (around line 146), add:

```go
// UnmarshalIR returns the persisted ir.Component (including Refs) by
// decoding the IRJSON column. Empty IRJSON returns a zero-value ir.Component
// without error.
func (c *Component) UnmarshalIR() (ir.Component, error) {
	var out ir.Component
	if len(c.IRJSON) == 0 {
		return out, nil
	}
	err := json.Unmarshal(c.IRJSON, &out)
	return out, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/inventory/
go build ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/inventory/repo.go pkg/inventory/component_ir_test.go
git commit -m "inventory: Component.UnmarshalIR ergonomic accessor for refs in IRJSON"
```

---

### Task 8: Graph page handlers + template + route registration

**Files:**
- Create: `internal/webapi/ui/templates/graph.html`
- Modify: `internal/webapi/ui/pages.go` (add `Graph` handler)
- Modify: `internal/webapi/router.go` (register routes)
- Modify: `internal/webapi/ui/templates/project_detail.html` (page tabs)
- Modify: `internal/webapi/ui/templates/deployment_detail.html` (page tabs)
- Modify: `internal/webapi/router_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/webapi/router_test.go` (the file already has helpers and seeds):

```go
func TestUI_DeploymentGraph_Renders(t *testing.T) {
	srv := newTestServerWithSeedData(t)  // or whatever the existing helper is called
	defer srv.Close()
	resp, body := get(t, srv, "/ui/deployments/d-1/graph")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "<svg") {
		t.Errorf("response missing <svg>; body:\n%s", body)
	}
	if !strings.Contains(body, "graph-toolbar") {
		t.Errorf("response missing graph-toolbar; body:\n%s", body)
	}
}

func TestUI_ProjectGraph_NoDeploymentPlaceholder(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()
	resp, body := get(t, srv, "/ui/projects/empty-project/graph")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "nimbusfab plan") {
		t.Errorf("expected 'nimbusfab plan' placeholder copy; body:\n%s", body)
	}
}
```

You may need to inspect `internal/webapi/router_test.go` to find the existing seed-data helper name and adjust IDs (`d-1`, `empty-project`) to match what the test infrastructure provides. If the helper doesn't already seed a deployment + project with refs, extend it minimally so this test has data to render.

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/webapi/ -run TestUI_DeploymentGraph
go test ./internal/webapi/ -run TestUI_ProjectGraph
```

Expected: FAIL — route returns 404.

- [ ] **Step 3: Add the template**

Create `internal/webapi/ui/templates/graph.html`:

```html
{{define "title"}}Graph — {{shortID .ID}}{{end}}
{{define "content"}}
<h1>Graph — <code>{{shortID .ID}}</code></h1>

<dl class="kv">
  <dt>{{.Kind}}</dt><dd><code>{{.ID}}</code></dd>
  {{if .StackName}}<dt>Stack</dt><dd><code>{{.StackName}}</code></dd>{{end}}
</dl>

<nav class="page-tabs">
  <a href="{{.OverviewURL}}">Overview</a> · <a href="{{.GraphURL}}"><strong>Graph</strong></a>
</nav>

{{if .Empty}}
<p class="muted">No deployments yet. Run <code>nimbusfab plan</code> to populate this project's graph.</p>
{{else}}
<div class="graph-toolbar">
  <span class="label">Layout:</span>
  <button class="seg {{if eq .Direction "tb"}}active{{end}}" data-dir="tb">▼ Top-down</button>
  <button class="seg {{if eq .Direction "lr"}}active{{end}}" data-dir="lr">▶ Left-right</button>
</div>

{{if .PairingWarnings}}
<div class="warning-panel">
  <strong>Cross-target ref warnings:</strong>
  <ul>
    {{range .PairingWarnings}}<li>{{.}}</li>{{end}}
  </ul>
</div>
{{end}}

<div class="graph-canvas" data-targets-json='{{.TargetsJSON}}'>
  {{.SVG | safeHTML}}
</div>

<aside id="node-detail" hidden>
  <h3 id="node-detail-title"></h3>
  <ul id="node-detail-targets"></ul>
  <button type="button" id="node-detail-close">Close</button>
</aside>

<script src="/assets/app.js"></script>
<script>nimbusfab.attachGraph();</script>
{{end}}
{{end}}
```

The template uses a `safeHTML` template func to inject the rendered SVG verbatim. Add it to the renderer's funcmap if not already present. Inspect `internal/webapi/ui/pages.go` for the existing template funcmap (look for `template.FuncMap`); add:

```go
"safeHTML": func(s string) template.HTML { return template.HTML(s) },
```

Add `"html/template"` to the file's imports if missing.

- [ ] **Step 4: Add the handler**

In `internal/webapi/ui/pages.go`, add a new method on `Renderer`:

```go
// Graph renders /ui/projects/{id}/graph and /ui/deployments/{id}/graph.
// The `kind` param ("project" or "deployment") selects the data shape.
func (r *Renderer) Graph(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		direction := readDirection(req)

		var (
			components []ir.Component
			targets    []graph.TargetSnapshot
			pageData   map[string]any
		)

		switch kind {
		case "deployment":
			dep, err := r.repo.Deployments().Get(req.Context(), r.orgID, id)
			if err != nil || dep == nil {
				r.renderError(w, http.StatusNotFound, "deployment not found: "+id)
				return
			}
			components, targets, err = r.loadDeploymentGraph(req.Context(), dep)
			if err != nil {
				r.renderError(w, http.StatusInternalServerError, err.Error())
				return
			}
			pageData = map[string]any{
				"ID": dep.ID, "Kind": "Deployment",
				"StackName":   dep.StackID,
				"OverviewURL": "/ui/deployments/" + dep.ID,
				"GraphURL":    "/ui/deployments/" + dep.ID + "/graph",
			}
		case "project":
			proj, err := r.repo.Projects().Get(req.Context(), r.orgID, id)
			if err != nil || proj == nil {
				r.renderError(w, http.StatusNotFound, "project not found: "+id)
				return
			}
			components, targets, err = r.loadProjectGraph(req.Context(), proj)
			if err != nil {
				r.renderError(w, http.StatusInternalServerError, err.Error())
				return
			}
			pageData = map[string]any{
				"ID": proj.ID, "Kind": "Project",
				"OverviewURL": "/ui/projects/" + proj.ID,
				"GraphURL":    "/ui/projects/" + proj.ID + "/graph",
			}
		}

		if len(components) == 0 {
			pageData["Empty"] = true
			pageData["Direction"] = direction
			r.render(w, "graph.html", r.withUser(req, pageData))
			return
		}

		// Convert ir.Component → graph.Component.
		gComps := make([]graph.Component, len(components))
		for i, c := range components {
			refs := make([]graph.Ref, len(c.Refs))
			for j, r := range c.Refs {
				refs[j] = graph.Ref{Component: r.Component, Output: r.Output, As: r.As}
			}
			gComps[i] = graph.Component{Name: c.Name, Type: c.Type, Refs: refs}
		}

		// PreflightPairing → warnings (NOT fatal in the UI; just annotate).
		var pairWarnings []string
		var pairErrors []graph.PairingError
		for _, pe := range upstream.PreflightPairing(components) {
			pairWarnings = append(pairWarnings, fmt.Sprintf("%s in %s/%s references %s.%s but no upstream target matches",
				pe.Component, pe.Cloud, pe.Region, pe.Ref.Component, pe.Ref.Output))
			pairErrors = append(pairErrors, graph.PairingError{
				Component: pe.Component,
				Ref:       graph.Ref{Component: pe.Ref.Component, Output: pe.Ref.Output, As: pe.Ref.As},
				Cloud:     pe.Cloud, Region: pe.Region, Reason: pe.Reason,
			})
		}

		out, err := graph.Layout(graph.Input{
			Components: gComps, Targets: targets,
			PairingErrors: pairErrors, Direction: direction,
		})
		if err != nil {
			r.renderError(w, http.StatusInternalServerError, "graph layout: "+err.Error())
			return
		}

		// Build the targets-json map: { componentName: [{cloud, region, status}, ...] }.
		tjson, _ := json.Marshal(buildTargetsJSON(targets))

		pageData["Direction"] = direction
		pageData["SVG"] = string(graph.RenderSVG(out))
		pageData["TargetsJSON"] = string(tjson)
		pageData["PairingWarnings"] = pairWarnings
		r.render(w, "graph.html", r.withUser(req, pageData))
	}
}

// readDirection picks the graph direction from query param > cookie > default.
func readDirection(req *http.Request) string {
	if q := req.URL.Query().Get("dir"); q == "tb" || q == "lr" {
		return q
	}
	if c, err := req.Cookie("nf_graph_dir"); err == nil && (c.Value == "tb" || c.Value == "lr") {
		return c.Value
	}
	return "tb"
}

func buildTargetsJSON(targets []graph.TargetSnapshot) map[string][]map[string]string {
	out := map[string][]map[string]string{}
	for _, t := range targets {
		out[t.Component] = append(out[t.Component], map[string]string{
			"cloud": t.Cloud, "region": t.Region, "status": t.Status,
		})
	}
	return out
}
```

Plus helpers `loadDeploymentGraph` and `loadProjectGraph` that resolve the actual components + targets:

```go
// loadDeploymentGraph returns the components (with refs) and per-target snapshots for a deployment.
func (r *Renderer) loadDeploymentGraph(ctx context.Context, dep *inventory.Deployment) ([]ir.Component, []graph.TargetSnapshot, error) {
	comps, err := r.repo.Components().ListByStack(ctx, r.orgID, dep.ProjectID, dep.StackID)
	if err != nil {
		return nil, nil, fmt.Errorf("list components: %w", err)
	}
	irComps := make([]ir.Component, 0, len(comps))
	for _, c := range comps {
		ic, err := c.UnmarshalIR()
		if err != nil {
			continue // tolerate a single broken row
		}
		irComps = append(irComps, ic)
	}
	dts, err := r.repo.DeploymentTargets().ListByDeployment(ctx, r.orgID, dep.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list targets: %w", err)
	}
	snaps := make([]graph.TargetSnapshot, len(dts))
	for i, dt := range dts {
		snaps[i] = graph.TargetSnapshot{
			Component: dt.ComponentName, Cloud: dt.Cloud, Region: dt.Region, Status: dt.Status,
		}
	}
	return irComps, snaps, nil
}

// loadProjectGraph returns the latest deployment's structure for a project,
// or no components if no deployment has run yet.
func (r *Renderer) loadProjectGraph(ctx context.Context, proj *inventory.Project) ([]ir.Component, []graph.TargetSnapshot, error) {
	// Find the latest deployment for the project's default stack (or any
	// stack); fall back to empty.
	deps, err := r.repo.Deployments().ListByProject(ctx, r.orgID, proj.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list deployments: %w", err)
	}
	if len(deps) == 0 {
		return nil, nil, nil
	}
	// deps are sorted descending by StartedAt in the existing impl.
	return r.loadDeploymentGraph(ctx, &deps[0])
}
```

Imports needed: `encoding/json`, `fmt`, `net/http`, `github.com/klehmer/nimbusfab/pkg/graph`, `github.com/klehmer/nimbusfab/pkg/inventory`, `github.com/klehmer/nimbusfab/pkg/ir`, `github.com/klehmer/nimbusfab/pkg/provisioner/upstream`.

If the `inventory.Repo` interface doesn't already have `Projects().Get` or `Deployments().ListByProject`, inspect what exists and adapt (the existing `ProjectDetail` and `DeploymentDetail` handlers will have the right pattern). Don't introduce new repo methods in this task.

- [ ] **Step 5: Register the routes**

In `internal/webapi/router.go`, alongside the existing UI routes (around line 117):

```go
	mux.Handle("GET /ui/projects/{id}/graph", uiAuth(renderer.Graph("project")))
	mux.Handle("GET /ui/deployments/{id}/graph", uiAuth(renderer.Graph("deployment")))
```

- [ ] **Step 6: Add page tabs to existing detail templates**

Open `internal/webapi/ui/templates/project_detail.html`. Near the top of `{{define "content"}}` (right after the `<h1>`), insert:

```html
<nav class="page-tabs">
  <a href="/ui/projects/{{.ID}}"><strong>Overview</strong></a> · <a href="/ui/projects/{{.ID}}/graph">Graph</a>
</nav>
```

Same in `internal/webapi/ui/templates/deployment_detail.html` (with `/ui/deployments/{{.Deployment.ID}}` URLs).

- [ ] **Step 7: Add CSS for tabs / toolbar / canvas / panel**

Append to `internal/webapi/ui/assets/style.css` (or whichever stylesheet the UI uses — confirm via `ls internal/webapi/ui/assets/`):

```css
.page-tabs { margin-bottom: 16px; font-size: 14px; }
.graph-toolbar { display: flex; gap: 8px; align-items: center; margin: 12px 0; }
.graph-toolbar .seg { padding: 4px 10px; border: 1px solid #ccc; background: #fff; cursor: pointer; }
.graph-toolbar .seg.active { background: #1e4d2b; color: #fff; border-color: #1e4d2b; }
.graph-canvas { border: 1px solid #ddd; padding: 12px; overflow: auto; }
.graph-canvas svg { display: block; }
.graph-node { cursor: pointer; }
.graph-node:hover rect { stroke-width: 2; }
#node-detail { position: fixed; right: 16px; top: 80px; width: 320px; background: #fff;
               border: 1px solid #ddd; padding: 14px; }
#node-detail h3 { margin-top: 0; }
.warning-panel { background: #fff8e1; border: 1px solid #d9a73d; padding: 10px 14px; margin: 12px 0; }
```

- [ ] **Step 8: Run tests to verify they pass**

```
go test ./internal/webapi/...
go build ./...
```

Expected: PASS. Build clean.

- [ ] **Step 9: Commit**

```bash
git add internal/webapi/ui/templates/graph.html \
        internal/webapi/ui/pages.go \
        internal/webapi/router.go \
        internal/webapi/ui/templates/project_detail.html \
        internal/webapi/ui/templates/deployment_detail.html \
        internal/webapi/ui/assets/style.css \
        internal/webapi/router_test.go
git commit -m "webapi: /ui/{projects,deployments}/{id}/graph page with SVG + page tabs"
```

---

### Task 9: Direction toggle (cookie + query param + JS)

**Files:**
- Modify: `internal/webapi/ui/assets/app.js`
- Modify: `internal/webapi/router_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/webapi/router_test.go`:

```go
func TestUI_DeploymentGraph_DirectionCookie(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()

	// Default (no cookie, no query) → top-down. Verify by looking for a node
	// at a Y position higher than the page's bottom margin.
	resp, bodyTB := getWithCookie(t, srv, "/ui/deployments/d-1/graph", "")
	if resp.StatusCode != 200 {
		t.Fatalf("default status=%d", resp.StatusCode)
	}

	// With ?dir=lr → left-right. The SVG should have a different bounding
	// box (width and height swap roles).
	resp, bodyLR := getWithCookie(t, srv, "/ui/deployments/d-1/graph?dir=lr", "")
	if resp.StatusCode != 200 {
		t.Fatalf("?dir=lr status=%d", resp.StatusCode)
	}
	if bodyTB == bodyLR {
		t.Errorf("TB and LR responses are identical; toggle has no effect")
	}

	// With cookie nf_graph_dir=lr and no query → LR.
	resp, bodyCookie := getWithCookie(t, srv, "/ui/deployments/d-1/graph", "nf_graph_dir=lr")
	if resp.StatusCode != 200 {
		t.Fatalf("cookie status=%d", resp.StatusCode)
	}
	// bodyCookie should match bodyLR's general shape (both rendered LR).
	if !strings.Contains(bodyCookie, `class="seg active" data-dir="lr"`) {
		t.Errorf("cookie path did not render LR (looking for active LR button); body:\n%s", bodyCookie)
	}
}
```

You'll need a `getWithCookie` helper that sets the Cookie header. If it doesn't exist in the test file, add:

```go
func getWithCookie(t *testing.T, srv *httptest.Server, path, cookie string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/webapi/ -run TestUI_DeploymentGraph_DirectionCookie
```

Expected: FAIL on the LR assertion if `readDirection` was implemented correctly in Task 8; otherwise on the first assertion. If your Task 8 already wires `readDirection`, this test mostly verifies behavior; if Task 8 is incomplete, complete it now.

- [ ] **Step 3: Add the JS toggle**

Append to `internal/webapi/ui/assets/app.js`:

```javascript
nimbusfab.attachGraph = function() {
  document.querySelectorAll('.graph-toolbar .seg').forEach(btn => {
    btn.addEventListener('click', () => {
      const dir = btn.dataset.dir;
      document.cookie = 'nf_graph_dir=' + dir + '; path=/; max-age=31536000; samesite=lax';
      const url = new URL(window.location.href);
      url.searchParams.set('dir', dir);
      window.location.href = url.toString();
    });
  });

  const canvas = document.querySelector('.graph-canvas');
  if (!canvas) return;
  const targets = JSON.parse(canvas.dataset.targetsJson || '{}');

  document.querySelectorAll('svg .graph-node').forEach(node => {
    node.addEventListener('click', () => {
      const name = node.dataset.component;
      renderNodeDetail(name, targets[name] || []);
    });
  });

  const closeBtn = document.getElementById('node-detail-close');
  if (closeBtn) {
    closeBtn.addEventListener('click', () => {
      document.getElementById('node-detail').hidden = true;
    });
  }
};

function renderNodeDetail(name, targetList) {
  const panel = document.getElementById('node-detail');
  if (!panel) return;
  document.getElementById('node-detail-title').textContent = name;
  const ul = document.getElementById('node-detail-targets');
  ul.innerHTML = '';
  if (targetList.length === 0) {
    const li = document.createElement('li');
    li.className = 'muted';
    li.textContent = 'No targets yet';
    ul.appendChild(li);
  } else {
    targetList.forEach(t => {
      const li = document.createElement('li');
      const cloud = document.createElement('span');
      cloud.className = 'badge';
      cloud.textContent = t.cloud;
      const region = document.createElement('code');
      region.textContent = ' ' + t.region + ' ';
      const status = document.createElement('span');
      status.className = 'badge status-' + (t.status || 'unknown');
      status.textContent = t.status || 'unknown';
      li.appendChild(cloud);
      li.appendChild(region);
      li.appendChild(status);
      ul.appendChild(li);
    });
  }
  panel.hidden = false;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/webapi/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webapi/ui/assets/app.js internal/webapi/router_test.go
git commit -m "webapi: graph direction toggle (cookie + query param) and node-detail panel JS"
```

---

### Task 10: Node click → side panel handler test

The JS for node click was added in Task 9. This task verifies via a server-side route test that `data-targets-json` is populated correctly, so the JS has data to render.

**Files:**
- Modify: `internal/webapi/router_test.go`

- [ ] **Step 1: Write the test**

Append to `internal/webapi/router_test.go`:

```go
func TestUI_DeploymentGraph_TargetsJSONPopulated(t *testing.T) {
	srv := newTestServerWithSeedData(t)
	defer srv.Close()
	resp, body := get(t, srv, "/ui/deployments/d-1/graph")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	// data-targets-json is a JSON object keyed by component name.
	if !strings.Contains(body, `data-targets-json='{`) {
		t.Errorf("missing data-targets-json attribute; body:\n%s", body)
	}
	// Specifically check that the seeded component name appears as a key.
	if !strings.Contains(body, `"web-network"`) {  // adjust based on seed data
		t.Errorf("targets json missing seeded component; body:\n%s", body)
	}
}
```

Adjust `"web-network"` to match whatever component name the test seeder uses. If the seeder doesn't currently include a component with refs, extend it minimally for this test.

- [ ] **Step 2: Run test**

```
go test ./internal/webapi/ -run TestUI_DeploymentGraph_TargetsJSON
```

Expected: PASS (the Task 8 handler already emits `data-targets-json`). If it fails, the handler isn't building `TargetsJSON` correctly — fix `buildTargetsJSON` in `pages.go`.

- [ ] **Step 3: Commit**

```bash
git add internal/webapi/router_test.go
git commit -m "test: graph page emits data-targets-json with seeded component"
```

---

### Task 11: CLI `nimbusfab graph` subcommand

**Files:**
- Create: `cmd/cli/graph.go`
- Create: `cmd/cli/graph_test.go`
- Create: `cmd/cli/testdata/cross-region-project/project.yaml`
- Create: `cmd/cli/testdata/cross-region-project/components/web-network.yaml`
- Create: `cmd/cli/testdata/cross-region-project/components/web-app.yaml`
- Modify: `cmd/cli/main.go` (register the subcommand)

- [ ] **Step 1: Create the cross-region fixture**

Create `cmd/cli/testdata/cross-region-project/project.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: cross-region-project
stacks:
  dev:
    stateBackend: { kind: local }
```

Create `cmd/cli/testdata/cross-region-project/components/web-network.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: web-network
type: network
spec:
  cidr: 10.0.0.0/16
  subnetCount: 1
targets:
  - cloud: aws
    region: us-east-1
```

Create `cmd/cli/testdata/cross-region-project/components/web-app.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: web-app
type: compute
spec:
  size: small
  instanceCount: 1
  subnetId: ${refs.subnetId}
refs:
  - component: web-network
    output: subnet_ids
    as: subnetId
targets:
  - cloud: aws
    region: us-west-2   # <-- intentionally mismatched with web-network's us-east-1
```

- [ ] **Step 2: Write the failing test**

Create `cmd/cli/graph_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGraphCommand_NetworkOnly(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Direction:   "tb",
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if code != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), `<svg`) {
		t.Errorf("stdout should start with <svg; got:\n%s", stdout.String()[:200])
	}
}

func TestGraphCommand_CrossRegionExits3(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/cross-region-project",
		Stack:       "dev",
		Direction:   "tb",
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if code != 3 {
		t.Errorf("expected exit 3 for cross-region; got %d (stderr: %s)", code, stderr.String())
	}
	// SVG should still be written so users can see the problem visually.
	if !strings.Contains(stdout.String(), `<svg`) {
		t.Errorf("stdout should still contain <svg on pairing failure; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "cross-target") && !strings.Contains(stderr.String(), "no upstream target") {
		t.Errorf("stderr should mention cross-target failure; got: %s", stderr.String())
	}
}

func TestGraphCommand_OutFlagWritesFile(t *testing.T) {
	outPath := t.TempDir() + "/graph.svg"
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Direction:   "lr",
		OutPath:     outPath,
		Stderr:      stderr,
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out file: %v", err)
	}
	if !strings.HasPrefix(string(data), `<svg`) {
		t.Errorf("out file should be SVG; got: %s", string(data)[:200])
	}
}
```

Add `import "os"` if not present (the third test uses it).

- [ ] **Step 3: Run tests to verify they fail**

```
go test ./cmd/cli/ -run TestGraphCommand
```

Expected: FAIL — `runGraph undefined`.

- [ ] **Step 4: Implement the subcommand**

Create `cmd/cli/graph.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/graph"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

type graphArgs struct {
	ProjectPath string
	Stack       string
	Direction   string
	OutPath     string
	Stdout      io.Writer
	Stderr      io.Writer
}

func newGraphCommand() *cobra.Command {
	var stack, direction, outPath string
	cmd := &cobra.Command{
		Use:   "graph [path]",
		Short: "Render a SVG dependency graph for a project; no inventory / cloud creds required",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."
			if len(args) == 1 {
				projectPath = args[0]
			}
			code := runGraph(graphArgs{
				ProjectPath: projectPath,
				Stack:       stack,
				Direction:   direction,
				OutPath:     outPath,
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
			})
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&stack, "stack", "dev", "stack name")
	cmd.Flags().StringVar(&direction, "dir", "tb", "layout direction: tb (top-down) or lr (left-right)")
	cmd.Flags().StringVar(&outPath, "out", "", "write SVG to file; default stdout")
	return cmd
}

// runGraph is the testable entry point. Returns exit code per spec:
// 0 success / 1 IO / 2 validator failure / 3 pairing failure.
func runGraph(args graphArgs) int {
	ctx := context.Background()
	stdout := args.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := args.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	project, err := loader.New().Load(ctx, args.ProjectPath)
	if err != nil {
		fmt.Fprintf(stderr, "load: %v\n", err)
		return 1
	}

	// Run the validator so structural ref errors surface at this entry point too.
	v := validator.New(components.DefaultRegistry())
	report, err := v.Validate(ctx, project)
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 2
	}
	if len(report.Issues) > 0 {
		for _, i := range report.Issues {
			fmt.Fprintf(stderr, "validator: %s: %s\n", i.Code, i.Message)
		}
		return 2
	}

	pairErrors := upstream.PreflightPairing(project.Components)
	for _, pe := range pairErrors {
		fmt.Fprintf(stderr, "pairing: %s in %s/%s references %s.%s but no upstream target matches\n",
			pe.Component, pe.Cloud, pe.Region, pe.Ref.Component, pe.Ref.Output)
	}

	gComps := make([]graph.Component, len(project.Components))
	for i, c := range project.Components {
		refs := make([]graph.Ref, len(c.Refs))
		for j, r := range c.Refs {
			refs[j] = graph.Ref{Component: r.Component, Output: r.Output, As: r.As}
		}
		gComps[i] = graph.Component{Name: c.Name, Type: c.Type, Refs: refs}
	}

	gPairs := make([]graph.PairingError, len(pairErrors))
	for i, pe := range pairErrors {
		gPairs[i] = graph.PairingError{
			Component: pe.Component,
			Ref:       graph.Ref{Component: pe.Ref.Component, Output: pe.Ref.Output, As: pe.Ref.As},
			Cloud:     pe.Cloud, Region: pe.Region, Reason: pe.Reason,
		}
	}

	out, err := graph.Layout(graph.Input{
		Components: gComps, PairingErrors: gPairs, Direction: args.Direction,
	})
	if err != nil {
		fmt.Fprintf(stderr, "layout: %v\n", err)
		return 1
	}
	svg := graph.RenderSVG(out)

	if args.OutPath != "" {
		if err := os.WriteFile(args.OutPath, svg, 0o644); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", args.OutPath, err)
			return 1
		}
	} else {
		_, _ = stdout.Write(svg)
		_, _ = stdout.Write([]byte("\n"))
	}

	if len(pairErrors) > 0 {
		return 3
	}
	return 0
}

// Suppress unused import warning during incremental development.
var _ = ir.Component{}
```

The final `var _ = ir.Component{}` line keeps the `ir` import in place in case it's only used transitively; remove it once Go reports it as unused.

- [ ] **Step 5: Register the subcommand**

Open `cmd/cli/main.go`. Find where the existing subcommands are added (`rootCmd.AddCommand(newPlanCommand(), ...)`). Add `newGraphCommand()` to the list.

- [ ] **Step 6: Run tests to verify they pass**

```
go test ./cmd/cli/ -run TestGraphCommand
go build ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/cli/graph.go cmd/cli/graph_test.go cmd/cli/main.go cmd/cli/testdata/cross-region-project
git commit -m "cli: nimbusfab graph subcommand for offline SVG dependency view"
```

---

## Self-Review Checklist

After all 11 tasks complete, verify:

- [ ] `git log --oneline main..HEAD` shows 11 commits.
- [ ] `go test ./...` passes.
- [ ] `nimbusfab graph cmd/cli/testdata/network-only-project --out=/tmp/a.svg` exits 0 and writes a non-empty SVG.
- [ ] `nimbusfab graph cmd/cli/testdata/cross-region-project --out=/tmp/b.svg` exits 3 and `/tmp/b.svg` shows red dashed edges (open in a browser to verify).
- [ ] `nimbusfab plan cmd/cli/testdata/cross-region-project --stack dev` fails with `ErrCrossTargetRefUnsupported`.
- [ ] Visit `/ui/projects/<some-project-id>` — the Graph link appears in page tabs.
- [ ] Visit `/ui/deployments/<some-deployment-id>/graph` — see SVG, click toggle, refresh; the LR layout sticks across reloads (cookie).
- [ ] Click a node — side panel opens with per-target rows.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-dependency-graph-ui.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review the diff between tasks, fast iteration. Good fit: 11 self-contained tasks with clear test gates.

**2. Inline Execution** — Tasks run in this session via `executing-plans`, batched checkpoints.

Which approach?
