-- Auth Service schema, per docs/08-database-design.md §7.
-- Note: `role` is a minimal, documented addition to the schema sketched in
-- docs/08 — the JWT claims (docs/10-authentication-authorization.md §2.1)
-- need a persisted source for a user's role, and the doc's user table didn't
-- enumerate it explicitly. Tracked back into docs/08 in the same change set
-- that adds this migration, per docs/00-master-ai-instructions.md §3.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               VARCHAR(256) NOT NULL UNIQUE,
    phone               VARCHAR(20) UNIQUE,
    password_hash       VARCHAR(256) NOT NULL,
    role                VARCHAR(16) NOT NULL DEFAULT 'customer'
                        CHECK (role IN ('customer','support','fraud_analyst','auditor','admin','super_admin')),
    email_verified      BOOLEAN NOT NULL DEFAULT FALSE,
    status              VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE','LOCKED','DISABLED')),
    failed_login_count  SMALLINT NOT NULL DEFAULT 0,
    locked_until        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    token_hash   VARCHAR(128) NOT NULL UNIQUE,
    family_id    UUID NOT NULL,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_family_id ON refresh_tokens (family_id);

CREATE TABLE email_verification_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    token_hash   VARCHAR(128) NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_email_verification_tokens_user_id ON email_verification_tokens (user_id);

CREATE TABLE password_reset_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    token_hash   VARCHAR(128) NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_password_reset_tokens_user_id ON password_reset_tokens (user_id);
