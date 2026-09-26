-- Saved payment methods (demo mode: no real card data ever stored — only the
-- brand and last 4 digits, derived once from the number the customer typed).
CREATE TABLE IF NOT EXISTS payment_methods (
    id               UUID PRIMARY KEY,
    customer_id      UUID        NOT NULL,
    cardholder_name  TEXT        NOT NULL,
    brand            TEXT        NOT NULL,
    last4            TEXT        NOT NULL,
    exp_month        INT         NOT NULL,
    exp_year         INT         NOT NULL,
    is_default       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_payment_methods_customer ON payment_methods (customer_id);
