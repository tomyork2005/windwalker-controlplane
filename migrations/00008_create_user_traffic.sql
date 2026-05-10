-- +goose Up
CREATE TABLE IF NOT EXISTS user_traffic (
    id          BIGSERIAL   PRIMARY KEY,
    agent_id    TEXT        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     TEXT        NOT NULL,
    window_end  TIMESTAMPTZ NOT NULL,
    bytes_up    BIGINT      NOT NULL DEFAULT 0,
    bytes_down  BIGINT      NOT NULL DEFAULT 0,
    ip_count    INT         NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_traffic_user
    ON user_traffic(user_id, window_end DESC);
CREATE INDEX IF NOT EXISTS idx_user_traffic_agent_window
    ON user_traffic(agent_id, window_end DESC);

-- +goose Down
DROP TABLE IF EXISTS user_traffic;
