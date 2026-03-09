CREATE TABLE IF NOT EXISTS users (
     id TEXT PRIMARY KEY,
     username TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS plans (
     id TEXT PRIMARY KEY,
     name TEXT NOT NULL,
     region TEXT NOT NULL,
     protocol TEXT NOT NULL,
     money_amount BIGINT NOT NULL,
     money_currency TEXT NOT NULL,
     duration_days BIGINT NOT NULL,
     archived BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS payment_methods (
   id TEXT PRIMARY KEY,
   name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    plan_id TEXT NOT NULL REFERENCES plans(id),
    chat_id TEXT NOT NULL,
    payment_provider TEXT NOT NULL,
    money_amount BIGINT NOT NULL,
    money_currency TEXT NOT NULL,
    status TEXT NOT NULL,
    checkout_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_invoices_user_id ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_plan_id ON invoices(plan_id);
CREATE INDEX IF NOT EXISTS idx_plans_archived ON plans(archived);