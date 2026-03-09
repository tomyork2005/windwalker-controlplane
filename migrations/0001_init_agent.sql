CREATE TABLE IF NOT EXISTS agents (
      id              TEXT PRIMARY KEY,
      instance_id     TEXT NOT NULL,
      region          TEXT NOT NULL,
      version         TEXT NOT NULL,
      driver_types    TEXT[] NOT NULL,
      last_seen_at    TIMESTAMPTZ,                   -- heartbeat
      hb_deadline_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agents_region ON agents(region);
CREATE INDEX IF NOT EXISTS idx_agents_driver_types ON agents USING GIN (driver_types);
CREATE INDEX IF NOT EXISTS idx_agents_hb_deadline ON agents(hb_deadline_at DESC, id);

-- hot point (minimum latency)
CREATE TABLE IF NOT EXISTS agent_seq (
    agent_id  TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    last_seq  BIGINT NOT NULL DEFAULT 0
);

-- fast candidate choosing + hot point
CREATE TABLE IF NOT EXISTS agent_load (
    agent_id    TEXT REFERENCES agents(id) ON DELETE CASCADE,
    region      TEXT NOT NULL,
    driver_type TEXT NOT NULL,
    users_cnt   INT  NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, driver_type)
);

CREATE INDEX IF NOT EXISTS idx_agent_load_pick
    ON agent_load(region, driver_type, users_cnt, agent_id);
