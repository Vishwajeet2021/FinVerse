-- Phase 3: simulated payment rails (FR-PAY-1), scheduled payments (FR-PAY-2)
-- and standing instructions (FR-PAY-3). docs/08-database-design.md section 8.

-- Async rails settle later: settle_at is when the rail "clears" the payment;
-- claimed_until is a lease so several replicas can poll without double-posting
-- (the ledger idempotency key is the second line of defence); settle_attempts
-- bounds retries of a transiently failing ledger call (FR-PAY-8).
ALTER TABLE payments
    ADD COLUMN settle_at        TIMESTAMPTZ,
    ADD COLUMN claimed_until    TIMESTAMPTZ,
    ADD COLUMN settle_attempts  INT NOT NULL DEFAULT 0;
CREATE INDEX idx_payments_due ON payments (settle_at) WHERE status = 'PROCESSING';

CREATE TABLE scheduled_payments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by       VARCHAR(64) NOT NULL,           -- JWT subject (owner for list/cancel)
    from_account_id  UUID NOT NULL,
    to_reference     VARCHAR(64) NOT NULL,
    rail             VARCHAR(8) NOT NULL DEFAULT 'INTERNAL'
                     CHECK (rail IN ('NEFT','RTGS','IMPS','UPI','INTERNAL')),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    run_at           TIMESTAMPTZ NOT NULL,
    status           VARCHAR(16) NOT NULL DEFAULT 'PENDING'
                     CHECK (status IN ('PENDING','EXECUTED','CANCELLED')),
    payment_id       UUID,
    claimed_until    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_scheduled_due ON scheduled_payments (run_at) WHERE status = 'PENDING';
CREATE INDEX idx_scheduled_owner ON scheduled_payments (created_by, created_at DESC);

CREATE TABLE standing_instructions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by       VARCHAR(64) NOT NULL,
    from_account_id  UUID NOT NULL,
    to_reference     VARCHAR(64) NOT NULL,
    rail             VARCHAR(8) NOT NULL DEFAULT 'INTERNAL'
                     CHECK (rail IN ('NEFT','RTGS','IMPS','UPI','INTERNAL')),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    frequency        VARCHAR(16) NOT NULL CHECK (frequency IN ('DAILY','WEEKLY','MONTHLY')),
    next_run_at      TIMESTAMPTZ NOT NULL,
    -- Day-of-month of the first run: MONTHLY clamps to short months but must
    -- return to this day afterwards (31 Jan -> 28 Feb -> 31 Mar), so it cannot
    -- be re-derived from next_run_at.
    anchor_day       INT NOT NULL CHECK (anchor_day BETWEEN 1 AND 31),
    status           VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'
                     CHECK (status IN ('ACTIVE','PAUSED','CANCELLED')),
    last_payment_id  UUID,
    claimed_until    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_standing_due ON standing_instructions (next_run_at) WHERE status = 'ACTIVE';
CREATE INDEX idx_standing_owner ON standing_instructions (created_by, created_at DESC);
