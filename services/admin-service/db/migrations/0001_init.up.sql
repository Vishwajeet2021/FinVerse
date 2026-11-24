-- Admin Service schema (docs/23). One table of privileged actions plus the outbox.
--
-- An action row is written BEFORE the downstream call (audit-then-act, D-A2). It starts
-- REQUESTED and moves exactly once to SUCCEEDED or FAILED. A row stuck in REQUESTED means
-- the service died mid-action: that is evidence, so rows are never deleted.
CREATE TABLE admin_actions (
    id             UUID PRIMARY KEY,
    actor_id       UUID NOT NULL,                 -- the administrator's user id (JWT subject)
    action_type    VARCHAR(40) NOT NULL
        CHECK (action_type IN ('ACCOUNT_FREEZE','ACCOUNT_UNFREEZE','FRAUD_CASE_RESOLVE','KYC_REVIEW')),
    target_type    VARCHAR(20) NOT NULL,
    target_id      VARCHAR(64) NOT NULL,
    reason         VARCHAR(500) NOT NULL CHECK (char_length(reason) >= 5),
    params         JSONB NOT NULL DEFAULT '{}',
    status         VARCHAR(12) NOT NULL DEFAULT 'REQUESTED'
        CHECK (status IN ('REQUESTED','SUCCEEDED','FAILED')),
    failure_reason VARCHAR(200),
    trace_id       VARCHAR(64),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ,
    CHECK ((status = 'REQUESTED') = (resolved_at IS NULL))
);
CREATE INDEX idx_admin_actions_created ON admin_actions (created_at DESC);
CREATE INDEX idx_admin_actions_target ON admin_actions (target_type, target_id);

-- Once an action has left REQUESTED it is final, and the request itself is immutable.
CREATE FUNCTION admin_actions_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'admin_actions rows are never deleted';
    END IF;
    IF OLD.status <> 'REQUESTED' THEN
        RAISE EXCEPTION 'admin action % is already final', OLD.id;
    END IF;
    IF NEW.actor_id <> OLD.actor_id OR NEW.action_type <> OLD.action_type OR NEW.target_type <> OLD.target_type
       OR NEW.target_id <> OLD.target_id OR NEW.reason <> OLD.reason OR NEW.params <> OLD.params
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'the request part of an admin action is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER admin_actions_guard BEFORE UPDATE OR DELETE ON admin_actions
    FOR EACH ROW EXECUTE FUNCTION admin_actions_guard();

CREATE TABLE outbox (
    id            UUID PRIMARY KEY,
    event_type    VARCHAR(64) NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ,
    traceparent   VARCHAR(64)
);
CREATE INDEX idx_outbox_unpublished ON outbox (created_at) WHERE published_at IS NULL;
