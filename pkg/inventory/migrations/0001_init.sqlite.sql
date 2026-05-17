-- SQLite flavor of 0001_init. JSONB -> TEXT, UUID -> TEXT, TIMESTAMPTZ -> TEXT
-- (ISO-8601), NUMERIC -> REAL. Phase 1 implements only the subset of tables
-- used by Plan/Apply/Destroy/Drift; the rest are created for forward-compat.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS orgs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    display_name    TEXT,
    is_local        INTEGER NOT NULL DEFAULT 0,
    oidc_provider   TEXT,
    oidc_subject    TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (org_id, email)
);

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    source_uri  TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (org_id, name)
);

CREATE TABLE IF NOT EXISTS stacks (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id         TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    state_backend_kind TEXT,
    state_backend_cfg  TEXT,
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS components (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id    TEXT NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    ir_json     TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, stack_id, name)
);

CREATE TABLE IF NOT EXISTS compositions (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    ir_json     TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, kind)
);

CREATE TABLE IF NOT EXISTS deployments (
    id                     TEXT PRIMARY KEY,
    org_id                 TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id             TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id               TEXT NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    requested_by_user_id   TEXT REFERENCES users(id),
    status                 TEXT NOT NULL,
    partial_failure_policy TEXT,
    started_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at            TEXT
);

CREATE TABLE IF NOT EXISTS deployment_targets (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_id   TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    component_name  TEXT NOT NULL,
    cloud           TEXT NOT NULL,
    region          TEXT NOT NULL,
    credential_ref  TEXT NOT NULL,
    workspace_path  TEXT,
    plan_file       TEXT,
    state_backend   TEXT,
    status          TEXT NOT NULL,
    started_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at     TEXT
);

CREATE TABLE IF NOT EXISTS runs (
    id                    TEXT PRIMARY KEY,
    org_id                TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_target_id  TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    kind                  TEXT NOT NULL,
    status                TEXT NOT NULL,
    exit_code             INTEGER,
    started_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at           TEXT,
    user_id               TEXT REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS drift_status (
    deployment_target_id TEXT PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    org_id               TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    detected_at          TEXT NOT NULL,
    has_drift            INTEGER NOT NULL,
    summary_json         TEXT NOT NULL
);

-- Parity additions (Postgres has these in 0001_init.sql; SQLite catches up
-- here so both backends ship the same schema surface). Repos for these
-- tables land with their owning phases (RunLogs / CostActuals / SecretsRefs
-- / ApiTokens are still notwired stubs at the repo layer).

CREATE TABLE IF NOT EXISTS api_tokens (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  BLOB NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at TEXT
);

CREATE TABLE IF NOT EXISTS run_logs (
    run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    timestamp  TEXT NOT NULL,
    stream     TEXT NOT NULL,
    body       TEXT NOT NULL,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE IF NOT EXISTS cost_estimates (
    id                TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    run_id            TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    primitive_id      TEXT NOT NULL,
    currency          TEXT NOT NULL,
    unit_price        REAL NOT NULL,
    units             REAL NOT NULL,
    unit_of_measure   TEXT NOT NULL,
    subtotal          REAL NOT NULL,
    pricing_key_json  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cost_actuals (
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    cloud         TEXT NOT NULL,
    period_start  TEXT NOT NULL,
    period_end    TEXT NOT NULL,
    service       TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    region        TEXT NOT NULL,
    amount        REAL NOT NULL,
    currency      TEXT NOT NULL,
    tags_json     TEXT,
    tag_set_hash  TEXT NOT NULL,
    PRIMARY KEY (org_id, cloud, period_start, service, resource_id, tag_set_hash)
);

CREATE TABLE IF NOT EXISTS secrets_refs (
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    backend_kind  TEXT NOT NULL,
    backend_cfg   TEXT,
    PRIMARY KEY (org_id, name)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    actor_user_id TEXT,
    verb          TEXT NOT NULL,
    target        TEXT,
    payload_json  TEXT,
    timestamp     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_deployments_proj    ON deployments (org_id, project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_targets_deployment  ON deployment_targets (org_id, deployment_id);
CREATE INDEX IF NOT EXISTS idx_runs_target         ON runs (org_id, deployment_target_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cost_actuals_period ON cost_actuals (org_id, cloud, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts        ON audit_log (org_id, timestamp DESC);
