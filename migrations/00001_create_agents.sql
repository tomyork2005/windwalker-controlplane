-- +goose Up
CREATE TABLE IF NOT EXISTS agents (
    id                 TEXT PRIMARY KEY,
    instance_id        TEXT NOT NULL,
    region             TEXT NOT NULL,
    version            TEXT NOT NULL DEFAULT '',
    driver_types       TEXT[] NOT NULL,
    uptime_seconds     BIGINT NOT NULL DEFAULT 0,
    last_seen_at       TIMESTAMPTZ,
    stats_deadline_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agents_region ON agents(region);
CREATE INDEX IF NOT EXISTS idx_agents_driver_types ON agents USING GIN (driver_types);
CREATE INDEX IF NOT EXISTS idx_agents_stats_deadline ON agents(stats_deadline_at);

-- +goose Down
DROP TABLE IF EXISTS agents;
