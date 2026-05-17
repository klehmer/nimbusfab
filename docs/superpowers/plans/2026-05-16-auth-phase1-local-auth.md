# Auth Phase 1 (Local Auth + PATs + Sessions) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** End the two Phase-1 auth stubs (env-var bearer token for API; baked-in `org_id="default"` for UI). Local user accounts with bcrypt-hashed passwords + cookie sessions for UI; argon2-hashed Personal Access Tokens for API. Disabled-auth mode preserved for `make dev` workflows. Audit-log middleware writes one row per mutating request.

**Scope cut from the web app spec:** OIDC SSO deferred to Auth Phase 2. Local password auth is the stepping-stone (Grafana / Gitea pattern); OIDC layers on the same session foundation cleanly later.

**Architecture:**
- `pkg/secrets` already loads the session-signing key; no new dep there.
- `golang.org/x/crypto/bcrypt` for user passwords; `golang.org/x/crypto/argon2` for PATs.
- New `internal/webapi/auth` package: session sign/verify, password hashing, PAT generation + verification.
- `UserRepo` and `ApiTokenRepo` wired for both backends (tables already exist post-Inventory-Persistence).
- New auth middleware replaces the bearer-token stub: tries cookie session, then `Authorization: Bearer nfp_...` PAT, then 401 (or attaches `(user_id="dev", org_id="default")` in disabled mode).
- New `/auth/login` (form POST), `/auth/logout` (POST), `/auth/me` (GET) handlers.
- New `/ui/login` server-rendered form.
- Audit middleware on POST endpoints writes `inventory.AuditLog.Append`.
- CLI: `nimbusfab user create --email --password` and `nimbusfab pat create --user-id` for bootstrapping.

**Out of scope (deferred):**
- OIDC SSO — Auth Phase 2.
- Password reset flow / email verification — Auth Phase 2 or Polish.
- PAT management UI page (CLI is the v1 bootstrap; UI page is 1B).
- Multi-org provisioning (single-org self-hosted is the v1 deployment shape).
- Server-side session table + reaper (stateless signed cookies don't need it).
- RBAC / per-role permissions (single user role for v1).

---

## Task 1: ApiTokenRepo + UserRepo for both backends

**Files:**
- Edit: `pkg/inventory/repo.go` (add ApiTokenRepo interface; flesh out UserRepo)
- Edit: `pkg/inventory/nullrepo.go`
- Create: `internal/inventory/sqlite/users.go`, `internal/inventory/sqlite/api_tokens.go`
- Create: `internal/inventory/postgres/users.go`, `internal/inventory/postgres/api_tokens.go`
- Edit: both `sqlite.go` + `postgres.go` accessors
- Edit: both `notwired.go` files (remove errUsers + errApiTokens stubs)

UserRepo additions (`Get` + `GetByEmail` + `Create` already exist as stubs; add `UpdatePasswordHash`):

```go
type UserRepo interface {
    Get(ctx, orgID, id) (*User, error)
    GetByEmail(ctx, orgID, email) (*User, error)
    Create(ctx, u) error
    UpdatePasswordHash(ctx, orgID, id, hash) error
}

// User struct already has Email + DisplayName + OIDC{Provider,Subject}.
// Add: PasswordHash []byte
type User struct {
    ID, OrgID, Email, DisplayName string
    PasswordHash                   []byte // bcrypt; nil for OIDC-only users
    OIDCProvider, OIDCSubject      string
    IsLocal                        bool
    CreatedAt                      time.Time
}
```

`schema 0001_init` has a `password_hash` column? Check; if not, add a migration. (Spoiler: the existing `users` table doesn't have it — needs `0002_password_hash.sql` for both flavors.)

New ApiTokenRepo:

```go
type ApiToken struct {
    ID, OrgID, UserID, Name, Prefix string
    TokenHash    []byte    // argon2id; never exposed
    CreatedAt    time.Time
    LastUsedAt   *time.Time
}

type ApiTokenRepo interface {
    Create(ctx, t ApiToken) error
    GetByPrefix(ctx, prefix string) (*ApiToken, error)  // efficient lookup
    ListByUser(ctx, orgID, userID string) ([]ApiToken, error)
    UpdateLastUsed(ctx, id string, t time.Time) error
    Revoke(ctx, orgID, id string) error
}
```

Schema: needs a `prefix TEXT UNIQUE` column on api_tokens. The Postgres init has `token_hash UNIQUE`; we add `prefix TEXT NOT NULL UNIQUE`. SQLite mirror.

Both backends implement the repo. Tests: bcrypt round-trip, PAT prefix lookup, list-by-user, revoke + can't-lookup.

---

## Task 2: Migration `0002_password_hash_and_pat_prefix.sql`

Two files: `0002_init.sql` (Postgres) + `0002_init.sqlite.sql` (SQLite). Adds:
- `users.password_hash BYTEA / BLOB`
- `api_tokens.prefix TEXT NOT NULL UNIQUE`

Migrations runner already supports versioned files; just drop them in.

Test: `TestRunMigrations_FreshDB` extended to assert the new columns exist.

---

## Task 3: `internal/webapi/auth` package

Three small files:

- `password.go`: `HashPassword(plaintext) ([]byte, error)` (bcrypt cost 12), `VerifyPassword(hash, plaintext) bool`.
- `session.go`: cookie sign/verify with HMAC-SHA256. Encodes `{user_id, org_id, exp}` as JSON, base64-encodes, appends `.<hmac>`. `SignSession(key, sess) string`, `VerifySession(key, cookie) (*Session, error)`.
- `pat.go`: `GeneratePAT() (token, prefix string, hash []byte)` returns `nfp_<8-char-prefix>_<32-char-secret>`; argon2id over the secret part. `VerifyPAT(token, prefix, hash) bool` re-derives and compares.

Tests for round-trips, tamper detection, expired sessions, wrong-key rejection.

---

## Task 4: Auth middleware

`internal/webapi/middleware/auth.go` gets a new `Auth(repo, sessionKey, mode)` middleware that supersedes BearerToken:

- `mode == "disabled"`: attach `(user_id="dev", org_id="default")` to request context, pass through.
- Else: try cookie session first; if valid, attach user context. Else try `Authorization: Bearer nfp_...`; look up by prefix, verify hash, update last_used_at, attach user. Else 401 (for API routes; for UI routes redirect to /auth/login).

User-context plumbed via a `context.WithValue(ctx, userKey, *User)`.

Replaces the existing `BearerToken` middleware in the router. The env-var stub goes away.

Tests: each path (disabled / cookie / pat / 401 / redirect for UI).

---

## Task 5: Login / logout / me handlers

`internal/webapi/api/auth.go`:

- `POST /auth/login`: form-encoded `email` + `password`; on success sets session cookie + redirects to `/ui/projects` (HTML) or returns 200 JSON (API).
- `POST /auth/logout`: clears cookie; redirects to `/ui/login`.
- `GET /auth/me`: returns `{userId, email, displayName, orgId}` JSON for the current session.

`internal/webapi/ui/templates/login.html`: form with email + password fields + submit.

---

## Task 6: Audit-log middleware

`internal/webapi/middleware/audit.go`: wraps each mutating endpoint. After the handler runs, append one `inventory.AuditEntry` with `(org_id, actor_user_id, verb, target, payload_json, timestamp)`. Verb derived from path (`applies` → "deployment.apply"; `destroys` → "deployment.destroy"; `drifts` → "deployment.drift").

Router applies the middleware to the three POST routes + future auth-sensitive endpoints (pat.create later).

---

## Task 7: CLI bootstrap commands

`cmd/cli/user.go`: `nimbusfab user create --email --password [--org default]`. Hashes password, creates Org if not exists, creates User.

`cmd/cli/pat.go`: `nimbusfab pat create --user-id [--name "ci-script"]`. Generates PAT, prints the FULL token ONCE (with copy-friendly format), creates ApiToken row with the hash. Future invocations of `pat list` show prefix + last 4 chars.

These give the dev / admin a way to bootstrap without a UI.

---

## Task 8: cmd/server config + UI login template + router wiring

- env vars: `NIMBUSFAB_AUTH_MODE` (`local` | `disabled`; default `local`), `NIMBUSFAB_SESSION_KEY_REF` (secrets ref returning the cookie signing key), `NIMBUSFAB_OIDC_*` (reserved for Phase 2; unused for now).
- Session key: if unset and mode != disabled, generate a random 32-byte key at startup with a WARN log (only for dev — a fresh key on each restart invalidates all sessions).
- Router: replace `BearerToken(apiToken)` with `Auth(repo, sessionKey, mode)`; add `/auth/login` + `/auth/logout` + `/auth/me` routes; the disabled-mode middleware attaches dev user automatically.
- UI: top-nav shows current user + logout link when authenticated; login page renders when not.

---

## Task 9: Docs

README status update + comprehensive CHANGELOG entry covering the entire phase. Out-of-scope items spell out OIDC deferral.

---

## Merge

Standard pattern.
