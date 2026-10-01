CREATE TABLE IF NOT EXISTS payment_methods (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL,
    provider_payment_method_id TEXT NOT NULL UNIQUE,
    brand TEXT NOT NULL,
    last4 CHAR(4) NOT NULL,
    expiry_month INT NOT NULL CHECK (expiry_month BETWEEN 1 AND 12),
    expiry_year INT NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_payment_methods_customer ON payment_methods (customer_id);