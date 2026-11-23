# 07 — Microservice Specifications

## 1. Template

Every service spec below follows the same shape, so switching context between services costs nothing (NFR-MAINT-3):

- **Purpose** — one paragraph, what this service is for and not for.
- **Owns** — its datastore(s), and the fact that no other service touches them directly.
- **Key APIs** — representative endpoints (full contracts in each service's own OpenAPI/proto file, per `09-api-standards.md`).
- **Publishes / Consumes** — domain events, cross-referencing `06-domain-driven-design.md §5`.
- **Sync dependencies** — calls this service makes that block its own response.
- **Scaling notes** — what's stateless, what's cacheable, what's the bottleneck.
- **Phase** — first roadmap phase this service appears in (`19-implementation-roadmap.md`).

## 2. Auth Service

- **Purpose**: Issues and validates identity. Owns the User (login identity) concept, not the Customer (person with money) concept.
- **Owns**: Postgres (`users`, `credentials`, `refresh_tokens`, `mfa_secrets`, `otp_codes`).
- **Key APIs**: `POST /auth/register`, `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout`, `POST /auth/mfa/enable`, `POST /auth/otp/verify`, `GET /.well-known/jwks.json`.
- **Publishes**: `UserRegistered`, `UserVerified`, `LoginFailed`, `MFAEnabled`.
- **Consumes**: none (Auth is upstream of almost everything; it should not need to react to other contexts).
- **Sync dependencies**: none on the login path. JWKS endpoint is polled/cached by the API Gateway, not called per-request.
- **Scaling notes**: fully stateless; refresh-token rotation is the only write-heavy path — indexed by token hash, not raw token.
- **Phase**: 1.

## 3. Customer Service

- **Purpose**: Owns the Customer profile, KYC lifecycle, and beneficiaries.
- **Owns**: Postgres (`customers`, `kyc_documents_meta`, `beneficiaries`); document binaries in S3, referenced by key.
- **Key APIs**: `POST /customers`, `PATCH /customers/{id}`, `POST /customers/{id}/documents`, `POST /customers/{id}/kyc/review` (admin), `POST /customers/{id}/beneficiaries`.
- **Publishes**: `KYCApproved`, `KYCRejected`, `BeneficiaryAdded`.
- **Consumes**: `UserVerified` (to allow profile creation only after email verification).
- **Sync dependencies**: S3 (document upload), none on other services.
- **Scaling notes**: read-heavy (profile lookups); document upload gets a presigned S3 URL rather than proxying bytes through the service.
- **Phase**: 1.

## 4. Account Service

- **Purpose**: Owns bank accounts (savings/current/joint/virtual), their limits, freeze state, and the balance *read projection*.
- **Owns**: Postgres (`accounts`, `account_holders`, `balance_projection`, `interest_schedules`). Does **not** own the ledger of truth — that's Transaction Service.
- **Key APIs**: `POST /accounts`, `GET /accounts` (list the caller's own — added during Phase 2 implementation; a customer can hold more than one account and needs a way to discover them), `GET /accounts/{id}`, `GET /accounts/{id}/balance`, `POST /accounts/{id}/freeze`, `POST /accounts/{id}/unfreeze`, `DELETE /accounts/{id}` (closes the account — terminal, requires a zero ledger balance), `GET /accounts/{id}/statement`.
- **Publishes**: `AccountOpened`, `AccountFrozen`, `AccountUnfrozen`, `AccountClosed`, `InterestAccrued`.
- **Consumes**: `TransactionPosted` (updates `balance_projection`), `KYCApproved` (raises tier/limits).
- **Sync dependencies**: Transaction Service, for statement generation (historical leg detail beyond what's projected locally); Customer Service, for a live KYC-tier lookup (internal HTTP, `GET /internal/customers/by-user/{userId}/kyc-tier`) since Auth Service's JWT `kyc_tier` claim is a documented Phase 1/2 stand-in that's always 0.
- **Scaling notes**: `balance_projection` is Redis-cached in front of Postgres to hit NFR-PERF-1; this is the CQRS read side described in `05-system-architecture.md §5`.
- **Phase**: 1–2.

## 5. Wallet Service

- **Purpose**: A lightweight ledger participant for wallet-style balances, always backed by a linked Account.
- **Owns**: Postgres (`wallets`, `wallet_links`).
- **Key APIs**: `POST /wallets/topup`, `POST /wallets/transfer`, `GET /wallets/{id}/balance`.
- **Publishes**: `WalletToppedUp`, `WalletTransferred`.
- **Consumes**: `TransactionPosted` (balance projection, same pattern as Account).
- **Sync dependencies**: Transaction Service (post ledger legs for top-up/transfer).
- **Scaling notes**: same projection-caching pattern as Account.
- **Phase**: 2.

## 6. Transaction / Ledger Service — core domain

- **Purpose**: The single source of truth for every movement of money on the platform. See `06-domain-driven-design.md §2.5`.
- **Owns**: Postgres (`transactions`, `legs`, `daily_closing_snapshots`, `outbox`). This database has the strictest constraints of any in the system (`08-database-design.md §Ledger`).
- **Key APIs**: `POST /ledger/transactions` (internal, called via gRPC by Payment/Wallet/Loan — not exposed to the public API Gateway directly), `POST /ledger/transactions/{id}/reverse`, `GET /ledger/accounts/{id}/history`.
- **Publishes**: `TransactionPosted`, `TransactionReversed`, `DailyClosingCompleted`.
- **Consumes**: none — Transaction Service must never depend on another service to decide whether to post; that would put a hard dependency in the critical path of every payment.
- **Sync dependencies**: none outbound on the write path.
- **Scaling notes**: the write path (`PostLedgerEntry`) is the throughput bottleneck by design (NFR-SCALE-2); every other service's balance/history views are projections precisely so they don't compete with ledger writes for the same rows.
- **Phase**: 2.

## 7. Payment Service — core domain

- **Purpose**: Orchestrates customer-initiated money movement across simulated rails, scheduled payments, and standing instructions. It is a *coordinator*, not a ledger — it always delegates the actual posting to Transaction Service.
- **Owns**: Postgres (`payments`, `standing_instructions`, `scheduled_payments`).
- **Key APIs**: `POST /payments`, `GET /payments/{id}`, `POST /payments/standing-instructions`, `DELETE /payments/standing-instructions/{id}`.
- **Publishes**: `PaymentInitiated`, `PaymentCompleted`, `PaymentFailed`.
- **Consumes**: none directly on the request path; a scheduler (cron-style, Lambda-triggered — see `14-aws-infrastructure.md`) triggers due scheduled payments/EMI-linked standing instructions.
- **Sync dependencies**: Fraud Service (`CheckTransaction`, gRPC, NFR-PERF-4), Transaction Service (`PostLedgerEntry`, gRPC), Auth Service (OTP step-up, when required), Account Service (account-number -> account-ID resolution for the INTERNAL rail, plain HTTP — the same internal-network-trust posture Account Service itself uses to call Customer Service for KYC tier, not one of the designated gRPC pairs).
- **Scaling notes**: stateless orchestration; the circuit breakers around its two sync dependencies (§7 of `05-system-architecture.md`) are the most important resilience code in this service.
- **Phase**: 2–3.

## 8. Card Service

- **Purpose**: Issues and manages tokenized virtual cards linked to accounts.
- **Owns**: Postgres (`cards` — tokenized reference + last4 only, never full PAN).
- **Key APIs**: `POST /cards`, `POST /cards/{id}/freeze`, `POST /cards/{id}/block`, `PATCH /cards/{id}/limits`.
- **Publishes**: `CardIssued`, `CardFrozen`, `CardBlocked`.
- **Consumes**: `AccountFrozen` (auto-freezes linked cards).
- **Sync dependencies**: none.
- **Scaling notes**: low volume relative to Transaction/Payment; no special scaling needs.
- **Phase**: 6.

## 9. Loan Service

- **Purpose**: Loan origination, EMI scheduling, collection, and prepayment.
- **Owns**: Postgres (`loans`, `emi_schedules`, `emi_payments`).
- **Key APIs**: `POST /loans/apply`, `GET /loans/{id}`, `GET /loans/{id}/schedule`, `POST /loans/{id}/prepay`.
- **Publishes**: `LoanApproved`, `LoanRejected`, `EMIPaid`, `EMIMissed`.
- **Consumes**: `RiskScoreUpdated` (feeds underwriting decision), `PaymentCompleted`/`PaymentFailed` (EMI collection outcome).
- **Sync dependencies**: Payment Service (initiates EMI collection as a payment), Fraud Service (risk score at underwriting time).
- **Scaling notes**: batch-oriented (EMI due-date scans); low request volume otherwise.
- **Phase**: 6.

## 10. Fraud & Risk Service — core domain

- **Purpose**: Real-time risk evaluation and case management. See `06-domain-driven-design.md §2.9`.
- **Owns**: Postgres (`rules`, `cases`, `risk_scores`) + Redis (velocity counters, sliding-window rate data — this is the one service where Redis is a primary data structure, not just a cache).
- **Key APIs**: `POST /fraud/cases/{id}/resolve` (admin), `GET /fraud/cases` (admin). Internal: `CheckTransaction` (gRPC, called by Payment/Loan).
- **Publishes**: `FraudCaseOpened`, `FraudCaseResolved`, `RiskScoreUpdated`.
- **Consumes**: `LoginFailed`, `TransactionPosted`, `CardFrozen`/`CardBlocked`, `EMIMissed` — anything that should move a risk score.
- **Sync dependencies**: none outbound (must stay fast and dependency-free to hit NFR-PERF-4).
- **Scaling notes**: `CheckTransaction` is the most latency-sensitive endpoint in the whole platform; velocity counters live in Redis specifically so this call never round-trips Postgres on the hot path.
- **Phase**: 3 (basic rules), 6 (case management UI, advanced scoring).

## 11. Notification Service

- **Purpose**: Renders and delivers notifications across channels from domain events. Has no opinion on *why* something happened.
- **Owns**: MongoDB (`notifications`, `delivery_attempts`, `preferences`) — chosen because notification documents are naturally schema-flexible per channel (email vs. SMS vs. push payloads differ in shape) and this service has no relational integrity needs.
- **Key APIs**: `GET /notifications` (customer inbox), `PATCH /notifications/preferences`. No public write API — notifications are event-driven only.
- **Publishes**: none (terminal consumer).
- **Consumes**: nearly every event in the catalog (`06-domain-driven-design.md §5`).
- **Sync dependencies**: none — this is the canonical "must never block the originating action" service (NFR-AVAIL-2).
- **Scaling notes**: per-service SQS queue with its own DLQ so a slow email provider never backs up other consumers (bulkhead pattern, `05-system-architecture.md §7`).
- **Phase**: 3.

## 12. Audit Service

- **Purpose**: Immutable record of everything. See `06-domain-driven-design.md §2.11`.
- **Owns**: Postgres, append-only (`audit_records`, partitioned by month; no UPDATE/DELETE grants on the application role — enforced at the DB permission level, not just convention).
- **Key APIs**: `GET /audit/records` (admin/compliance query, filterable). No public write API.
- **Publishes**: none.
- **Consumes**: every event in the catalog, plus explicit "admin action" events emitted directly by Admin Service for actions that don't otherwise produce a domain event.
- **Sync dependencies**: none.
- **Scaling notes**: write-heavy, append-only, time-partitioned; queries are admin-frequency, not customer-frequency, so read optimization is secondary to write durability.
- **Phase**: 2 (basic), ongoing.

## 13. Admin Service

- **Purpose**: BFF (backend-for-frontend) for the admin console; aggregates other services read-only and issues privileged commands, always leaving an audit trail.
- **Owns**: Postgres (`admin_users`, `admin_roles`, `approvals`) — its own data is about *who administers*, not about customers/accounts (which it reads from, never writes into directly).
- **Key APIs**: `GET /admin/dashboard`, `GET /admin/customers/search` (proxies Search Service), `POST /admin/accounts/{id}/freeze` (proxies Account Service + writes Audit), `POST /admin/fraud/cases/{id}/resolve` (proxies Fraud Service).
- **Publishes**: `AdminActionPerformed` (generic event, consumed by Audit when the underlying action doesn't already produce its own domain event).
- **Consumes**: `FraudCaseOpened` (surface to dashboard), health/metrics from other services for the dashboard widget.
- **Sync dependencies**: Account, Fraud, Customer, Search, Reporting Services — Admin is the one service allowed to fan out synchronously to many others, because it's an internal tool where a slightly higher latency budget is acceptable (not on the customer-facing critical path).
- **Scaling notes**: low request volume, internal users only; no special scaling needs.
- **Phase**: 3–6 (grows incrementally as each domain gets an admin view).

## 14. Analytics Service

- **Purpose**: Aggregated, query-friendly analytics for internal use. Pure read model.
- **Owns**: ClickHouse (event-sourced aggregate tables).
- **Key APIs**: `GET /analytics/transactions/volume`, `GET /analytics/customers/growth`, `GET /analytics/risk/distribution`.
- **Publishes**: none.
- **Consumes**: `TransactionPosted`, `PaymentCompleted`, `KYCApproved`, `LoanApproved`, `FraudCaseOpened`, etc.
- **Sync dependencies**: none.
- **Scaling notes**: explicitly allowed to lag (NFR-DATA-2); never a dependency for any write path (FR-ANALYTICS-2).
- **Phase**: 6 (optional/later, per original vision).

## 15. Reporting Service

- **Purpose**: On-demand and scheduled report/statement generation for customers and admins.
- **Owns**: Postgres (`report_requests`, metadata only) — actual report output (PDF/CSV) lands in S3, referenced by a presigned URL.
- **Key APIs**: `POST /reports/statements`, `GET /reports/{id}` (status + download link).
- **Publishes**: none.
- **Consumes**: `TransactionPosted`, `DailyClosingCompleted` (to know when a period is finalized and safe to report on).
- **Sync dependencies**: Transaction Service (historical leg data for the requested range), S3.
- **Scaling notes**: large-range reports run async (Lambda or a worker), never synchronously in the request/response cycle.
- **Phase**: 5–6.

## 16. Search Service

- **Purpose**: Cross-entity fuzzy search for admin/support. Pure read model, never a system of record.
- **Owns**: OpenSearch indices (`customers`, `accounts`, `transactions`).
- **Key APIs**: `GET /search?q=...&type=...`.
- **Publishes**: none.
- **Consumes**: `AccountOpened`, `KYCApproved` (customer/account indexing), `TransactionPosted` (transaction indexing) — via a dedicated indexer consumer, not the query service itself.
- **Sync dependencies**: none.
- **Scaling notes**: index writes are async and can lag (FR-SEARCH-2); query path is read-only and horizontally scalable by adding OpenSearch nodes.
- **Phase**: 6.

## 17. Cross-service conventions recap

All 15 services additionally conform to: `09-api-standards.md` (REST/gRPC/event contract shape), `10-authentication-authorization.md` (every endpoint behind JWT + RBAC unless explicitly internal-only), `11-security-standards.md`, `12-coding-standards.md` (Go project layout), `16-observability.md` (OTel instrumentation is not optional per service — it's part of what "done" means for any service, per `00-master-ai-instructions.md §6`).

## 18. Traceability

Each service's `Owns` section is elaborated with full schemas in `08-database-design.md`; each `Key APIs` row is elaborated with full request/response contracts in the service's own OpenAPI/proto file per `09-api-standards.md §Contract Location`.
