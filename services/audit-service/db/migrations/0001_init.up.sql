-- Audit Service schema (docs/07-microservice-specifications.md section 12).
-- Append-only is enforced IN THE DATABASE, not by convention: a trigger rejects
-- any UPDATE, DELETE or TRUNCATE, so even a bug in this service (or someone with
-- the app credentials) cannot rewrite history. Production additionally revokes
-- UPDATE/DELETE from the application role (docs/14-aws-infrastructure.md); the
-- local compose setup uses one superuser role, which is why the trigger matters.
-- Monthly partitioning from the spec is deferred (see docs/20-ai-dlc-phase3.md).
CREATE TABLE audit_records (
    id             BIGSERIAL PRIMARY KEY,
    event_id       UUID NOT NULL UNIQUE,   -- idempotency: one record per bus event
    event_type     VARCHAR(64) NOT NULL,
    source_service VARCHAR(64) NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL,
    trace_id       VARCHAR(64),
    payload        JSONB NOT NULL,
    recorded_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_type_time ON audit_records (event_type, occurred_at DESC);
CREATE INDEX idx_audit_time ON audit_records (occurred_at DESC);

CREATE FUNCTION audit_records_reject_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_records is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_records_immutable
    BEFORE UPDATE OR DELETE ON audit_records
    FOR EACH ROW EXECUTE FUNCTION audit_records_reject_change();

CREATE TRIGGER audit_records_no_truncate
    BEFORE TRUNCATE ON audit_records
    FOR EACH STATEMENT EXECUTE FUNCTION audit_records_reject_change();
