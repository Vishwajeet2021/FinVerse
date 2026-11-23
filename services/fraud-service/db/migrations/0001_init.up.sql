-- Fraud Service schema (docs/07-microservice-specifications.md §10). Velocity
-- counters live in Redis only (hot path); Postgres holds the durable cases an
-- analyst works. Rule configuration is environment-driven in Phase 3.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE fraud_cases (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID NOT NULL,
    payment_id   UUID,
    amount_minor BIGINT NOT NULL,
    verdict      VARCHAR(8) NOT NULL CHECK (verdict IN ('REVIEW','DENY')),
    rule         VARCHAR(32) NOT NULL,
    reason       TEXT NOT NULL,
    risk_score   INT NOT NULL,
    status       VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED')),
    resolution   VARCHAR(16),
    resolved_by  VARCHAR(64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);
CREATE INDEX idx_fraud_cases_status ON fraud_cases (status, created_at DESC);

CREATE TABLE outbox (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(64) NOT NULL,
    payload      JSONB NOT NULL,
    traceparent  VARCHAR(64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;
