ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS mfa_verified;
DROP TABLE IF EXISTS mfa_challenges;
DROP TABLE IF EXISTS mfa_recovery_codes;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_mfa_enabled_has_secret,
    DROP COLUMN IF EXISTS mfa_enrolled_at,
    DROP COLUMN IF EXISTS mfa_last_step,
    DROP COLUMN IF EXISTS mfa_enabled,
    DROP COLUMN IF EXISTS mfa_secret_enc;
