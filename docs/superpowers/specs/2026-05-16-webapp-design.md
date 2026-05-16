# Web App Subsystem Spec

**Status:** Subsystem spec. Defines the self-hosted web app (HTTP server + UI) that wraps the same Engine library the CLI uses. Spec-only — splits the work into multiple implementation phases (HTTP Phase 1, UI Phase 1, Auth Phase 1, …).

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (web app role; REST + SSE convention; PAT vs cookie auth)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (Engine interfaces; event stream)
- `docs/superpowers/specs/2026-05-16-inventory-design.md` (Repo interface — Postgres/SQLite both implement it)
- `docs/superpowers/specs/2026-05-16-secrets-design.md` (SecretsBackend for credential resolution; web app needs the same hook)

**Depended on by:**
- Future implementation phases (HTTP Phase 1, UI Phase 1, Auth Phase 1, etc.).
- Inventory Phase 2 (Postgres backend) — server deployments expect concurrent readers/writers; SQLite works for development but Postgres is the recommended production backend.
- Cost Collector spec — exposes `cost_actuals` table via `/api/v1/costs`.
- GitOps daemon (v2) — uses the same engine library and inventory contract, but acts on git changes rather than HTTP requests.

---

## Context

nimbusfab's v1 product scope (per the architecture spec) is "CLI + self-hosted web app." The CLI is functionally complete: it validates projects, plans / applies / destroys / drifts deployments across AWS/Azure/GCP, computes parity reports, and estimates costs. The web app is the second half of v1.

**Why a web app at all?** The CLI is great for individual users iterating on infrastructure. The web app exists for:
- **Multiple users / teams** sharing the same projects. Centralized view of who deployed what, when, where, and what it cost.
- **Long-running operations.** Applies that take minutes can be triggered from a browser and watched live via SSE, rather than tying up a terminal.
- **Audit trail.** A persistent inventory of runs / deployments / drift status / cost actuals, queryable through the same UI the team uses to deploy.
- **Drift monitoring.** Background drift detection that emails / Slacks when anything diverges from declared state.
- **Cost dashboard.** Estimated vs. actual cost across clouds, drillable by component / target / time period.
- **OIDC / SSO.** Real organizations can't ship long-lived credentials in CI; the web app brokers per-user identities and resolves cloud credentials per-request.

**What the web app is NOT (in v1):**
- A WYSIWYG infrastructure editor. YAML is primary; the UI shows / explains but doesn't author. (Future: a YAML-syntax-aware editor with type-aware autocomplete.)
- A multi-org SaaS. v1 is self-hosted — one organization per running server. Multi-tenancy plumbing exists in the data model (org_id everywhere) but a single org_id is the realistic v1 deployment.
- A GitOps daemon. The architecture spec calls out a separate v2 daemon for that.

**Design principles:**
1. **Thin frontend layer.** All business logic lives in the engine library. The web backend is a translation layer: REST → engine calls → JSON. If a behavior needs both CLI and web access, it lives in the engine.
2. **REST + SSE only.** No GraphQL, no WebSockets. REST handles request/response; SSE handles run-event streaming. Both are HTTP-1.1 native, browser-native, and CLI-friendly (`curl -N` works).
3. **Server-rendered shell, JS-progressive enhancement.** The HTML loads a usable page from the server (project list, deployment detail, parity report); JavaScript adds live updates (SSE) and interactivity (apply button). No SPA framework — the UI is small enough that vanilla JS + a tiny templating layer suffices.
4. **One binary.** `nimbusfab-server` embeds the UI assets via `embed.FS`. No separate build step for "deploy the frontend"; the server tarball IS the deployable artifact.
5. **Single authoritative engine.** The web app constructs `engine.Config` exactly the same way the CLI does (same `CloudAdapters`, `ComponentTypes`, `SecretsBackend`, `Estimator`, `InventoryRepo`, `TofuRunner`). The Engine is unaware it's being invoked from HTTP rather than CLI.

---

## Scope

### In scope (this spec — collectively across HTTP/UI/Auth/Polish phases)

- `internal/webapi/` HTTP router (chi or net/http; see "Dependencies" below).
- `/api/v1/...` REST endpoints covering: projects, deployments, runs, drift, parity, costs, runs/{id}/events SSE.
- Cookie sessions (browser) + bearer Personal Access Tokens (CLI / scripts).
- OIDC SSO at the auth boundary (Google, GitHub, generic OIDC IdP).
- Idempotency-Key header support on mutating endpoints (POST / DELETE).
- HTML UI: server-rendered shell + progressive enhancement. Pages:
  - Login / OIDC callback.
  - Project list.
  - Project detail (components / targets / refs / parity / cost).
  - Deployments list per project.
  - Deployment detail (per-target run status, drift status).
  - Run detail (live SSE log stream, plan diff, parity report, cost estimate).
  - Cost dashboard (estimated vs. actual, time-series, drill-down).
  - Drift overview (anything drifting across the org).
- `embed.FS` of static UI assets (CSS, minimal JS, fonts).
- `cmd/server/` wired up: parses config, constructs engine, mounts router.
- Audit log: every mutating API call writes an `audit_logs` row (Inventory contract already has the table; nullRepo accepts and discards).
- Health / readiness endpoints: `/healthz`, `/readyz`.
- Configuration via env vars + optional YAML config file (paths, OIDC issuer, session cookie key, listen addr).

### Out of scope (deferred to v2+)

- GitOps daemon. Separate spec.
- YAML editor / linter integration in the UI. v2.
- Slack / email notifications for drift / failures. v2 (the data is there; the integration is the lift).
- Multi-org SaaS hosting. v1 is single-org self-hosted.
- Real-time collaborative editing.
- Approval workflows (e.g., "two reviewers required before apply to prod"). v2 — needs an RBAC + approval data model.
- Mobile-optimized UI. v1 is desktop-first.
- Custom dashboards / saved queries. v2.
- Webhook outbound integrations (post-run hooks). v2.
- Plugin UI for user-defined component types. v3-ish.
- API rate limiting per user / IP. v2 — for single-org self-hosted, latency-fairness is fine; production deployments with many users need it.

---

## Phase breakdown

The web app implementation is split into focused phases so each can ship independently:

| Phase | Name | Deliverable |
|---|---|---|
| HTTP Phase 1 | API skeleton | `/api/v1/projects`, `/api/v1/deployments`, `/api/v1/runs` GET endpoints; PAT auth; integration tests |
| UI Phase 1 | Read-only UI | Project list, project detail, deployment detail, run detail (no live updates yet); server-rendered HTML; embedded CSS |
| HTTP Phase 2 | Mutating endpoints + SSE | POST /api/v1/projects/{id}/plans, …/applies, …/destroys, …/drifts; SSE on /api/v1/runs/{id}/events; idempotency keys |
| UI Phase 2 | Live updates | SSE-driven log streaming on run detail page; Deploy / Destroy / Drift buttons; partial-failure handling |
| Auth Phase 1 | OIDC SSO | Cookie sessions; Login / Callback; PAT management page; audit log writes |
| Dashboards Phase 1 | Cost + parity | Cost dashboard (read-only, requires Cost Phase 2 snapshot data); parity overview across projects |
| Drift Phase 1 | Drift monitor | Background drift cron; drift overview page; per-target drift detail |
| Polish Phase 1 | OIDC providers + config | Google / GitHub / generic OIDC; YAML config file support; readiness endpoint logic; production-deploy docs |

This spec covers all of the above conceptually; each phase will get its own plan that references this spec.

---

## Architecture

```
              ┌─────────────────────────────────────┐
              │      nimbusfab-server (one Go bin)  │
              │                                     │
   Browser ──▶│  HTTP router (chi)                  │
              │   ├─ /api/v1/*   (REST + SSE)       │
              │   ├─ /ui/*       (server-rendered)  │
              │   ├─ /assets/*   (embed.FS)         │
              │   ├─ /auth/*     (OIDC + sessions)  │
              │   └─ /healthz, /readyz              │
              │                                     │
              │  ↓ calls Engine library             │
              │                                     │
              │  engine.Engine                      │
              │   ├─ CloudAdapters    (AWS/Az/GCP)  │
              │   ├─ ComponentTypes   (4 v1 types)  │
              │   ├─ SecretsBackend   (env+file…)   │
              │   ├─ InventoryRepo    (sqlite/PG)   │
              │   ├─ TofuRunner       (exec)        │
              │   └─ Estimator        (snapshot)    │
              └─────────────────────────────────────┘
                          │
                  ┌───────┴───────┐
                  ▼               ▼
            Postgres / SQLite   Tofu binary
            (inventory)         (state ops)
```

### Package layout

```
cmd/server/                  # main; flag/env parsing; engine wiring
internal/webapi/
    router.go                # chi mux setup; middleware chain
    middleware/
        auth.go              # cookie session + PAT validation
        idempotency.go       # Idempotency-Key dedup against inventory
        audit.go             # writes audit_logs on mutating requests
        logging.go           # structured per-request logs
        recover.go           # panic recovery → 500
    handlers/
        projects.go          # CRUD-ish for projects metadata
        deployments.go       # list / get deployments
        runs.go              # list / get / SSE stream
        plans.go             # POST plans (sync or async)
        applies.go           # POST applies
        destroys.go          # POST destroys
        drift.go             # POST drift; GET drift status
        parity.go            # GET parity reports
        costs.go             # GET costs (estimates + actuals)
        auth.go              # OIDC login / callback / logout / PATs
    sse/
        stream.go            # SSE writer + heartbeat
    ui/
        templates/           # html/template files, embedded
        pages.go             # page handlers (server-rendered HTML)
        assets/              # CSS / JS / fonts, embedded
internal/webapi/auth/
    session.go               # cookie sessions (gorilla/sessions or std)
    oidc.go                  # OIDC client wrapper
    pat.go                   # bearer token validation
```

---

## API surface

All endpoints under `/api/v1/`. Versioned at the URL prefix; future v2 lives at `/api/v2/`. JSON request and response bodies. Errors return `{"error": {"code": "...", "message": "...", "details": {...}}}` matching the existing `ir.Issue` shape where applicable.

### Read endpoints (HTTP Phase 1)

```
GET    /api/v1/projects
GET    /api/v1/projects/{project_id}
GET    /api/v1/projects/{project_id}/components
GET    /api/v1/projects/{project_id}/stacks
GET    /api/v1/projects/{project_id}/deployments
GET    /api/v1/deployments/{deployment_id}
GET    /api/v1/deployments/{deployment_id}/targets
GET    /api/v1/deployments/{deployment_id}/runs
GET    /api/v1/runs/{run_id}
GET    /api/v1/runs/{run_id}/events           # SSE
GET    /api/v1/runs/{run_id}/logs             # static log dump (post-run)
GET    /api/v1/drift                          # current drift status across org
GET    /api/v1/drift/{drift_id}
GET    /api/v1/parity                         # latest parity reports per project
GET    /api/v1/parity/{report_id}
GET    /api/v1/costs?groupBy=cloud&period=month
GET    /api/v1/costs/estimates
GET    /api/v1/costs/actuals
GET    /api/v1/healthz
GET    /api/v1/readyz
```

### Mutating endpoints (HTTP Phase 2)

```
POST   /api/v1/projects                                       # register a project (path / git URL / inline YAML)
PUT    /api/v1/projects/{project_id}
DELETE /api/v1/projects/{project_id}
POST   /api/v1/projects/{project_id}/plans                    # returns plan_id (sync) or run_id (async)
POST   /api/v1/projects/{project_id}/applies                  # body: { stack, planID?, autoApprove, partialFailure }
POST   /api/v1/projects/{project_id}/destroys
POST   /api/v1/projects/{project_id}/drifts
DELETE /api/v1/deployments/{deployment_id}                    # alias for destroy

POST   /api/v1/auth/pats                                      # mint a PAT
DELETE /api/v1/auth/pats/{pat_id}
```

All mutating endpoints accept an `Idempotency-Key` header. The middleware looks the key up in inventory (new `idempotency_keys` table; Inventory Phase 2 extension); on hit, returns the cached response. On miss, processes the request and caches the response. Keys expire after 24h.

### Auth endpoints

```
GET    /auth/login                  # OIDC redirect
GET    /auth/callback               # OIDC code exchange → session cookie
POST   /auth/logout                 # clears cookie
GET    /auth/me                     # returns current user (whoami)
```

PAT-only callers skip the cookie path entirely; the bearer middleware accepts `Authorization: Bearer nfp_...`.

---

## Auth model

### Two credential types

**Cookie session (browser):**
- OIDC code flow at `/auth/login` → `/auth/callback`.
- Server stores session in cookie (signed JWT, HttpOnly, Secure, SameSite=Lax).
- Cookie holds `(user_id, org_id, expires_at)`.
- Session refresh on activity; absolute timeout 12h.

**PAT (programmatic):**
- Prefix: `nfp_` (nimbusfab personal token), distinguishable in logs.
- Hashed at rest (`argon2id` or `bcrypt`); only the prefix + last 4 chars visible in the UI.
- PAT scope is the issuing user's org and permissions; v1 has no per-PAT scope reduction (deferred).
- 30-day default expiry; renewable.

### OIDC providers

v1 supports any OIDC-compliant provider. Tested configurations: Google Workspace, GitHub, Auth0, Keycloak. Configuration via env vars:

```
NIMBUSFAB_OIDC_ISSUER=https://accounts.google.com
NIMBUSFAB_OIDC_CLIENT_ID=...
NIMBUSFAB_OIDC_CLIENT_SECRET=<from secrets.Backend ref>
NIMBUSFAB_OIDC_REDIRECT_URL=https://nimbusfab.example.com/auth/callback
NIMBUSFAB_OIDC_ALLOWED_DOMAINS=example.com   # comma-separated; empty = allow all authenticated
```

The OIDC client secret is itself a `credentialRef` resolved through `secrets.Backend` (same machinery the engine uses for cloud creds). Closes the "where does the server keep its OWN secrets" loop without inventing a new mechanism.

### No-auth mode

For local development: `NIMBUSFAB_AUTH_MODE=disabled` short-circuits middleware to attach a fixed `(user_id="dev", org_id="default")` to every request. Useful for `make dev` workflows. Production binaries log a startup warning when this is enabled; release notes call it out as not for production.

---

## Multi-tenancy

Every inventory table has an `org_id` column already (per inventory spec). The web app's auth middleware sets `org_id` on the request context; every handler scopes its inventory queries to that org. SQL constraint: `WHERE org_id = $1` in every read; INSERT statements populate it from context.

v1 self-hosted has one org_id ("default" if disabled-auth, or derived from OIDC email domain by default). v2 multi-org is a data-model question (org provisioning, billing) more than a code question — the scoping plumbing is already in place.

---

## SSE: run event streaming

Endpoint: `GET /api/v1/runs/{run_id}/events`

Content-Type: `text/event-stream`. Each event:

```
id: <monotonic int>
event: <kind>           # "start" | "log" | "progress" | "success" | "failure"
data: {"timestamp": "...", "message": "...", "tofu_phase": "...", "target_id": "..."}
```

Server emits a heartbeat comment (`: ping\n\n`) every 15 seconds to keep proxies / load balancers from closing the connection.

Reconnect protocol: client sends `Last-Event-ID` header on reconnect; server resumes from that event ID by replaying events from the inventory's `run_logs` table. Events older than 24h are not guaranteed to be available.

Run lifecycle:
1. `POST /api/v1/projects/{id}/applies` returns `{ "run_id": "run-..." }`.
2. Client opens SSE on `/api/v1/runs/{run_id}/events`.
3. Server attaches a per-run `chan RunEvent` (provisioner already emits these) to the SSE writer.
4. Each event from the channel becomes one SSE event. Channel close → final SSE `complete` event → connection closes.

If two clients connect to the same run, both receive the full stream (fan-out via subscriber list on the run handle).

### Async vs. sync apply

- Default: async. POST returns the run_id immediately; client streams via SSE.
- `?wait=true`: server holds the connection until the run completes, then returns the final ApplyResult JSON. Useful for shell scripts that don't want to parse SSE.
- 5-minute hard timeout on `?wait=true` to prevent stuck connections; clients should fall back to SSE for longer runs.

---

## UI design

### Visual stack

- **HTML:** server-rendered with Go's `html/template`. One layout template, one page template per route.
- **CSS:** single hand-authored stylesheet, ~3 KB after minification. Embedded via `embed.FS`. No Tailwind / Bootstrap.
- **JS:** minimal vanilla. SSE EventSource for run streaming. `fetch()` for button actions (Deploy / Destroy / Drift). No SPA framework. Total JS: ~5 KB.
- **Fonts:** system stack. No web fonts (avoid layout shift, avoid third-party tracking).

The whole UI is usable without JS — read-only views fully server-rendered; mutating actions go through standard `<form method="POST">` submission, with progressive JS enhancement adding live updates and AJAX submission.

### Page inventory

| Route | Purpose |
|---|---|
| `/` | Redirects to `/ui/projects` if authed, `/auth/login` otherwise |
| `/ui/projects` | List of projects this user can see |
| `/ui/projects/{id}` | Project detail: components, stacks, recent deployments, parity summary, cost summary |
| `/ui/projects/{id}/components` | Component browser with type-grouped view |
| `/ui/projects/{id}/deployments` | Deployment history table |
| `/ui/deployments/{id}` | Per-target run status, drift status badges |
| `/ui/runs/{id}` | Live log stream + plan diff + parity report + cost estimate |
| `/ui/drift` | Org-wide drift overview |
| `/ui/parity` | Org-wide parity overview |
| `/ui/costs` | Cost dashboard (time-series chart + breakdown table) |
| `/ui/account/pats` | Manage your personal access tokens |
| `/ui/account/sessions` | Active sessions (logout from a single device) |
| `/auth/login` | OIDC redirect |

### Interaction patterns

- **Deploy a project:** From project detail, click "Deploy" → modal asks for stack + partial-failure policy → POST → redirect to run detail → SSE live-updates the log stream.
- **Watch a drift:** Drift overview shows red badges; click → drift detail (drifted resource list with attribute-level diffs).
- **Compare costs:** Cost dashboard defaults to "this month, grouped by cloud, all projects." Filters: project / cloud / period / time range. Time-series chart + breakdown table side by side.
- **Mint a PAT:** Account → New PAT → name + expiry → server returns full token ONCE (with copy button) → token list shows prefix + last 4 chars only.

---

## Dependencies

- `github.com/go-chi/chi/v5` — router. Stable, std-net/http compatible, lightweight. Already widely used in the Go ecosystem.
- `github.com/coreos/go-oidc/v3` — OIDC client.
- `golang.org/x/oauth2` — OAuth2 helpers (transitive).
- `github.com/gorilla/sessions` or std-lib `securecookie` — cookie session signing. Lean toward std-lib for fewer deps.
- `golang.org/x/crypto/argon2` — PAT hashing.
- `html/template` — server rendering (std lib).
- `embed` — static assets (std lib).

No SPA framework, no client-side build step, no JS bundler. The entire UI ships as embedded files in the Go binary.

---

## Configuration

Server config via env vars (12-factor):

| Var | Default | Meaning |
|---|---|---|
| `NIMBUSFAB_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `NIMBUSFAB_DB_DSN` | `sqlite:./nimbusfab.db` | Inventory DSN; `postgres://...` for Phase 2 backend |
| `NIMBUSFAB_AUTH_MODE` | `oidc` | `oidc` \| `disabled` |
| `NIMBUSFAB_OIDC_ISSUER` | — | OIDC issuer URL |
| `NIMBUSFAB_OIDC_CLIENT_ID` | — | OIDC client ID |
| `NIMBUSFAB_OIDC_CLIENT_SECRET_REF` | — | secretsRef the server resolves to get the client secret |
| `NIMBUSFAB_SESSION_KEY_REF` | — | secretsRef for cookie signing key |
| `NIMBUSFAB_WORKROOT` | `~/.nimbusfab/work` | Engine WorkRoot |
| `NIMBUSFAB_TOFU_BINARY` | `tofu` | Path / name of tofu binary |
| `NIMBUSFAB_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

Optional YAML config file at `~/.nimbusfab/server.yaml` supplements env vars (env vars win on conflict). YAML allows multi-line / structured config (OIDC scopes list, allowed-domains list).

---

## Persistence schema additions

The Inventory spec already covers `users`, `orgs`, `projects`, `deployments`, `deployment_targets`, `runs`, `run_logs`, `drift_status`, `parity_reports`, `cost_estimates`, `cost_actuals`, `audit_logs`. The web app adds:

| Table | Purpose | Owner |
|---|---|---|
| `sessions` | Cookie session state (rotated each login; expired session reaper) | Auth Phase 1 |
| `pats` | Personal Access Tokens: id, user_id, name, hash, prefix, last_used_at, expires_at | Auth Phase 1 |
| `idempotency_keys` | (org_id, key, response_json, expires_at) for replay-safe POST | HTTP Phase 2 |

All three tables are columnar additions to the same inventory contract; the SQLite implementation gains them in Inventory Phase 2 alongside the Postgres backend. Backend-agnostic SQL (no Postgres-only features).

---

## Error handling

REST endpoints return:
- `200` for successful GETs.
- `201 Created` for successful POSTs with a Location header pointing at the new resource.
- `202 Accepted` for async POSTs (apply returns 202 with `run_id`; client streams via SSE).
- `400 Bad Request` for client errors with `{"error": {...}}` body.
- `401 Unauthorized` for missing / invalid auth.
- `403 Forbidden` for valid auth but no permission.
- `404 Not Found` for missing resources.
- `409 Conflict` for idempotency-key conflicts (same key, different body).
- `500 Internal Server Error` for engine / runner failures.

Error body shape:

```json
{
  "error": {
    "code": "ErrValidatorTypeSpec",
    "message": "component \"web-net\" spec missing required field 'cidr'",
    "path": "components[0].spec.cidr"
  }
}
```

Validator codes (`ErrValidator*`), secrets codes (`ErrSecretsRefUnresolved`), provisioner errors all surface through this shape. Internal panics → 500 with a generic message; the panic + stack trace lands in server logs only.

---

## Audit log

Every mutating API call appends a row to `audit_logs`:

| Column | Example |
|---|---|
| `id` | `audit-uuid` |
| `org_id` | from session |
| `user_id` | from session |
| `action` | `apply` / `destroy` / `drift` / `pat.create` / `project.update` |
| `resource_type` | `project` / `deployment` / `pat` |
| `resource_id` | resource UUID |
| `request_method` | `POST` |
| `request_path` | `/api/v1/projects/abc/applies` |
| `request_idempotency_key` | nullable |
| `response_status` | HTTP code |
| `created_at` | timestamp |

Read endpoints do not write audit rows (would 10× the table for no value). The inventory's existing `audit_logs` repo gains this writer; SQLite + Postgres both support it.

---

## Verification (design-level)

1. **End-to-end apply walkthrough.** Browser POSTs `/api/v1/projects/{id}/applies` with cookie auth → web-api authenticates → calls `engine.Apply` async → returns `run_id` → browser opens SSE on `/api/v1/runs/{run_id}/events` → server fans out provisioner `RunEvent` channel to SSE writer → browser renders log lines live → run completes → SSE final event → page polls `/api/v1/runs/{run_id}` for full result.
2. **CLI ↔ web-api equivalence.** Same project, deployed once via `nimbusfab apply` and once via `POST /api/v1/.../applies`. Both produce identical `deployments` and `runs` rows in inventory (only `triggered_by` differs).
3. **OIDC code-flow walkthrough.** New user visits `/`, redirects to `/auth/login`, OIDC redirects to IdP, IdP redirects back to `/auth/callback` with code, server exchanges code for token, derives user identity, creates session, sets cookie, redirects to `/ui/projects`.
4. **PAT walkthrough.** User creates PAT in UI → server generates `nfp_<random>`, argon2-hashes, stores → UI shows full token once → user uses PAT in `curl -H "Authorization: Bearer nfp_..." /api/v1/projects` → middleware hashes incoming token, looks up by hash, attaches user context.
5. **Idempotency walkthrough.** Client POSTs apply with `Idempotency-Key: abc-123` → server sees no prior key for this org → processes request, caches response → 30 seconds later client retries with same key → server returns cached 202 + run_id (does NOT re-trigger apply).
6. **Concurrent SSE walkthrough.** Two browser tabs open SSE for the same run → both receive identical event streams (server fans out from the same channel).
7. **Disabled-auth walkthrough.** Dev runs `NIMBUSFAB_AUTH_MODE=disabled nimbusfab-server`. Hitting `/api/v1/projects` succeeds without cookies / PATs; every request attaches `user_id="dev", org_id="default"`. Startup logs the warning.
8. **Multi-org SQL scoping walkthrough.** Hand-trace `GET /api/v1/projects` with `org_id="acme"` — every SELECT carries `WHERE org_id = 'acme'`; no leakage to other orgs' rows.

---

## Future hooks (not v1 phases)

- **GitOps daemon** — separate binary (`nimbusfab-gitops`) that watches a git repo, runs the same engine on push events, posts run results back as commit statuses or PR comments. Shares the engine library and inventory contract.
- **Webhook outbound integrations** — Slack on apply failure, email on drift detection, PagerDuty for partial-failure runs.
- **RBAC + approval workflows** — Roles (admin, deployer, viewer); approval requirements per project / stack ("prod applies require 2 approvers").
- **YAML editor with type-aware autocomplete** — Monaco or similar in the UI, fed by the existing per-type SpecSchemas.
- **Plan diff visualization** — Color-coded diff renderer in the UI for tofu plan output.
- **Cost forecasting** — Project monthly cost from trend; alert on anomalies.
- **Multi-org SaaS** — Org provisioning, billing, per-org rate limits, isolated workspaces.
- **Mobile UI** — Responsive design for the existing pages; or a separate read-only mobile app.
- **GraphQL** — If the UI grows complex enough to benefit. v1 deliberately avoids it.
- **WebAuthn / passkeys** — Replace / complement OIDC for end-user auth.
- **Per-PAT scope reduction** — Scope tokens to specific projects or read-only access.
- **Plugin UI for user-defined component types** — surface Type.Outputs() and SpecSchema in the component browser, even for types from external plugins.
