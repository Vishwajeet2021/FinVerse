-- Who placed the current freeze (docs/23 D-A4). NULL when the account is not frozen.
-- A customer may lift only their own freeze; an admin hold needs an admin to lift it.
ALTER TABLE accounts ADD COLUMN frozen_by TEXT;
ALTER TABLE accounts ADD CONSTRAINT accounts_frozen_by_check
    CHECK (frozen_by IS NULL OR frozen_by IN ('customer', 'admin'));
-- Existing frozen accounts were frozen through the customer endpoint.
UPDATE accounts SET frozen_by = 'customer' WHERE status = 'FROZEN';
