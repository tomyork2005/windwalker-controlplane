-- +goose Up
CREATE TABLE IF NOT EXISTS user_traffic (
    user_id          TEXT        PRIMARY KEY,
    agent_id         TEXT        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    bytes_up         BIGINT      NOT NULL DEFAULT 0,
    bytes_down       BIGINT      NOT NULL DEFAULT 0,
    last_ip_count    INT         NOT NULL DEFAULT 0,
    last_window_end  TIMESTAMPTZ NOT NULL,
    last_updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS user_traffic;
