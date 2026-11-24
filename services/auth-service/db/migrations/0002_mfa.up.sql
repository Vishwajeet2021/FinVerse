-- Second factor (docs/24-admin-mfa.md).
--
-- The TOTP secret is stored ENCRYPTED (AES-256-GCM, key held outside the database), never in
-- clear. mfa_enabled is false while an enrolment is pending: a secret may be present but is not
-- yet trusted, because the user has not proved they can produce a code from it.
--
-- mfa_last_step is the newest 30-second TOTP step already accepted. A code is refused unless its
-- step is greater, so a code that was observed cannot be replayed within its validity window.
ALTER TABLE users
    ADD COLUMN mfa_secret_enc  TEXT,
    ADD COLUMN mfa_enabled     BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN mfa_last_step   BIGINT      NOT NULL DEFAULT 0,
    ADD COLUMN mfa_enrolled_at TIMESTAMPTZ,
    ADD CONSTRAINT users_mfa_enabled_has_secret CHECK (NOT mfa_enabled OR mfa_secret_enc IS NOT NULL);

-- Recovery codes: stored as SHA-256 hashes, single use.
CREATE TABLE mfa_recovery_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash   VARCHAR(128) NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

-- A challenge is what a successful password check returns instead of tokens when a second step
-- is still owed. Only its hash is stored. It carries its own attempt counter so that guessing
-- codes against one challenge is bounded independently of the account lockout.
--   LOGIN  - the user has MFA and must present a code.
--   ENROLL - the user's role requires MFA and none is enrolled yet: the only thing this can do is enrol.
CREATE TABLE mfa_challenges (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   VARCHAR(128) NOT NULL UNIQUE,
    purpose      VARCHAR(8)  NOT NULL CHECK (purpose IN ('LOGIN', 'ENROLL')),
    attempts     SMALLINT    NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mfa_challenges_user_id ON mfa_challenges (user_id);

-- Whether the session that a refresh-token family belongs to passed the second factor. It must
-- survive rotation, or a refresh would silently downgrade an MFA session (and an access token
-- is re-minted from it every 15 minutes).
ALTER TABLE refresh_tokens ADD COLUMN mfa_verified BOOLEAN NOT NULL DEFAULT FALSE;
