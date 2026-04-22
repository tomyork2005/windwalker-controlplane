-- +goose Up
CREATE TABLE IF NOT EXISTS agent_seq (
                                         agent_id   TEXT    PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    last_seq   BIGINT  NOT NULL DEFAULT 0
    );

-- +goose Down
DROP TABLE IF EXISTS agent_seq;