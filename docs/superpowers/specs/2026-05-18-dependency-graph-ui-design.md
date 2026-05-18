# Dependency Graph UI — v1.1 Design

**Status:** Subsystem spec. Adds a per-project / per-deployment dependency graph (DAG) view to the nimbusfab web UI, a `nimbusfab graph` CLI subcommand for offline preview, and bundles a small cross-target ref pre-flight check that the cross-component planning Phase 1 left as a gap.

**Date:** 2026-05-18

**Depends on:**
- `docs/superpowers/specs/2026-05-17-cross-component-planning-design.md` (Phase 1) — toposort, `upstream.Pair`, `pkg/provisioner/upstream` package.
- `docs/superpowers/specs/2026-05-16-webapp-design.md` — server-rendered HTML pages, vanilla-JS interactivity, no SPA framework.
- `docs/superpowers/specs/2026-05-16-inventory-design.md` — `components` table schema; migration runner.

**Depended on by:**
- Future "deployment workflow" feature (v1.2+) — timed transitions between component layers (e.g., "wait 30 s between database migration and app rollout") needs per-edge metadata that this spec's `pkg/graph` will display.

---

## Context

Cross-Component Planning Phase 1 (merged 2026-05-17, `b605862`) gave the engine the machinery to plan and apply multi-component projects with refs between them. What it didn't give users is a way to **see** that structure. The four motivating use cases:

1. **See structure at a glance.** Open the project page, confirm at a glance that "network is upstream of compute and the database."
2. **Debug blocked targets.** When `nimbusfab apply` partial-fails and downstream targets get `RunStatusBlocked`, see which upstream broke and what it took down with it.
3. **Verify refs before applying.** Sanity-check that the YAML's `refs:` field wired the topology you intended — catches missing or misnamed refs visually, before clicking Deploy.
4. **Plan change impact.** Before editing a component, see which downstream components would also re-plan.

Use case (3) also surfaces the small structural gap left by Phase 1: `upstream.Pair` was implemented but never called from production code, so cross-target pairing failures (dependent in cloud X with no upstream in cloud X) surface at apply time as a confusing `ErrUpstreamStateUnreadable` instead of at plan time as the correct `ErrCrossTargetRefUnsupported`. This spec bundles that pre-flight fix in.

## Design principles

1. **One renderer, three surfaces.** A pure-Go `pkg/graph` package owns layout. The web UI and the CLI both consume its output. No duplicate layout code.
2. **Server-rendered SVG.** Matches existing UI conventions (server-rendered HTML, ~100 LOC vanilla JS, no client-side framework). Zero new client-side bundle weight.
3. **Inventory-driven for the UI, YAML-driven for the CLI.** The server reads what's already in the inventory (components + refs). The CLI loads YAML so it works for projects that have never been deployed — covering the "verify before applying" use case without a server-side YAML upload endpoint.
4. **Hybrid node granularity.** One node per component (compact for multi-cloud projects); click expands a side panel with per-target detail. The graph doesn't try to be a full Sugiyama renderer — for nimbusfab project sizes (<50 components in practice), a simple rank-by-toposort + alphabetical column layout is enough.
5. **Same Pair logic everywhere.** `upstream.Pair` already exists; this spec adds `upstream.PreflightPairing` that calls it across all (component, ref, target) triples. The provisioner calls it for fail-fast plan-time errors; the graph renderer calls it to draw red dashed edges for unmatched refs.

## Architecture

### New package: `pkg/graph`

Pure Go. Std lib + existing nimbusfab packages only.

```go
type Input struct {
    Components    []Component
    Targets       []TargetSnapshot  // optional; drives status overlays
    PairingErrors []PairingError    // optional; drives red dashed edges
    Direction     string            // "tb" (default) or "lr"
}

type Component struct {
    Name, Type string
    Refs       []Ref
}

type Ref struct {
    Component, Output, As string
}

type TargetSnapshot struct {
    Component, Cloud, Region, Status string
}

type PairingError struct {
    Component   string  // dependent component name
    Ref         Ref
    Cloud       string  // dependent's cloud
    Region      string  // dependent's region
    Reason      string
}
```

`pkg/graph.Layout(in Input) (*Output, error)` produces:

```go
type Output struct {
    Width, Height int
    Nodes []NodeBox    // x, y, w, h, name, type, statusBadges []TargetBadge
    Edges []EdgePath   // d string for <path>, kind: "ok" or "unmatched"
}
```

`pkg/graph.RenderSVG(out *Output) []byte` produces a self-contained SVG document with embedded styles, suitable for both inline-template use and CLI standalone-file use.

### Layout algorithm

Sugiyama-lite:

1. **Rank assignment (longest-path layering)** — call `upstream.Toposort(components)` to get a dependency-first order. Walk that order once: for each component, set `rank[name] = 1 + max(rank[ref] for ref in component.Refs)` where the `max` over an empty set is `-1` (so a component with no refs lands at rank 0). Single pass; O(V + E).
2. **Column assignment** within a rank — stable alphabetical sort of component names.
3. **Coordinate calculation** — node width 140 px, height 56 px, horizontal gap 32 px, vertical gap 64 px. Origin top-left.
4. **Direction:**
   - `"tb"` (top-down): ranks → y rows; columns → x positions. Edges flow downward.
   - `"lr"` (left-right): ranks → x columns; columns → y positions. Edges flow rightward.
5. **Edge routing** — orthogonal. From source-bottom (or right, in LR) go perpendicular 16 px, then parallel to the dependent's column (or row), then perpendicular to the dependent. Single bend if columns/rows match; double bend otherwise.

For projects with >25 nodes, this layout produces some crossings — explicitly accepted in v1.1. Sugiyama crossing-reduction is deferred to v1.2 if it ever matters in practice.

### `upstream.PreflightPairing`

```go
func PreflightPairing(components []ir.Component, targets []ir.DeploymentTarget) []PairingError
```

For each (component, ref, dependent-target) triple, calls existing `upstream.Pair` and accumulates non-nil errors as `PairingError` rows. Pure function — no I/O.

Used in two call sites:

1. **`provisioner.Plan`** — at the top, before toposort. If `len(errors) > 0`, returns `fmt.Errorf("%w: %d unmatched refs (first: %s in %s/%s)", upstream.ErrCrossTargetRefUnsupported, ...)`. Fail-fast; no partial plan. Cross-target refs are a structural issue, and a partial plan against a broken structure is more confusing than helpful.
2. **`pkg/graph.Layout`** — input field; renderer draws red dashed edges for any ref appearing in `PairingErrors`.

### Refs persistence — already done

A code read during spec self-review found that `engine.persistPlan` (`pkg/engine/inventory.go:71`) already serializes the full `ir.Component` (including `Refs`) into the existing `ir_json` column on the `components` table. No migration is needed, and no new field on `inventory.Component` is needed.

The UI handler recovers refs by `json.Unmarshal(comp.IRJSON, &irComp)`. To keep that ergonomic, a single helper lands on `inventory.Component`:

```go
// UnmarshalIR returns the persisted ir.Component (including Refs). Use this
// when reading components for cross-component graph rendering / analysis.
func (c *Component) UnmarshalIR() (ir.Component, error) {
    var out ir.Component
    if len(c.IRJSON) == 0 {
        return out, nil
    }
    err := json.Unmarshal(c.IRJSON, &out)
    return out, err
}
```

That's the only inventory-layer change. No SQL migration, no new column, no repo signature changes. The plan task list (Section "Implementation phasing") drops the migration + repo serialization tasks accordingly.

### Webapi integration

**New routes:**

- `GET /ui/projects/{id}/graph` — latest deployment's components + refs from inventory. Status badges roll up the latest deployment's targets. If the project has no deployments, renders the placeholder page (see Empty States).
- `GET /ui/deployments/{id}/graph` — same shape, scoped to the specific deployment.

**Route handlers** in `internal/webapi/ui/pages.go`:
1. Look up project / deployment from `inventory.Repo`.
2. Read direction preference: query param `?dir=` wins; else cookie `nf_graph_dir`; else default `"tb"`.
3. Build `graph.Input` from inventory data.
4. Call `graph.Layout(input)`.
5. Render `templates/graph.html` with the `Output` struct.

**Template (`graph.html`):**

```html
{{define "title"}}Graph — {{shortID .ID}}{{end}}
{{define "content"}}
<h1>Graph — <code>{{shortID .ID}}</code></h1>
<dl class="kv">
  <dt>Project</dt><dd>...</dd>
  <dt>Stack</dt><dd>...</dd>
</dl>

<nav class="page-tabs">
  <a href="/ui/{kind}/{id}">Overview</a> · <a href="/ui/{kind}/{id}/graph"><strong>Graph</strong></a>
</nav>

<div class="graph-toolbar">
  <span class="label">Layout:</span>
  <button class="seg {{if eq .Direction "tb"}}active{{end}}" data-dir="tb">▼ Top-down</button>
  <button class="seg {{if eq .Direction "lr"}}active{{end}}" data-dir="lr">▶ Left-right</button>
</div>

<div class="graph-canvas" data-targets-json='{{.TargetsJSON}}'>
  {{.SVG}}
</div>

<aside id="node-detail" hidden>
  <h3 id="node-detail-title"></h3>
  <ul id="node-detail-targets"></ul>
</aside>

<script src="/assets/app.js"></script>
<script>nimbusfab.attachGraph();</script>
{{end}}
```

**Vanilla JS (`app.js`, ~50 LOC added):**

```js
nimbusfab.attachGraph = function() {
  // Direction toggle: write cookie, navigate to ?dir=...
  document.querySelectorAll('.graph-toolbar .seg').forEach(btn => {
    btn.addEventListener('click', () => {
      const dir = btn.dataset.dir;
      document.cookie = `nf_graph_dir=${dir}; path=/; max-age=31536000; samesite=lax`;
      const url = new URL(window.location.href);
      url.searchParams.set('dir', dir);
      window.location.href = url.toString();
    });
  });

  // Node click: open side panel with per-target detail
  const targets = JSON.parse(
    document.querySelector('.graph-canvas').dataset.targetsJson || '{}');
  document.querySelectorAll('svg .graph-node').forEach(node => {
    node.addEventListener('click', () => {
      const name = node.dataset.component;
      renderNodeDetail(name, targets[name] || []);
    });
  });
};
```

**CSS additions** (~30 lines in `assets/style.css`):
```css
.page-tabs { margin-bottom: 16px; font-size: 14px; }
.graph-toolbar { display: flex; gap: 8px; align-items: center; margin: 12px 0; }
.graph-toolbar .seg { padding: 4px 10px; border: 1px solid #ccc; background: #fff; cursor: pointer; }
.graph-toolbar .seg.active { background: #1e4d2b; color: #fff; border-color: #1e4d2b; }
.graph-canvas { border: 1px solid #ddd; padding: 12px; overflow: auto; }
.graph-canvas svg { display: block; }
.graph-node { cursor: pointer; }
.graph-node:hover rect { stroke-width: 2; }
#node-detail { position: fixed; right: 16px; top: 80px; width: 320px;
               background: #fff; border: 1px solid #ddd; padding: 14px; }
```

**Navigation links** added to existing `project_detail.html` and `deployment_detail.html` — a single `<nav class="page-tabs">` row near the top of each that lets the user switch between Overview and Graph.

### CLI integration

```
nimbusfab graph <project-path> [--stack=<s>] [--dir=tb|lr] [--out=<file>]
```

Behavior:
1. Loads project YAML via `loader.New().Load(ctx, projectPath)`.
2. Runs validator (so Phase 5 ref errors surface at this entry point too).
3. Runs `upstream.PreflightPairing` against the project's components + targets.
4. Builds `graph.Input` with no `Targets` (no deployment yet); populates `PairingErrors` from step 3.
5. Calls `graph.Layout` and `graph.RenderSVG`.
6. Writes SVG bytes to stdout or `--out` file.

**Exit codes:**
- `0` — success.
- `2` — validator failure (ref structural error, schema error).
- `3` — pairing failure (cross-target ref).
- `1` — IO / loader error.

**Flags:**
- `--stack=<name>` — required if the project has multiple stacks and no default.
- `--dir=tb|lr` — defaults to `tb`.
- `--out=<path>` — defaults to stdout.

No server, no inventory, no cloud credentials required. Pure offline tool.

## Data flow

```
        ┌─────────────────┐
        │  project YAML   │
        └────────┬────────┘
                 │
                 ▼
        ┌─────────────────┐
        │ loader → IR     │
        └────────┬────────┘
                 │
       ┌─────────┼─────────┐
       │                   │
       ▼                   ▼
  CLI graph cmd      validator + provisioner.Plan
       │              already persists ir.Component (incl. Refs) → inventory.components.ir_json
       │                   │
       │                   ▼
       │         ┌──────────────────────┐
       │         │ webapi: GET /ui/.../graph
       │         │   reads components + refs from inventory
       │         │   reads deployment_targets for status
       │         └─────────┬────────────┘
       │                   │
       └──────────┬────────┘
                  │
                  ▼
        ┌────────────────────────────┐
        │ upstream.PreflightPairing  │ ── PairingErrors
        └────────────┬───────────────┘
                     │
                     ▼
        ┌────────────────────────────┐
        │ pkg/graph.Layout           │ ── Output (Nodes, Edges)
        └────────────┬───────────────┘
                     │
                     ▼
        ┌────────────────────────────┐
        │ pkg/graph.RenderSVG        │ ── []byte
        └────────────┬───────────────┘
                     │
            ┌────────┴────────┐
            │                 │
            ▼                 ▼
       CLI stdout      template inline SVG
```

## Empty / edge states

| Condition | Behavior |
|-----------|----------|
| Project has no deployments | `/ui/projects/{id}/graph` renders placeholder: *"Run `nimbusfab plan` to see this project's graph."* |
| Project has refs but no targets satisfy them | Graph renders with red dashed edges for unmatched refs + warning panel listing `PairingError` codes. |
| Component has no refs | Stands alone at rank 0; no edges. |
| Single-component project | One node, no edges. Renders fine. |
| CLI: project with cross-target refs | Exits 3, writes `PairingError` summary to stderr. Still writes SVG (with red dashed edges) to stdout/`--out` so the user can visualize the problem. |

## Error handling

| Layer | Error | Surface |
|-------|-------|---------|
| `pkg/graph.Layout` | toposort cycle (shouldn't happen; validator catches) | returns wrapped `upstream.Toposort` error |
| `provisioner.Plan` | pairing failure | wrapped `ErrCrossTargetRefUnsupported` returned; no partial plan |
| `webapi` | inventory read error | 500 with JSON error envelope (existing pattern) |
| `webapi` | deployment ID not found | 404 with placeholder page |
| CLI | validator error | exit code 2; validator's existing stderr format |
| CLI | pairing failure | exit code 3; SVG still written |

## Non-goals (deferred to v1.2+)

- **Sugiyama crossing reduction.** v1.1 produces some edge crossings on dense graphs; acceptable for typical nimbusfab project sizes.
- **Server-side user preferences.** Cookie-per-browser is enough for v1.1. Sync across browsers / devices requires a `user_preferences` table that doesn't exist yet — defer to a future spec.
- **Real-time SSE updates of the graph.** Status badges reflect the deployment as of page load. The deployment-detail page already streams events via SSE; if a user wants live graph updates, that's a v1.2 enhancement.
- **Interactive editing.** This view is read-only. YAML is still the source of truth.
- **Cross-stack visualization.** Each graph shows one stack's components. Cross-stack dependencies (v2+ feature) are out of scope.
- **Per-edge metadata for timed transitions.** Surfaced by user as a future need ("wait X seconds between component layers"). The `Edge` struct in `pkg/graph` carries enough freedom (`kind` field) to extend later — no schema lock-in.
- **Mermaid / dagre / cytoscape integration.** Server-side SVG is the chosen path. If layout quality ever becomes a real problem, swap in a JS lib then.
- **Zoom / pan.** Graphs fit in viewport for typical project sizes. Add later if needed.

## Implementation phasing

Single phase: **Dependency Graph UI Phase 1.**

Estimated implementation tasks (~11):

1. `pkg/graph` package skeleton + types (`Input`, `Component`, `Ref`, `TargetSnapshot`, `PairingError`, `Output`, `NodeBox`, `EdgePath`).
2. `pkg/graph.Layout` rank + column assignment + coordinates for `Direction="tb"`; unit tests across fixtures (empty, single, linear, fan-out, fan-in, diamond).
3. `pkg/graph.Layout` `Direction="lr"` mode + tests (same fixtures).
4. `pkg/graph.RenderSVG` — produces self-contained SVG document; XML-validity test + golden-file tests for a couple of fixtures.
5. `upstream.PreflightPairing` + tests (single-cloud OK, cross-region fails, cross-cloud fails, multi-error accumulation).
6. `provisioner.Plan` integration — preflight call + fail-fast wrapping + tests asserting `ErrCrossTargetRefUnsupported` is wrapped.
7. `inventory.Component.UnmarshalIR()` helper + roundtrip test (writes a component via persistPlan, reads it back, refs match).
8. `internal/webapi/ui/templates/graph.html` + `pages.go` handlers for both `/ui/projects/{id}/graph` and `/ui/deployments/{id}/graph` + route registration + page-tabs nav added to existing project/deployment detail templates.
9. Direction cookie + query-param handling in handlers; segmented-button toggle JS in `app.js` (`nimbusfab.attachGraph()`); CSS additions; route tests asserting cookie/`?dir=` switch the rendered SVG.
10. Node click → side panel JS + per-target detail rendering + handler test asserting `data-targets-json` is populated correctly.
11. CLI `nimbusfab graph` subcommand + integration test against `network-only-project` and a new cross-region fixture (exit-code 3 path).

## Testing strategy

Mirrors the cross-component planning Phase 1 testing approach: unit tests for the pure-Go layout package and pairing function, repo round-trip tests for the schema migration, webapi handler tests using `httptest.Server`, CLI tests using the existing fake-runner harness, plus a real-tofu-free integration test for the CLI command.

Coverage gaps explicitly accepted:
- No browser-level test — the JS is ~50 LOC of toggle + click handlers, exercised via webapi route tests that assert the right SVG markup is emitted.
- No visual-regression test — `pkg/graph.RenderSVG` golden-file tests catch structural changes; pixel-level layout differences are not tracked.

## Open questions

None blocking. Possible v1.2 items surfaced:
- Should pairing errors be a `validator.Phase6` instead of (or in addition to) a provisioner pre-flight? Phase 6 would catch errors at `nimbusfab validate` time. Skipped for v1.1 — provisioner.Plan is the natural fail-fast point.
- Should the project-detail page get the graph embedded inline (a small `<svg>` thumbnail), or just the link? Spec ships the link only; embedded thumbnail is a polish item.
