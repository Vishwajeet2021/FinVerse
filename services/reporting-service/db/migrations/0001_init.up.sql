-- Reporting Service schema (docs/23). Metadata only: the files themselves live in storage.
CREATE TABLE report_requests (
    id            UUID PRIMARY KEY,
    requester_id  UUID NOT NULL,
    kind          VARCHAR(20) NOT NULL CHECK (kind IN ('STATEMENT','FRAUD_SUMMARY')),
    account_id    UUID,
    period_from   DATE,
    period_to     DATE,
    status        VARCHAR(10) NOT NULL DEFAULT 'QUEUED'
        CHECK (status IN ('QUEUED','RUNNING','READY','FAILED','EXPIRED')),
    row_count     INTEGER NOT NULL DEFAULT 0,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    sha256        CHAR(64),
    file_key      VARCHAR(80),
    failure_code  VARCHAR(40),
    attempts      INTEGER NOT NULL DEFAULT 0,
    lease_until   TIMESTAMPTZ,
    snapshot      JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    CHECK (kind <> 'STATEMENT' OR (account_id IS NOT NULL AND period_from IS NOT NULL AND period_to IS NOT NULL AND period_from <= period_to)),
    CHECK (status <> 'READY' OR (file_key IS NOT NULL AND sha256 IS NOT NULL))
);
CREATE INDEX idx_reports_requester ON report_requests (requester_id, created_at DESC);
-- The worker's claim query scans only unfinished work.
CREATE INDEX idx_reports_work ON report_requests (created_at) WHERE status IN ('QUEUED','RUNNING');
CREATE INDEX idx_reports_expiry ON report_requests (expires_at) WHERE status = 'READY';
