-- Refunds and the provider intent lookup used by webhooks.
ALTER TABLE payments ADD COLUMN IF NOT EXISTS refunded_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_intent ON payments (stripe_intent_id);
-- Customer history is read most recent first.
CREATE INDEX IF NOT EXISTS idx_payments_customer_created ON payments (customer_id, created_at DESC);

-- Provider customer each user's saved cards are attached to.
CREATE TABLE IF NOT EXISTS payment_customers (
    customer_id          UUID PRIMARY KEY,
    provider_customer_id TEXT        NOT NULL UNIQUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Transactional outbox: events are written with the state change they
-- describe and relayed to RabbitMQ afterwards (at-least-once).
CREATE TABLE IF NOT EXISTS outbox_events (
    id           UUID PRIMARY KEY,
    routing_key  TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    attempts     INT         NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished ON outbox_events (occurred_at) WHERE published_at IS NULL;

-- At most one default card per customer (keep the most recent one).
UPDATE payment_methods pm SET is_default = FALSE
WHERE is_default AND id <> (
    SELECT x.id FROM payment_methods x
    WHERE x.customer_id = pm.customer_id AND x.is_default
    ORDER BY x.created_at DESC LIMIT 1);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_methods_one_default
    ON payment_methods (customer_id) WHERE is_default;
