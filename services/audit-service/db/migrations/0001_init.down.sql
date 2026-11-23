DROP TRIGGER IF EXISTS audit_records_immutable ON audit_records;
DROP FUNCTION IF EXISTS audit_records_reject_change();
DROP TABLE IF EXISTS audit_records;
