-- Payment Service schema, per docs/08-database-design.md §8. This phase
-- (Phase 2, docs/19-implementation-roadmap.md) only needs the `payments`
-- table -- standing_instructions/scheduled_payments are Phase 3.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE payments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  VARCHAR(128) NOT NULL,
    from_account_id  UUID NOT NULL,
    to_reference     VARCHAR(64) NOT NULL,
    rail             VARCHAR(8) NOT NULL DEFAULT 'INTERNAL'
                     CHECK (rail IN ('NEFT','RTGS','IMPS','UPI','INTERNAL')),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    status           VARCHAR(16) NOT NULL DEFAULT 'INITIATED'
                     CHECK (status IN ('INITIATED','PROCESSING','COMPLETED','FAILED','REVERSED')),
    transaction_id   UUID,
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (from_account_id, idempotency_key)
);

CREATE TABLE outbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    VARCHAR(64) NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;
