DROP INDEX IF EXISTS idx_payment_methods_one_default;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS payment_customers;
DROP INDEX IF EXISTS idx_payments_customer_created;
DROP INDEX IF EXISTS idx_payments_intent;
ALTER TABLE payments DROP COLUMN IF EXISTS refunded_at;
