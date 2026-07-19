-- Customer Service schema, per docs/08-database-design.md §6.
-- Note: kyc_documents_meta.storage_key replaces the doc's `s3_key` column
-- name — Phase 1 stores documents on local disk behind the Storage
-- interface (internal/service/interfaces.go), not S3 yet
-- (docs/19-implementation-roadmap.md Phase 4), so "s3_key" would be a
-- misleading name for what's actually a local filesystem path today. Same
-- column, same purpose, tracked back into docs/08 per
-- docs/00-master-ai-instructions.md §3.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL UNIQUE,
    full_name       VARCHAR(128) NOT NULL,
    dob             DATE NOT NULL,
    pan_encrypted   BYTEA NOT NULL,
    pan_masked      VARCHAR(16) NOT NULL,
    kyc_tier        SMALLINT NOT NULL DEFAULT 0 CHECK (kyc_tier IN (0,1,2)),
    kyc_status      VARCHAR(16) NOT NULL DEFAULT 'PENDING'
                    CHECK (kyc_status IN ('PENDING','UNDER_REVIEW','APPROVED','REJECTED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE kyc_documents_meta (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL REFERENCES customers(id),
    doc_type     VARCHAR(32) NOT NULL,
    storage_key  VARCHAR(256) NOT NULL,
    uploaded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kyc_documents_meta_customer_id ON kyc_documents_meta (customer_id);

CREATE TABLE beneficiaries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL REFERENCES customers(id),
    name         VARCHAR(128) NOT NULL,
    account_ref  VARCHAR(64) NOT NULL,
    verified     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_beneficiaries_customer_id ON beneficiaries (customer_id);
