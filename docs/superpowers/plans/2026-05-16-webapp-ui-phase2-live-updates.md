# Web App UI Phase 2 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Browser interactivity. After UI Phase 2, opening a deployment detail page shows three buttons (Deploy / Destroy / Drift) that POST to the HTTP Phase 2 endpoints, and a log pane that streams `RunEvent`s via `EventSource` until the operation completes.

**Architecture:**
- New `internal/webapi/ui/assets/app.js` — vanilla JS, ~80 LOC. Hijacks button clicks, opens `EventSource`, renders events into a `<pre>` log pane, manages button enable/disable state. No SPA framework.
- Updated `deployment_detail.html` template — adds the action bar (3 buttons + status badge) and an empty log pane that JS populates. Page is fully usable without JS for read-only purposes; without JS the buttons just do nothing (or could fall back to plain form POSTs if we wanted, but Phase 2 stays JS-only for interactivity per the spec's "progressive enhancement" stance).
- CSS additions for the action bar + log pane styling (~30 lines).
- One additional template helper: `csrfToken` placeholder for future CSRF (Auth Phase 1) — for now returns empty string.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-webapp-ui-phase2/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.
- Vanilla JS only — no jQuery, no React, no build step. Total JS ≤ 5KB minified-but-readable.

**Out of scope:**
- Plan trigger (CLI plans; web app applies/destroys/drifts existing deployments).
- Auth / CSRF tokens (Auth Phase 1).
- Confirmation modals (browser `confirm()` is fine for Phase 2).
- Log filtering / search (Polish Phase 1).
- Reconnect / resume after disconnect (depends on RunLogs replay).
- Cost / parity dashboards (Dashboards Phase 1).

---

## Task 1: JS asset + CSS additions

**Files:**
- Create: `internal/webapi/ui/assets/app.js`
- Edit: `internal/webapi/ui/assets/style.css`
- Edit: `internal/webapi/ui/pages_test.go` (assert app.js embeds and loads)

- [ ] **Step 1: app.js**

Vanilla module-pattern script. Exposes one function `attachDeploymentActions(deploymentID)` that the template calls inline. Public surface:
- POSTs to the right endpoint on button click
- Opens `EventSource` on `/api/v1/deployments/{id}/events`
- Renders each event as one `<div class="log-line">` (timestamp, target, kind, message)
- On `event: complete`, closes the source and re-enables buttons; refreshes the page after 500ms so target statuses re-read from inventory
- Confirms before Destroy via `confirm()`
- Handles auth: if a `data-api-token` attribute is set on the script tag, every fetch includes `Authorization: Bearer <token>` (server-rendered when `NIMBUSFAB_API_TOKEN` is configured — the UI cannot easily share a session with the API in Phase 2; Auth Phase 1 unifies this with cookie sessions)

```js
(function () {
  "use strict";

  function attachDeploymentActions(deploymentID) {
    const log = document.getElementById("event-log");
    const buttons = document.querySelectorAll("[data-action]");
    const apiToken = document.currentScript ? document.currentScript.dataset.apiToken : "";

    function authHeaders() {
      return apiToken ? { Authorization: "Bearer " + apiToken } : {};
    }

    function appendLog(html, cls) {
      const div = document.createElement("div");
      div.className = "log-line " + (cls || "");
      div.innerHTML = html;
      log.appendChild(div);
      log.scrollTop = log.scrollHeight;
    }

    function setBusy(busy) {
      buttons.forEach((b) => (b.disabled = busy));
    }

    function startStream() {
      const src = new EventSource("/api/v1/deployments/" + encodeURIComponent(deploymentID) + "/events");
      src.onmessage = (e) => appendLog(escapeHtml(e.data));
      src.addEventListener("start", (e) => appendLog(formatEvt("start", e.data), "start"));
      src.addEventListener("log", (e) => appendLog(formatEvt("log", e.data), "log"));
      src.addEventListener("progress", (e) => appendLog(formatEvt("progress", e.data), "log"));
      src.addEventListener("success", (e) => appendLog(formatEvt("success", e.data), "success"));
      src.addEventListener("failure", (e) => appendLog(formatEvt("failure", e.data), "failure"));
      src.addEventListener("complete", () => {
        appendLog("<em>operation complete</em>", "complete");
        src.close();
        setBusy(false);
        setTimeout(() => window.location.reload(), 500);
      });
      src.onerror = () => {
        appendLog("<em>connection error</em>", "failure");
        src.close();
        setBusy(false);
      };
      return src;
    }

    function trigger(op) {
      if (op === "destroys" && !confirm("Destroy this deployment? This is not reversible.")) return;
      setBusy(true);
      log.innerHTML = "";
      appendLog("<em>kicking off " + op + "…</em>");
      const src = startStream();
      fetch("/api/v1/deployments/" + encodeURIComponent(deploymentID) + "/" + op, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: "{}",
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json();
        })
        .catch((err) => {
          appendLog("<em>POST failed: " + escapeHtml(err.message) + "</em>", "failure");
          setBusy(false);
          src.close();
        });
    }

    buttons.forEach((b) => {
      b.addEventListener("click", () => trigger(b.dataset.action));
    });
  }

  function formatEvt(kind, raw) {
    try {
      const o = JSON.parse(raw);
      const target = o.cloud && o.region ? o.cloud + "/" + o.region : "";
      return (
        "<span class=\"ts\">" + escapeHtml(o.timestamp || "") + "</span> " +
        "<span class=\"target\">" + escapeHtml(target) + "</span> " +
        "<span class=\"kind\">" + escapeHtml(kind) + "</span> " +
        "<span class=\"msg\">" + escapeHtml(o.message || "") + "</span>"
      );
    } catch (e) {
      return escapeHtml(raw);
    }
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
  }

  window.nimbusfab = { attachDeploymentActions: attachDeploymentActions };
})();
```

- [ ] **Step 2: CSS additions**

Append to `style.css`:

```css
.actions { display: flex; gap: 0.5rem; margin: 1rem 0; align-items: center; }
.actions button {
  font: inherit;
  padding: 0.4rem 0.9rem;
  border: 1px solid #2266cc;
  background: #2266cc;
  color: #fff;
  border-radius: 0.25rem;
  cursor: pointer;
}
.actions button:hover { background: #1a4f9a; }
.actions button:disabled { background: #aaa; border-color: #aaa; cursor: not-allowed; }
.actions button.destructive { background: #c33; border-color: #c33; }
.actions button.destructive:hover { background: #a22; }

.log-pane {
  border: 1px solid #ddd;
  background: #fafafa;
  border-radius: 0.3rem;
  padding: 0.5rem 0.75rem;
  margin: 1rem 0;
  max-height: 50vh;
  overflow-y: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
}
.log-pane:empty::before { content: "No live events yet — click an action above to begin."; color: #999; font-style: italic; font-family: inherit; }
.log-line { padding: 0.1rem 0; }
.log-line .ts { color: #888; margin-right: 0.5em; }
.log-line .target { color: #2266cc; margin-right: 0.5em; }
.log-line .kind { display: inline-block; min-width: 4em; color: #555; }
.log-line.success .msg { color: #1a5a1a; }
.log-line.failure { background: #fff0f0; }
.log-line.complete { color: #555; }
```

- [ ] **Step 3: Tests**

Add a test in `pages_test.go` that `AssetsFS()` returns `app.js` non-empty and that the new CSS classes (`actions`, `log-pane`, `log-line`) appear in `style.css`.

- [ ] **Step 4: Build + test + commit** `ui: app.js for buttons + EventSource; styling`

---

## Task 2: Deployment detail page gains actions + log pane

**Files:**
- Edit: `internal/webapi/ui/templates/deployment_detail.html`
- Edit: `internal/webapi/ui/pages_test.go`

- [ ] **Step 1: Template additions**

After the kv block (Deployment ID / Project / Stack / etc.), before the Targets table, add:

```html
<div class="actions">
  <button type="button" data-action="applies">Deploy</button>
  <button type="button" data-action="destroys" class="destructive">Destroy</button>
  <button type="button" data-action="drifts">Detect drift</button>
</div>

<h2>Live events</h2>
<div id="event-log" class="log-pane"></div>

<script src="/assets/app.js"{{if .APIToken}} data-api-token="{{.APIToken}}"{{end}}></script>
<script>nimbusfab.attachDeploymentActions({{.Deployment.ID}});</script>
```

The `APIToken` is passed from the handler via the page data map; tests will verify both with-and-without-token rendering. The trailing inline script uses Go's auto-JSON-encoding of action values via `{{.Deployment.ID}}` — html/template escapes strings safely for JS contexts.

- [ ] **Step 2: Handler change**

`DeploymentDetail` handler in `pages.go` adds `"APIToken": r.OrgID` — wait, that's wrong. The renderer needs access to the API token to pass it through. Two options:
- Add `APIToken` field to the Renderer struct (set at NewRenderer time)
- Read from a request context value (set by middleware)

For Phase 2, just add `APIToken string` to the Renderer struct and have `webapi.New` pass it from `cfg.APIToken`.

```go
// pages.go
type Renderer struct {
    Repo     inventory.Repo
    OrgID    string
    APIToken string  // for JS to authenticate /api/v1/* mutating calls
    pages    map[string]*template.Template
}

func NewRenderer(repo inventory.Repo, orgID, apiToken string) (*Renderer, error) { ... }

func (r *Renderer) DeploymentDetail(...) {
    r.render(w, "deployment_detail.html", map[string]any{
        "Deployment": d,
        "Targets":    enriched,
        "APIToken":   r.APIToken,
    })
}
```

Update `webapi.New` callsite and tests.

- [ ] **Step 3: Tests**

- `TestDeploymentDetail_RendersActionBar`: body contains the 3 buttons and the `event-log` div.
- `TestDeploymentDetail_RendersScript`: body contains `<script src="/assets/app.js"`.
- `TestDeploymentDetail_APITokenWiredIntoScriptTag`: with APIToken set on the renderer, the script tag has `data-api-token="secret"`; without, the attribute is absent.

- [ ] **Step 4: Build + test + commit** `ui: deployment detail page gets action bar + live log pane`

---

## Task 3: Router wires APIToken into renderer; smoke test

**Files:**
- Edit: `internal/webapi/router.go`
- Edit: `internal/webapi/router_test.go` (smoke: GET /ui/deployments/{id} contains script tag)

- [ ] **Step 1: Router**

```go
renderer, err := ui.NewRenderer(cfg.Repo, cfg.OrgID, cfg.APIToken)
```

That's the only change. The `APIToken` flow is now:
- env var → Config.APIToken
- Config.APIToken → middleware (auth on /api/v1/* if set)
- Config.APIToken → Renderer (UI script tag, so browser-side fetch can include the Bearer header)

For Phase 2 with no real auth, the API token is shared globally. Auth Phase 1 will replace this with proper per-user PATs / cookie sessions; the JS will read the cookie automatically (browser sends it) and the script tag won't need a baked-in token.

- [ ] **Step 2: Test + smoke**

Existing tests pass. Add: GET /ui/deployments/{id} contains `<script src="/assets/app.js"`. Smoke-test the binary by hand: open the UI, click Deploy on a real deployment ID, watch events stream.

- [ ] **Step 3: Build + test + commit** `webapi: thread APIToken through renderer to script tag`

---

## Task 4: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: README** brief note on browser-triggered deployments.

- [ ] **Step 2: CHANGELOG** entry under "Unreleased — Web App UI Phase 2":
  - app.js (~80 LOC, vanilla, no framework)
  - Deploy/Destroy/Drift buttons + live log pane on deployment detail
  - APIToken propagation
  - Out of scope: confirmation modals, log filtering, reconnect

- [ ] **Step 3: Final test + gofmt**

- [ ] **Step 4: Commit** `docs: UI Phase 2 merged — buttons + EventSource live updates`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/webapp-ui-phase2 -m "Merge feat/webapp-ui-phase2: buttons + live log streaming"
git push origin main
git push origin feat/webapp-ui-phase2
```
