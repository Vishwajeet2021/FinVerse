# 03 — Functional Requirements (FRD)

## 1. Purpose and conventions

This document enumerates testable functional requirements derived from `02-business-requirements.md`. Each requirement has an ID of the form `FR-<DOMAIN>-<n>`. Requirements are grouped by owning service (see `06-domain-driven-design.md` for bounded-context mapping and `07-microservice-specifications.md` for implementation detail). "Must" = required for the phase that includes this service per `19-implementation-roadmap.md`; "Should" = expected but not blocking.

## 2. AUTH — Authentication & Authorization Service

| ID | Requirement |
|---|---|
| FR-AUTH-1 | System must allow registration with email, phone, and password; password strength validated per `11-security-standards.md`. |
| FR-AUTH-2 | System must send an email verification link/OTP on registration; account remains Tier 0 until email is verified. |
| FR-AUTH-3 | System must support login with email/phone + password, returning a JWT access token and a rotating refresh token. |
| FR-AUTH-4 | System must support enabling/disabling TOTP-based MFA per user; when enabled, login requires a valid TOTP code. |
| FR-AUTH-5 | System must support OTP step-up verification for actions flagged as high-risk by the calling service (e.g. large transfer, new beneficiary). |
| FR-AUTH-6 | System must support refresh-token rotation: each use of a refresh token issues a new one and invalidates the old; reuse of an invalidated token revokes the whole session family. |
| FR-AUTH-7 | System must support logout (single session) and logout-all (revoke all refresh tokens for a user). |
| FR-AUTH-8 | System must support password reset via a time-boxed, single-use, emailed token. |
| FR-AUTH-9 | System must lock an account for a cooldown period after N consecutive failed login attempts (N configurable; default 5) and notify the customer. |
| FR-AUTH-10 | System must expose a JWKS endpoint so the API Gateway and other services can validate JWTs without calling Auth synchronously. |
| FR-AUTH-11 | System must encode role and permission scopes in the JWT claims (RBAC — see `10-authentication-authorization.md`). |
| FR-AUTH-12 | System must publish `UserRegistered`, `UserVerified`, `LoginFailed`, `MFAEnabled` domain events for consumption by Notification, Fraud, and Audit. |

## 3. CUST — Customer Service

| ID | Requirement |
|---|---|
| FR-CUST-1 | System must allow a registered user to create a customer profile (name, DOB, address, PAN-equivalent ID). |
| FR-CUST-2 | System must support document upload (ID proof, address proof) to S3 with metadata stored in Customer Service, never storing the binary in the relational DB. |
| FR-CUST-3 | System must support a KYC review state machine: `PENDING → UNDER_REVIEW → APPROVED (Tier 1 or Tier 2) | REJECTED`. |
| FR-CUST-4 | System must allow a customer to add, edit, and remove beneficiaries, with newly added beneficiaries entering an unverified state requiring confirmation before first transfer. |
| FR-CUST-5 | System must allow a customer to view and update their own profile fields that don't affect KYC tier (e.g. phone, notification preferences) without re-review. |
| FR-CUST-6 | System must publish `KYCApproved`, `KYCRejected`, `BeneficiaryAdded` events consumed by Account, Fraud, Notification, Audit. |
| FR-CUST-7 | System must redact PII (full PAN, full address) from any log line; only a masked form may appear in logs. |

## 4. ACC — Account Service

| ID | Requirement |
|---|---|
| FR-ACC-1 | System must allow a Tier 1+ customer to open a savings, current, or joint account. |
| FR-ACC-2 | System must enforce per-tier transaction/day and balance limits (Tier 1 lower than Tier 2) at the account level. |
| FR-ACC-3 | System must maintain a real-time available balance and ledger balance per account, sourced from Transaction Service events (see FR-TXN). |
| FR-ACC-4 | System must accrue interest daily on savings accounts per a configurable annual rate, crediting monthly. |
| FR-ACC-5 | System must support freezing and unfreezing an account, recording the reason and actor (customer/system/admin). |
| FR-ACC-6 | System must reject any debit-affecting operation against a frozen account while continuing to accept credits. |
| FR-ACC-7 | System must generate a statement (PDF/CSV) for an arbitrary date range, sourced from Transaction Service history. |
| FR-ACC-8 | System must support a "virtual account" type usable by Wallet Service without a full account-opening flow. |
| FR-ACC-9 | System must publish `AccountOpened`, `AccountFrozen`, `AccountUnfrozen`, `InterestAccrued` events. |

## 5. WAL — Wallet Service

| ID | Requirement |
|---|---|
| FR-WAL-1 | System must provision exactly one wallet per verified customer, linked to a primary funding account. |
| FR-WAL-2 | System must support wallet top-up from a linked account, posting a balanced ledger entry via Transaction Service. |
| FR-WAL-3 | System must support wallet-to-wallet transfer between two FinVerse customers. |
| FR-WAL-4 | System must reject a wallet debit that would take the wallet balance negative. |
| FR-WAL-5 | System must publish `WalletToppedUp`, `WalletTransferred` events. |

## 6. TXN — Transaction / Ledger Service

| ID | Requirement |
|---|---|
| FR-TXN-1 | System must record every money movement as a double-entry transaction with exactly one debit leg and one credit leg (or a balanced set of legs for split transactions) whose amounts are equal. |
| FR-TXN-2 | System must reject, at the database level, any transaction whose legs do not sum to zero (see `08-database-design.md §Ledger invariants`). |
| FR-TXN-3 | System must process each transaction request idempotently keyed on a client-supplied idempotency key; a duplicate request returns the original result without double-posting. |
| FR-TXN-4 | System must support a compensating-entry reversal for any posted transaction; reversals reference the original transaction and never delete it. |
| FR-TXN-5 | System must run a daily closing job that snapshots every account's end-of-day balance and flags any reconciliation mismatch. |
| FR-TXN-6 | System must expose transaction history queryable by account, date range, and status, with pagination. |
| FR-TXN-7 | System must publish `TransactionPosted`, `TransactionReversed`, `DailyClosingCompleted` events consumed by Account, Wallet, Notification, Audit, Analytics, Fraud. |
| FR-TXN-8 | System must guarantee serializable consistency on balance-affecting writes to a single account (no lost updates under concurrency). |

## 7. PAY — Payment Service

| ID | Requirement |
|---|---|
| FR-PAY-1 | System must support simulated NEFT, RTGS, IMPS, and UPI transfer initiation, each with rail-appropriate processing-time simulation and status semantics. |
| FR-PAY-2 | System must support scheduling a future-dated one-time payment. |
| FR-PAY-3 | System must support creating, pausing, and cancelling a recurring standing instruction (e.g. monthly). |
| FR-PAY-4 | System must require OTP step-up (via Auth) for any transfer exceeding the configured high-value threshold. |
| FR-PAY-5 | System must call Transaction Service to post the actual ledger entry only after the payment instruction passes fraud screening (FR-FRAUD). |
| FR-PAY-6 | System must expose a payment status lifecycle (`INITIATED → PROCESSING → COMPLETED | FAILED | REVERSED`) queryable by the customer. |
| FR-PAY-7 | System must publish `PaymentInitiated`, `PaymentCompleted`, `PaymentFailed` events. |
| FR-PAY-8 | System must retry a transiently-failed downstream call (e.g. Transaction Service timeout) with backoff before marking a payment failed. |

## 8. CARD — Card Service

| ID | Requirement |
|---|---|
| FR-CARD-1 | System must allow a customer to request a virtual card linked to an eligible account. |
| FR-CARD-2 | System must allow setting and updating per-card spend limits (daily, per-transaction). |
| FR-CARD-3 | System must support instant freeze/unfreeze and permanent block of a card by the customer or admin. |
| FR-CARD-4 | System must never persist or log a full card number or PIN; only a tokenized reference and last-4 digits are stored/displayed. |
| FR-CARD-5 | System must publish `CardIssued`, `CardFrozen`, `CardBlocked` events. |

## 9. LOAN — Loan Service

| ID | Requirement |
|---|---|
| FR-LOAN-1 | System must accept a loan application (amount, tenure, purpose) from a Tier 2 customer. |
| FR-LOAN-2 | System must produce a simulated underwriting decision (approve/reject) based on documented eligibility rules (income proxy, existing exposure, risk score from Fraud Service). |
| FR-LOAN-3 | System must generate an amortizing EMI schedule (principal, interest, due date per installment) on approval. |
| FR-LOAN-4 | System must collect EMIs on schedule by initiating a transfer via Payment Service from the linked account. |
| FR-LOAN-5 | System must apply a documented penalty and risk-score contribution on missed EMI, without double-charging for the same missed date. |
| FR-LOAN-6 | System must support full or partial prepayment with schedule recalculation. |
| FR-LOAN-7 | System must publish `LoanApproved`, `LoanRejected`, `EMIPaid`, `EMIMissed` events. |

## 10. NOTIF — Notification Service

| ID | Requirement |
|---|---|
| FR-NOTIF-1 | System must consume domain events from other services and render channel-appropriate notifications (email, SMS, push, webhook). |
| FR-NOTIF-2 | System must respect per-customer channel preferences and quiet hours where configured. |
| FR-NOTIF-3 | System must retry a failed delivery with exponential backoff up to a configured max attempt count. |
| FR-NOTIF-4 | System must route exhausted-retry notifications to a dead-letter queue and surface them to an admin view rather than dropping them silently. |
| FR-NOTIF-5 | System must deduplicate notifications for the same event ID (idempotent consumption). |

## 11. FRAUD — Fraud & Risk Service

| ID | Requirement |
|---|---|
| FR-FRAUD-1 | System must evaluate every money-movement request against configurable daily limit and velocity rules before it is allowed to post. |
| FR-FRAUD-2 | System must maintain a running risk score per customer, updated on login failures, unusual-location access, high-velocity activity, and fraud-rule triggers. |
| FR-FRAUD-3 | System must block an action and open a review case when risk score or a specific rule crosses its configured threshold. |
| FR-FRAUD-4 | System must allow an admin/fraud analyst to review, approve, or reject an open case, with the decision recorded to Audit. |
| FR-FRAUD-5 | System must expose a synchronous "check" call (gRPC) with a strict latency budget (see `04-non-functional-requirements.md`) since Payment/Loan block on its result. |
| FR-FRAUD-6 | System must publish `FraudCaseOpened`, `FraudCaseResolved`, `RiskScoreUpdated` events. |

## 12. AUDIT — Audit Service

| ID | Requirement |
|---|---|
| FR-AUDIT-1 | System must record every state-changing action across every service as an immutable audit record (actor, action, target, timestamp, before/after where applicable, correlation/trace ID). |
| FR-AUDIT-2 | System must make audit records append-only at the database level (no UPDATE/DELETE grants on the audit table for any application role). |
| FR-AUDIT-3 | System must support query by actor, target entity, action type, and date range for admin/compliance review. |
| FR-AUDIT-4 | System must consume domain events from all other services asynchronously so that a slow or unavailable Audit Service never blocks the originating action. |

## 13. ADMIN — Admin Service

| ID | Requirement |
|---|---|
| FR-ADMIN-1 | System must provide admin search/view over customers, accounts, and transactions (via Search Service). |
| FR-ADMIN-2 | System must allow authorized admin roles to freeze/unfreeze accounts, resolve fraud cases, and adjust documented limits. |
| FR-ADMIN-3 | System must require every sensitive admin action to succeed only after it's been written to the audit trail (audit-then-act or transactionally linked — see `07-microservice-specifications.md §Admin`). |
| FR-ADMIN-4 | System must present a dashboard of platform health metrics (volume, active customers, open fraud cases, alert counts). |

## 14. ANALYTICS — Analytics Service

| ID | Requirement |
|---|---|
| FR-ANALYTICS-1 | System must aggregate transaction, customer growth, and risk data into query-friendly analytical models (ClickHouse), sourced from the same domain events other services consume. |
| FR-ANALYTICS-2 | System must not be a dependency for any customer-facing write path — analytics is read-only and eventually consistent by design. |

## 15. REPORT — Reporting Service

| ID | Requirement |
|---|---|
| FR-REPORT-1 | System must generate customer-facing statements and simple tax-relevant reports (e.g. interest earned) on demand and asynchronously for large ranges. |
| FR-REPORT-2 | System must generate internal operational reports (daily closing summary, fraud case summary) for admin consumption. |

## 16. SEARCH — Search Service

| ID | Requirement |
|---|---|
| FR-SEARCH-1 | System must support fuzzy/partial search across customers, accounts, and transactions by common identifiers for admin/support use. |
| FR-SEARCH-2 | System must keep its index eventually consistent with source-of-truth services via domain events, never acting as a system of record. |

## 17. Cross-cutting functional requirements

| ID | Requirement |
|---|---|
| FR-X-1 | Every write API must accept and honor an idempotency key where the operation has financial or side-effecting consequence. |
| FR-X-2 | Every service must emit domain events for state changes other bounded contexts are documented as caring about (see `06-domain-driven-design.md §Context Map`). |
| FR-X-3 | Every user-facing error must map to a documented error code (see `09-api-standards.md §Error Format`) rather than leaking internal detail. |
| FR-X-4 | Every service must expose a health check and readiness endpoint for orchestration and monitoring. |

## 18. Traceability

Each `FR-*` requirement maps to one or more `NFR-*` constraints in `04-non-functional-requirements.md` and to a concrete implementation in the corresponding section of `07-microservice-specifications.md`.
