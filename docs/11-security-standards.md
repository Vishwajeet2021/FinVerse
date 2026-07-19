# 11 — Security Standards

## 1. Data classification

| Class | Examples | Handling |
|---|---|---|
| **Restricted** | Password hashes, TOTP secrets, PAN, full card numbers, refresh tokens | Encrypted at rest, never logged even masked-partially in the case of secrets, access itself is audited |
| **Confidential** | KYC documents, address, transaction amounts, balances | Encrypted at rest, masked in logs, access-controlled by ownership |
| **Internal** | Risk scores, fraud rules, admin notes | Not customer-visible, standard internal access controls |
| **Public** | Marketing pages, published API docs | No special handling |

Every new field added to any schema must be assigned a class in the owning service's docs before it ships (`00-master-ai-instructions.md §4.2`).

## 2. OWASP Top 10 — how FinVerse addresses each

| Risk | Mitigation |
|---|---|
| Broken Access Control | RBAC + row-level ownership checks on every handler (`10-authentication-authorization.md §3.3`); deny-by-default |
| Cryptographic Failures | TLS 1.2+ everywhere; AES-256-GCM field-level encryption for Restricted/Confidential data (§4); Argon2id for passwords |
| Injection | GORM parameterized queries only — no string-concatenated SQL, ever; input validation at the API boundary (§5) |
| Insecure Design | Threat-model review at the point each service spec is written (`07-microservice-specifications.md`), not bolted on after |
| Security Misconfiguration | IaC (Terraform) for all infra so config is reviewable and reproducible; no manual console changes in staging/prod |
| Vulnerable & Outdated Components | Automated dependency scanning in CI (`15-cicd-pipeline.md §Security Gates`) — Go modules and npm packages both scanned |
| Identification & Auth Failures | See `10-authentication-authorization.md` in full — MFA, rotation, lockout, ownership checks |
| Software & Data Integrity Failures | Signed container images, pinned dependency versions, CI-verified build provenance before ECS deploy |
| Security Logging & Monitoring Failures | Every auth failure, authz denial, and fraud trigger is logged and alertable (`16-observability.md §Security Alerts`) |
| Server-Side Request Forgery | No service accepts a user-supplied URL to fetch server-side; outbound calls are to a fixed, code-defined allowlist of internal services only |

## 3. Secrets management

- No secret (DB password, JWT signing key, third-party API key) is ever committed to source control, baked into a container image, or written to a log line.
- Local development: `.env` files, gitignored, with a checked-in `.env.example` containing only placeholder values.
- Staging/prod: AWS Secrets Manager for credentials that rotate (DB passwords, JWT signing keys) and Parameter Store (SecureString) for static configuration secrets. Services fetch secrets at startup via their ECS task role — never via a hardcoded IAM user key.
- JWT signing uses an asymmetric key pair (RS256); the private key lives only in Auth Service's Secrets Manager entry, the public key/JWKS is what everyone else fetches — this means no other service can forge a token even if fully compromised.
- Secret rotation: DB credentials rotate automatically via Secrets Manager's rotation Lambda; JWT signing keys rotate on a documented schedule with an overlap window (old key still validates until all outstanding tokens expire).

## 4. Field-level encryption

- **What's encrypted at the field level** (beyond disk-level RDS encryption, which covers everything): PAN, full card number/PIN references, TOTP secrets — i.e., data where "the disk was encrypted" isn't enough protection against a compromised DB credential or a careless query result being logged.
- **How**: AES-256-GCM, application-side, using a data key sourced from Secrets Manager (envelope encryption — a KMS-managed key encrypts the data key, which encrypts the field). Encrypt/decrypt is a small shared Go package (`shared/crypto`) so every service does it identically rather than reinventing it per service.
- **Masking for display**: every encrypted field has a paired masked column (e.g. `pan_masked`) computed once at write time, used for every UI/log/audit display need so decryption only ever happens on the narrow, audited path that genuinely needs the plaintext (`08-database-design.md §6`).

## 5. Input validation

- Every REST/gRPC handler validates its input against an explicit schema (Go struct tags + a validation library) before any business logic runs — reject early, reject with a clear `400`/`INVALID_ARGUMENT`.
- Numeric monetary inputs are validated as positive integers in minor units; any float-shaped monetary input from a client is rejected outright, not coerced.
- All user-supplied strings destined for display are output-encoded by the frontend framework (SvelteKit's default escaping) — no `{@html}` of user content without explicit sanitization.
- File uploads (KYC documents) are validated by content-type and size limit server-side (never trust the client-reported MIME type alone — verify magic bytes), and stored in S3 with a randomized key, never the original filename.

## 6. Rate limiting

Two layers, intentionally different granularity (`09-api-standards.md §2.9`):
1. **API Gateway**: coarse, per-IP and per-token, protects against volumetric abuse/basic scripted attacks.
2. **Fraud Service velocity rules**: business-aware, per-customer, protects against abuse that looks like normal traffic volume but is anomalous *for that customer* (e.g. 20 transfers in a minute from an account that normally does 2/day).

Login endpoints specifically get a tighter, IP+account-combined limit to blunt credential-stuffing attempts, independent of the general API rate limit.

## 7. Network security

- All services run in private subnets; only the ALB (itself only reachable via API Gateway's VPC Link) has any inbound path from outside the VPC.
- Security groups are least-privilege and service-specific: e.g. the Transaction Service's RDS security group only accepts inbound from the Transaction Service's own ECS security group, not from "the VPC" broadly.
- No database, cache, or search cluster has a public endpoint, in any environment, ever.

## 8. Logging and PII redaction

- Structured (JSON) logs never contain: raw passwords, raw tokens, full PAN, full card numbers, or full KYC document content.
- A shared logging middleware (`shared/logging`) redacts known-sensitive field names before a log line is emitted, as a backstop against a developer forgetting to mask manually.
- Audit records (`08-database-design.md §11`) are the one place "before/after state" is intentionally captured in detail — even there, Restricted-class fields are stored masked, never as recoverable plaintext, because audit records are never deleted and thus represent the longest-lived exposure surface if compromised.

## 9. Container and dependency security

- Base images are minimal (distroless or Alpine) and pinned to a specific digest, not a floating tag.
- CI runs a dependency vulnerability scan (`govulncheck` for Go, `npm audit`/equivalent for the frontend) and a container image scan before any image is allowed to push to ECR; a Critical/High finding blocks the pipeline (`15-cicd-pipeline.md §Security Gates`).
- Services run as a non-root container user; no service's container has write access to its own filesystem beyond an explicit temp path.

## 10. Incident response (lightweight, appropriate to project scale)

- A documented runbook per critical scenario (compromised refresh token family, suspected data exposure, ledger imbalance detected) lives in `docs/runbooks/` (created as each scenario becomes realistic to defend against, starting Phase 3).
- The emergency `jti` denylist (`10-authentication-authorization.md §2.3`) and account lockout are the two fastest levers available without a deploy.
- Any real security finding during development is fixed before merge, never deferred with a "TODO: security" comment (`00-master-ai-instructions.md §5`).

## 11. Verification checklist (used before closing out any security-relevant task)

- [ ] No secret or credential in the diff.
- [ ] Every new endpoint has an explicit RBAC scope requirement and, where applicable, an ownership check.
- [ ] Every new monetary/PII field has a documented data class (§1) and, if Restricted/Confidential, is handled per §4/§8.
- [ ] New dependencies pass the CI vulnerability scan.
- [ ] New AWS resources follow least-privilege IAM (`14-aws-infrastructure.md §IAM`).
- [ ] Rate limiting applies to any new public write endpoint.

## 12. Traceability

This document operationalizes NFR-SEC-1 through NFR-SEC-5 (`04-non-functional-requirements.md §6`) and the non-negotiables in `00-master-ai-instructions.md §5`. Security-relevant findings during code review follow the process in `18-development-workflow.md §Code Review` and, for structural changes, `/security-review`.
