-- Initial inventory schema. Targets Postgres; SQLite implementation translates
-- types and drops UUID / JSONB specifics. See the Inventory subsystem spec
-- for column-level finalization; this file is the v0 starting point.

CREATE TABLE IF NOT EXISTS orgs (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY,
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    display_name    TEXT,
    is_local        BOOLEAN NOT NULL DEFAULT false,
    oidc_provider   TEXT,
    oidc_subject    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, email)
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id          UUID PRIMARY KEY,
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  BYTEA NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY,
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    source_uri  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE TABLE IF NOT EXISTS stacks (
    id                 UUID PRIMARY KEY,
    org_id             UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id         UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    state_backend_kind TEXT,
    state_backend_cfg  JSONB,
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS components (
    id          UUID PRIMARY KEY,
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id    UUID NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    ir_json     JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, stack_id, name)
);

CREATE TABLE IF NOT EXISTS compositions (
    id          UUID PRIMARY KEY,
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    ir_json     JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, kind)
);

CREATE TABLE IF NOT EXISTS deployments (
    id                     UUID PRIMARY KEY,
    org_id                 UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stack_id               UUID NOT NULL REFERENCES stacks(id)   ON DELETE CASCADE,
    requested_by_user_id   UUID REFERENCES users(id),
    status                 TEXT NOT NULL,
    partial_failure_policy TEXT,
    started_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at            TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS deployment_targets (
    id              UUID PRIMARY KEY,
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_id   UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    component_name  TEXT NOT NULL,
    cloud           TEXT NOT NULL,
    region          TEXT NOT NULL,
    credential_ref  TEXT NOT NULL,
    workspace_path  TEXT,
    state_backend   JSONB,
    status          TEXT NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS runs (
    id                    UUID PRIMARY KEY,
    org_id                UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    deployment_target_id  UUID NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    kind                  TEXT NOT NULL,
    status                TEXT NOT NULL,
    exit_code             INTEGER,
    started_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at           TIMESTAMPTZ,
    user_id               UUID REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS run_logs (
    run_id     UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    org_id     UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    seq        BIGINT NOT NULL,
    timestamp  TIMESTAMPTZ NOT NULL,
    stream     TEXT NOT NULL,
    body       TEXT NOT NULL,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE IF NOT EXISTS drift_status (
    deployment_target_id UUID PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    org_id               UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    detected_at          TIMESTAMPTZ NOT NULL,
    has_drift            BOOLEAN NOT NULL,
    summary_json         JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS cost_estimates (
    id                UUID PRIMARY KEY,
    org_id            UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    run_id            UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    primitive_id      TEXT NOT NULL,
    currency          TEXT NOT NULL,
    unit_price        NUMERIC(20,8) NOT NULL,
    units             NUMERIC(20,8) NOT NULL,
    unit_of_measure   TEXT NOT NULL,
    subtotal          NUMERIC(20,8) NOT NULL,
    pricing_key_json  JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS cost_actuals (
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    cloud         TEXT NOT NULL,
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    service       TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    region        TEXT NOT NULL,
    amount        NUMERIC(20,8) NOT NULL,
    currency      TEXT NOT NULL,
    tags_json     JSONB,
    tag_set_hash  TEXT NOT NULL,
    PRIMARY KEY (org_id, cloud, period_start, service, resource_id, tag_set_hash)
);

CREATE TABLE IF NOT EXISTS secrets_refs (
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    backend_kind  TEXT NOT NULL,
    backend_cfg   JSONB,
    PRIMARY KEY (org_id, name)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            BIGSERIAL PRIMARY KEY,
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    actor_user_id UUID,
    verb          TEXT NOT NULL,
    target        TEXT,
    payload_json  JSONB,
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_components_stack    ON components (org_id, project_id, stack_id);
CREATE INDEX IF NOT EXISTS idx_deployments_proj    ON deployments (org_id, project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_targets_deployment  ON deployment_targets (org_id, deployment_id);
CREATE INDEX IF NOT EXISTS idx_runs_target         ON runs (org_id, deployment_target_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cost_actuals_period ON cost_actuals (org_id, cloud, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts        ON audit_log (org_id, timestamp DESC);
