-- Account Service schema, per docs/08-database-design.md §5.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE accounts (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id             UUID NOT NULL,
    account_number          VARCHAR(20) NOT NULL UNIQUE,
    product_type            VARCHAR(16) NOT NULL CHECK (product_type IN ('SAVINGS','CURRENT','JOINT','VIRTUAL')),
    status                  VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','FROZEN','CLOSED')),
    overdraft_limit_minor   BIGINT NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_accounts_customer_id ON accounts (customer_id);

CREATE TABLE account_holders (
    account_id   UUID NOT NULL REFERENCES accounts(id),
    customer_id  UUID NOT NULL,
    role         VARCHAR(16) NOT NULL DEFAULT 'OWNER' CHECK (role IN ('OWNER','JOINT_HOLDER')),
    PRIMARY KEY (account_id, customer_id)
);

-- CQRS read-side projection, rebuilt from Transaction Service's
-- TransactionPosted events (docs/05-system-architecture.md §5).
CREATE TABLE balance_projection (
    account_id               UUID PRIMARY KEY REFERENCES accounts(id),
    ledger_balance_minor     BIGINT NOT NULL DEFAULT 0,
    available_balance_minor  BIGINT NOT NULL DEFAULT 0,
    last_transaction_id      UUID,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    VARCHAR(64) NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;

-- Backs the idempotent TransactionPosted consumer
-- (internal/service/account_service.go's ConsumeTransactionPosted) — Redis
-- Streams consumer groups are at-least-once delivery, so a redelivered event
-- must be a guaranteed no-op, never a double-counted balance.
CREATE TABLE processed_events (
    event_id      UUID PRIMARY KEY,
    processed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
