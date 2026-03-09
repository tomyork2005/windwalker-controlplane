CREATE TABLE outbox_events (
    id           uuid PRIMARY KEY,
    event_type   text        NOT NULL,
    payload      jsonb       NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0,
    error        text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz NULL
);

CREATE INDEX outbox_events_unprocessed_idx
    ON outbox_events (created_at)
    WHERE processed_at IS NULL;


