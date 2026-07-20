DROP TABLE IF EXISTS daily_closing_snapshots;
DROP TABLE IF EXISTS outbox;
DROP TRIGGER IF EXISTS trg_legs_balance ON legs;
DROP FUNCTION IF EXISTS check_transaction_balances();
DROP TABLE IF EXISTS legs;
DROP TABLE IF EXISTS transactions;
