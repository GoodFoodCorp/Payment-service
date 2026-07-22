CREATE TABLE IF NOT EXISTS payments (
    id               UUID PRIMARY KEY,
    order_id         UUID        NOT NULL,
    customer_id      UUID        NOT NULL,
    stripe_intent_id TEXT        NOT NULL,
    status           TEXT        NOT NULL,
    amount_cents     BIGINT      NOT NULL CHECK (amount_cents > 0),
    currency         TEXT        NOT NULL DEFAULT 'eur',
    paid_at          TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One payment per order.
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_order ON payments (order_id);
CREATE INDEX IF NOT EXISTS idx_payments_customer ON payments (customer_id);
