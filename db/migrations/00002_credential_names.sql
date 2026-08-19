-- +goose Up
-- A credential's name is how a person picks it out of a dropdown, so two with
-- the same name in one workspace is a usability bug rather than a data one.
-- Enforced per workspace, not globally: two tenants may both call one "Slack".
CREATE UNIQUE INDEX credentials_workspace_name_idx
    ON credentials (workspace_id, lower(name));

-- The node drawer's picker asks for one workspace's credentials of one type,
-- which is the only hot read on this table.
CREATE INDEX credentials_workspace_type_created_idx
    ON credentials (workspace_id, type, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS credentials_workspace_type_created_idx;
DROP INDEX IF EXISTS credentials_workspace_name_idx;
