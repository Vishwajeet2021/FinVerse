-- Notification Service schema (docs/07-microservice-specifications.md §11).
-- The spec names MongoDB for this service; Phase 3 uses Postgres instead
-- (see docs/20-ai-dlc-phase3.md, decision D-3) — the delivery log here is
-- tabular and idempotency is a UNIQUE constraint, both of which Postgres
-- does natively, and it avoids a third datastore for one service.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE notifications (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- One notification per source event: the idempotency key that makes
    -- at-least-once delivery from the bus safe (docs/09-api-standards.md §4.1).
    event_id      UUID NOT NULL UNIQUE,
    event_type    VARCHAR(64) NOT NULL,
    recipient_ref VARCHAR(128) NOT NULL,
    channel       VARCHAR(16) NOT NULL DEFAULT 'EMAIL',
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','SENT')),
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT,
    trace_id      VARCHAR(64),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at       TIMESTAMPTZ
);
CREATE INDEX idx_notifications_recipient ON notifications (recipient_ref, created_at DESC);
