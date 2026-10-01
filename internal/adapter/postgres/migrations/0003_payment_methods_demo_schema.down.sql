-- Retour au schéma 0002. Les lignes sans provider_payment_method_id ne
-- peuvent pas redevenir NOT NULL : on les complète avec leur id.
UPDATE payment_methods SET provider_payment_method_id = id::text WHERE provider_payment_method_id IS NULL;
ALTER TABLE payment_methods ALTER COLUMN provider_payment_method_id SET NOT NULL;
ALTER TABLE payment_methods ALTER COLUMN last4 TYPE CHAR(4);
ALTER TABLE payment_methods RENAME COLUMN exp_month TO expiry_month;
ALTER TABLE payment_methods RENAME COLUMN exp_year TO expiry_year;
ALTER TABLE payment_methods DROP COLUMN IF EXISTS cardholder_name;
