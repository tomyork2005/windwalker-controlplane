-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN creds TEXT,
    ADD COLUMN creds_ready_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS creds_ready_at,
    DROP COLUMN IF EXISTS creds;
