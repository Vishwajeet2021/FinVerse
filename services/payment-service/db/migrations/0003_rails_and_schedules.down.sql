DROP TABLE IF EXISTS standing_instructions;
DROP TABLE IF EXISTS scheduled_payments;
DROP INDEX IF EXISTS idx_payments_due;
ALTER TABLE payments DROP COLUMN IF EXISTS settle_attempts, DROP COLUMN IF EXISTS claimed_until, DROP COLUMN IF EXISTS settle_at;
