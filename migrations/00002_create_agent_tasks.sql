-- +goose Up
CREATE TABLE IF NOT EXISTS agent_tasks (
    id           BIGSERIAL PRIMARY KEY,
    agent_id     TEXT        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    request_id   TEXT        NOT NULL,
    kind         TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    sent_at      TIMESTAMPTZ,
    done_at      TIMESTAMPTZ,
    ok           BOOLEAN,
    error        TEXT        NOT NULL DEFAULT '',
    retries      INT         NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, request_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_agent_tasks_pending
    ON agent_tasks(agent_id, id)
    WHERE done_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_tasks_created
    ON agent_tasks(created_at);

-- +goose Down
DROP TABLE IF EXISTS agent_tasks;
