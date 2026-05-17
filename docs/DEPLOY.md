# Deploying nimbusfab-server

Production deployment guide for the self-hosted web app. CLI users skip this
file — `nimbusfab` is a single static binary.

## What ships in v1

- **CLI** (`nimbusfab`): plan/apply/destroy/drift across AWS / Azure / GCP.
- **Web server** (`nimbusfab-server`): browser UI + JSON API over the same
  engine, backed by a shared inventory DB.
- **Inventory backends**: SQLite (default; single-node) and Postgres
  (recommended for multi-instance deployments).
- **Auth**: local username + bcrypt password + HMAC-signed cookie sessions
  for the UI; argon2-hashed Personal Access Tokens for the API. OIDC is
  deferred to Auth Phase 2 (post-v1).

## Quick start (single VM, SQLite, local auth)

```bash
# 1. Build
go build -o /usr/local/bin/nimbusfab ./cmd/cli/
go build -o /usr/local/bin/nimbusfab-server ./cmd/server/

# 2. Create a working directory + initial admin user
mkdir -p /var/lib/nimbusfab
export NIMBUSFAB_DB_DSN=sqlite:/var/lib/nimbusfab/inventory.db
nimbusfab user create --email admin@example.com --password 'STRONG-PASSWORD-HERE' \
  --display-name "Admin" --inventory-dsn "$NIMBUSFAB_DB_DSN"

# 3. Generate a stable session-signing key (32 random bytes, base64-encoded)
openssl rand -base64 32 > /var/lib/nimbusfab/session.key
chmod 0600 /var/lib/nimbusfab/session.key

# 4. Run the server
export NIMBUSFAB_LISTEN_ADDR=:8080
export NIMBUSFAB_AUTH_MODE=local
export NIMBUSFAB_SESSION_KEY=$(cat /var/lib/nimbusfab/session.key)
export NIMBUSFAB_WORK_ROOT=/var/lib/nimbusfab/work
nimbusfab-server
```

Visit `http://localhost:8080`; you'll be redirected to `/auth/login`. Sign in
with the admin email + password.

## Configuration (env vars)

| Variable | Default | Purpose |
|----------|---------|---------|
| `NIMBUSFAB_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `NIMBUSFAB_DB_DSN` | `sqlite:./nimbusfab.db` | Inventory DSN. `sqlite:<path>` or `postgres://user:pass@host:5432/dbname?sslmode=require` |
| `NIMBUSFAB_ORG_ID` | `default` | Single-tenant org ID; data is scoped to this org |
| `NIMBUSFAB_AUTH_MODE` | `disabled` | `disabled` (dev — attaches a fixed dev user; do NOT expose publicly) or `local` (cookie + PAT auth) |
| `NIMBUSFAB_SESSION_KEY` | (random per restart) | ≥16-byte secret used to HMAC-sign cookies. **Required for any multi-instance or persistent deployment** — without it, sessions invalidate on every restart and don't cross replicas. |
| `NIMBUSFAB_WORK_ROOT` | `$TMPDIR/nimbusfab-server` | Engine workspace root for per-deployment tofu workdirs |
| `NIMBUSFAB_SECRET_<UPPER_REF>` | — | Per-cloud credentials (see Secrets section) |

## Postgres backend

For multi-instance deployments, use Postgres so the inventory survives
restarts and shares state across replicas.

```bash
# 1. Create the database
createdb nimbusfab
psql nimbusfab -c "CREATE USER nimbusfab WITH PASSWORD 'changeme';"
psql nimbusfab -c "GRANT ALL ON SCHEMA public TO nimbusfab;"

# 2. Point the server at it
export NIMBUSFAB_DB_DSN='postgres://nimbusfab:changeme@db.example.com:5432/nimbusfab?sslmode=require'

# 3. First-startup migrations run automatically on the server boot
nimbusfab-server
```

The same migration files apply to both backends; the server picks the right
flavor based on the DSN scheme.

## Cloud credentials

The server resolves per-target cloud credentials via the same secrets
backend the CLI uses. Two backends ship:

- **Env vars**: `NIMBUSFAB_SECRET_<UPPER_REF>` (e.g. `NIMBUSFAB_SECRET_AWS_DEV`)
  containing JSON of the env vars the cloud provider expects:
  ```bash
  export NIMBUSFAB_SECRET_AWS_DEV='{"AWS_ACCESS_KEY_ID":"AKIA...","AWS_SECRET_ACCESS_KEY":"..."}'
  ```
- **File**: `~/.nimbusfab/secrets/<ref>.json` with the same JSON shape.

In a project YAML, `credentialRef: aws-dev` triggers lookup of
`NIMBUSFAB_SECRET_AWS_DEV` (env first, then file). The resolved env vars
flow into the Tofu runner's per-command env so the AWS / Azure / GCP
provider authenticates.

## Reverse proxy + TLS

`nimbusfab-server` speaks plain HTTP. For production, front it with nginx /
Caddy / similar to terminate TLS. Make sure the `Secure` cookie flag is set
by setting `NIMBUSFAB_COOKIE_SECURE=1` (TODO: wire env var — currently
hardcoded false; Polish Phase 1B may add).

Example Caddyfile:

```
nimbusfab.example.com {
    reverse_proxy localhost:8080
}
```

The HTTP server respects `X-Forwarded-For` and `X-Forwarded-Proto` only
when set by your proxy; if your proxy doesn't set them, browsers see the
internal IP / scheme.

## systemd unit

Sample `/etc/systemd/system/nimbusfab.service`:

```ini
[Unit]
Description=nimbusfab-server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nimbusfab
Group=nimbusfab
WorkingDirectory=/var/lib/nimbusfab
EnvironmentFile=/etc/nimbusfab/server.env
ExecStart=/usr/local/bin/nimbusfab-server
Restart=on-failure
RestartSec=5s

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/nimbusfab

[Install]
WantedBy=multi-user.target
```

`/etc/nimbusfab/server.env` (mode 0640, owner nimbusfab:nimbusfab):

```env
NIMBUSFAB_LISTEN_ADDR=:8080
NIMBUSFAB_DB_DSN=postgres://nimbusfab:secret@localhost:5432/nimbusfab?sslmode=require
NIMBUSFAB_AUTH_MODE=local
NIMBUSFAB_SESSION_KEY=<paste output of `openssl rand -base64 32`>
NIMBUSFAB_WORK_ROOT=/var/lib/nimbusfab/work
NIMBUSFAB_SECRET_AWS_DEV={"AWS_ACCESS_KEY_ID":"...","AWS_SECRET_ACCESS_KEY":"..."}
```

## Bootstrapping users + PATs

The CLI bootstraps both:

```bash
# Create a user (run from the same machine that can reach the inventory DB)
nimbusfab user create --email alice@example.com --password '...' \
  --display-name "Alice" --inventory-dsn "$NIMBUSFAB_DB_DSN"

# Mint a PAT for CI / scripts. The full token prints ONCE — copy it now.
USER_ID=$(nimbusfab user create ... | grep -oE 'usr-[a-f0-9-]+')
nimbusfab pat create --user-id "$USER_ID" --name "github-actions" \
  --inventory-dsn "$NIMBUSFAB_DB_DSN"
# → nfp_<prefix>.<secret>
```

Then use the PAT against the API:

```bash
curl -H "Authorization: Bearer nfp_<prefix>.<secret>" \
  https://nimbusfab.example.com/api/v1/projects
```

## Health checks

- `GET /healthz` — always returns `ok`. Use for liveness.
- `GET /readyz` — pings the inventory DB. Returns 503 on failure. Use
  for readiness gating in Kubernetes / load balancers.

## Upgrading

Migrations run automatically on every server startup. Roll forward by
replacing the binary and restarting; the migration runner is idempotent.

For multi-instance deployments, do a rolling restart: each instance runs
the same migrations (idempotent), so old + new instances can coexist
briefly during the rollout.

## Things deferred to v1.1+

- OIDC SSO (Google / GitHub / Keycloak)
- Background drift cron (currently drift is on-demand)
- Live AWS / Azure / GCP pricing API (snapshots ship today)
- Cost actuals (billing-API polling) — the schema exists; the collector
  daemon does not
- Email / Slack notifications on drift / failed deploys
- PAT management UI page (CLI is the bootstrap path in v1)
- Webhook outbound integrations
- Multi-org provisioning (single-org self-hosted in v1)
