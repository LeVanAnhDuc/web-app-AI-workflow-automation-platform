-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE workspaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'owner',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX users_workspace_idx ON users (workspace_id);

CREATE TABLE workflows (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name              TEXT        NOT NULL,
    active            BOOLEAN     NOT NULL DEFAULT false,
    active_version_id UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workflows_workspace_idx ON workflows (workspace_id, updated_at DESC);

CREATE TABLE workflow_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID        NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    version     INT         NOT NULL,
    graph       JSONB       NOT NULL,
    created_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, version)
);

ALTER TABLE workflows
    ADD CONSTRAINT workflows_active_version_fk
        FOREIGN KEY (active_version_id) REFERENCES workflow_versions (id) ON DELETE SET NULL;

CREATE TABLE executions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    workflow_id         UUID        NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    workflow_version_id UUID        NOT NULL REFERENCES workflow_versions (id) ON DELETE CASCADE,
    status              TEXT        NOT NULL,
    trigger_type        TEXT        NOT NULL,
    trigger_data        JSONB,
    error               JSONB,
    resume_from_node    TEXT,
    cancel_requested    BOOLEAN     NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ
);
CREATE INDEX executions_workspace_idx ON executions (workspace_id, created_at DESC);
CREATE INDEX executions_workflow_idx ON executions (workflow_id, created_at DESC);
CREATE INDEX executions_status_idx ON executions (workspace_id, status, created_at DESC);

CREATE TABLE node_executions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES executions (id) ON DELETE CASCADE,
    node_id      TEXT NOT NULL,
    node_name    TEXT NOT NULL,
    node_type    TEXT NOT NULL,
    status       TEXT NOT NULL,
    attempt      INT  NOT NULL DEFAULT 1,
    input        JSONB,
    output       JSONB,
    error        JSONB,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    UNIQUE (execution_id, node_id)
);
CREATE INDEX node_executions_exec_idx ON node_executions (execution_id, started_at);

CREATE TABLE webhooks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    workflow_id  UUID        NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    node_id      TEXT        NOT NULL,
    path         TEXT        NOT NULL UNIQUE,
    method       TEXT        NOT NULL DEFAULT 'POST',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX webhooks_workflow_idx ON webhooks (workflow_id);

CREATE TABLE schedules (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    workflow_id  UUID        NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    node_id      TEXT        NOT NULL,
    cron         TEXT        NOT NULL,
    timezone     TEXT        NOT NULL DEFAULT 'UTC',
    next_run_at  TIMESTAMPTZ NOT NULL,
    last_run_at  TIMESTAMPTZ,
    UNIQUE (workflow_id, node_id)
);
CREATE INDEX schedules_due_idx ON schedules (next_run_at);

CREATE TABLE credentials (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    type           TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    data_encrypted BYTEA       NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX credentials_workspace_idx ON credentials (workspace_id, type);

CREATE TABLE jobs (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'pending',
    attempt      INT         NOT NULL DEFAULT 0,
    max_attempts INT         NOT NULL DEFAULT 3,
    run_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at    TIMESTAMPTZ,
    locked_by    TEXT,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX jobs_claim_idx ON jobs (run_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS schedules;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS node_executions;
DROP TABLE IF EXISTS executions;
ALTER TABLE IF EXISTS workflows DROP CONSTRAINT IF EXISTS workflows_active_version_fk;
DROP TABLE IF EXISTS workflow_versions;
DROP TABLE IF EXISTS workflows;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS workspaces;
