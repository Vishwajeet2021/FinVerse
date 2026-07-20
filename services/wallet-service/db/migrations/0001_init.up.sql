-- Wallet Service schema, per docs/08-database-design.md's Wallet Service
-- section and docs/06-domain-driven-design.md §2.4: a Wallet is a
-- lightweight ledger participant, always backed by exactly one funding
-- Account -- "Wallet" here never means a physical/hardware wallet.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         UUID NOT NULL UNIQUE,  -- FR-WAL-1: exactly one wallet per customer; reference to Customer Service, not a FK (separate DB)
    funding_account_id  UUID NOT NULL,          -- the linked Account this wallet tops up from; reference to Account Service, not a FK (separate DB)
    status              VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','CLOSED')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- CQRS read-side projection, rebuilt by consuming Transaction Service's
-- TransactionPosted events (docs/05-system-architecture.md §5) -- never
-- written to directly by a customer-facing write path (top-up/transfer only
-- ever go through the ledger via gRPC; this table is updated solely by the
-- event consumer in internal/repository/postgres/wallet_repo.go).
CREATE TABLE balance_projection (
    wallet_id           UUID PRIMARY KEY REFERENCES wallets(id),
    balance_minor       BIGINT NOT NULL DEFAULT 0,
    last_transaction_id UUID,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Redis Streams consumer groups are at-least-once delivery, so the same
-- TransactionPosted event can be redelivered to this service. This table is
-- what makes the balance_projection update idempotent under redelivery: the
-- event id is inserted (ON CONFLICT DO NOTHING) in the SAME database
-- transaction as the balance update, and a 0-row insert means "already
-- applied, skip" (see ApplyTransactionPostedIdempotent in
-- internal/repository/postgres/wallet_repo.go) -- otherwise a naive
-- `balance += delta` on every delivery would double-count on redelivery, a
-- serious bug for a ledger-backed balance.
CREATE TABLE processed_events (
    event_id     UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    VARCHAR(64) NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;
