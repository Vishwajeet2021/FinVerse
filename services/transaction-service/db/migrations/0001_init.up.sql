-- Transaction/Ledger Service schema, per docs/08-database-design.md §4.
-- This is the strictest schema in the system: the double-entry invariant is
-- enforced here at the database level, not only in application code
-- (NFR-DATA-3).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE transactions (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key          VARCHAR(128) NOT NULL,
    status                   VARCHAR(16)  NOT NULL DEFAULT 'POSTED'
                              CHECK (status IN ('POSTED','REVERSED')),
    reverses_transaction_id  UUID REFERENCES transactions(id),
    source_service           VARCHAR(64)  NOT NULL,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (source_service, idempotency_key)
);

CREATE TABLE legs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id   UUID NOT NULL REFERENCES transactions(id),
    participant_id   UUID NOT NULL,
    participant_type VARCHAR(32) NOT NULL CHECK (participant_type IN ('ACCOUNT','WALLET','SUSPENSE')),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    direction        CHAR(1) NOT NULL CHECK (direction IN ('D','C')),
    currency         CHAR(3) NOT NULL DEFAULT 'INR'
);
CREATE INDEX idx_legs_transaction_id ON legs (transaction_id);
CREATE INDEX idx_legs_participant_id ON legs (participant_id);

-- Enforces docs/08-database-design.md §4's ledger invariant: for every
-- (transaction_id, currency), sum(debits) - sum(credits) must be exactly 0.
-- Deferred to end-of-transaction (not per-statement) so all of a
-- transaction's legs can be inserted in one batch before the check runs.
CREATE OR REPLACE FUNCTION check_transaction_balances() RETURNS TRIGGER AS $$
DECLARE
    imbalance BIGINT;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN direction = 'D' THEN amount_minor ELSE -amount_minor END), 0)
    INTO imbalance
    FROM legs
    WHERE transaction_id = NEW.transaction_id AND currency = NEW.currency;

    IF imbalance != 0 THEN
        RAISE EXCEPTION 'ledger imbalance for transaction %: debits-credits = % (must be 0)',
            NEW.transaction_id, imbalance;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_legs_balance
    AFTER INSERT ON legs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_transaction_balances();

-- transactions/legs are immutable at the grant level, per
-- docs/08-database-design.md §4 — the only permitted mutation is setting
-- transactions.status = 'REVERSED', which happens through application code,
-- never a bare UPDATE from outside the reversal code path. There's no
-- separate DB role yet (single `finverse` role for all services locally),
-- so this is enforced by application discipline today and tightened to a
-- real per-service role + REVOKE in Phase 4 (docs/14-aws-infrastructure.md §8).

CREATE TABLE outbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    VARCHAR(64) NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;

CREATE TABLE daily_closing_snapshots (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    participant_id UUID NOT NULL,
    balance_minor  BIGINT NOT NULL,
    currency       CHAR(3) NOT NULL DEFAULT 'INR',
    as_of          TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_daily_closing_participant ON daily_closing_snapshots (participant_id, as_of);
