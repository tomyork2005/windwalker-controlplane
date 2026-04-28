-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id          TEXT   PRIMARY KEY,
    username    TEXT   NOT NULL,
    telegram_id BIGINT NULL
);
CREATE UNIQUE INDEX idx_users_telegram_id
    ON users(telegram_id) WHERE telegram_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS plans (
    id              TEXT    PRIMARY KEY,
    name            TEXT    NOT NULL,
    region          TEXT    NOT NULL,
    protocol        TEXT    NOT NULL,
    money_amount    BIGINT  NOT NULL,
    money_currency  TEXT    NOT NULL,
    duration_days   BIGINT  NOT NULL,
    archived        BOOLEAN NOT NULL DEFAULT FALSE,
    is_trial        BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS payment_methods (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
    id TEXT PRIMARY KEY,
    provider_order_id TEXT NULL,
    user_id TEXT NOT NULL REFERENCES users(id),
    plan_id TEXT NOT NULL REFERENCES plans(id),
    chat_id BIGINT NOT NULL,
    payment_provider TEXT NOT NULL,
    money_amount BIGINT NOT NULL,
    money_currency TEXT NOT NULL,
    status TEXT NOT NULL,
    checkout_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    renews_subscription_id TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_invoices_user_id ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_plan_id ON invoices(plan_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_provider_order
    ON invoices(payment_provider, provider_order_id)
    WHERE provider_order_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_plans_archived ON plans(archived);
CREATE INDEX idx_plans_is_trial ON plans(is_trial) WHERE is_trial = TRUE;

-- +goose Down
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS payment_methods;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS users;
