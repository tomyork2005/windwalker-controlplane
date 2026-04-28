-- +goose Up
CREATE TABLE IF NOT EXISTS subscriptions (
    id          TEXT        PRIMARY KEY,
    user_id     TEXT        NOT NULL REFERENCES users(id),
    invoice_id  TEXT        NULL REFERENCES invoices(id),
    plan_id     TEXT        NOT NULL REFERENCES plans(id),
    agent_id    TEXT,
    chat_id     BIGINT      NOT NULL,
    status      TEXT        NOT NULL,
    is_trial    BOOLEAN     NOT NULL DEFAULT FALSE,
    start_at    TIMESTAMPTZ NOT NULL,
    end_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id     ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_invoice_id  ON subscriptions(invoice_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_plan_id     ON subscriptions(plan_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_active_end_at
    ON subscriptions(end_at) WHERE status = 'active';
CREATE UNIQUE INDEX uq_subscriptions_one_trial_per_user
    ON subscriptions(user_id) WHERE is_trial = TRUE;

-- +goose Down
DROP TABLE IF EXISTS subscriptions;
