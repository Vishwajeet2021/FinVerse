ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_frozen_by_check;
ALTER TABLE accounts DROP COLUMN IF EXISTS frozen_by;
