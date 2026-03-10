-- +goose Up
CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    invoice_id TEXT NOT NULL REFERENCES invoices(id),
    plan_id TEXT NOT NULL REFERENCES plans(id),
    agent_id TEXT, -- will be added when choosing agent
    status TEXT NOT NULL,
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_invoice_id ON subscriptions(invoice_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_plan_id ON subscriptions(plan_id);

-- +goose Down
DROP TABLE IF EXISTS subscriptions;