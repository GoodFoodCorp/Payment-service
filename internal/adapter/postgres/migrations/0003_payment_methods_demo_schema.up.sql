-- Aligne payment_methods sur le mode démo (cardholder_name, exp_month/exp_year,
-- jamais de numéro complet stocké). La table peut déjà exister sur
-- l'environnement déployé avec le schéma 0002 : on la convertit sur place,
-- sans perdre de lignes ni supprimer de colonne.
ALTER TABLE payment_methods ADD COLUMN IF NOT EXISTS cardholder_name TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payment_methods' AND column_name = 'expiry_month') THEN
        ALTER TABLE payment_methods RENAME COLUMN expiry_month TO exp_month;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payment_methods' AND column_name = 'expiry_year') THEN
        ALTER TABLE payment_methods RENAME COLUMN expiry_year TO exp_year;
    END IF;
END $$;

ALTER TABLE payment_methods ALTER COLUMN last4 TYPE TEXT;

-- Plus renseignée par le code : on la garde (données existantes) mais optionnelle.
ALTER TABLE payment_methods ALTER COLUMN provider_payment_method_id DROP NOT NULL;
