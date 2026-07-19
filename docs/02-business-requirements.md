# 02 — Business Requirements (BRD)

## 1. Purpose

This document describes FinVerse's business capabilities in business language — what the platform must be able to do for its (simulated) stakeholders — independent of implementation. `03-functional-requirements.md` translates these capabilities into system-level functional requirements; `06-domain-driven-design.md` maps them onto bounded contexts and services.

## 2. Stakeholders

| Stakeholder | Role in the system | Primary concerns |
|---|---|---|
| Retail Customer | Opens accounts, moves money, applies for loans | Speed, clarity of balance/history, security of their money |
| Prospective Customer | Registers, completes KYC | Fast, low-friction onboarding |
| Bank Admin / Ops | Manages customers, accounts, approvals, investigates issues | Visibility, control, audit trail |
| Compliance / Auditor (simulated) | Reviews audit trail, KYC records | Immutability, completeness of records |
| Fraud Analyst (simulated) | Reviews flagged transactions | Accurate risk scoring, low false-positive rate |
| Platform Owner (the developer) | Owns the roadmap, cost, and architecture | Cost control, extensibility, demonstrable rigor |
| Recruiter / Interviewer | Evaluates the system from outside | Clarity of documentation, coherence of design decisions |

## 3. Business capabilities by domain

### 3.1 Identity & Access
- Prospective customers can register with email + phone and verify both before the account is usable.
- Customers and admins authenticate with password + optional MFA; sensitive actions require step-up verification (OTP).
- Access to any resource is governed by role (customer, support, auditor, admin, super_admin) and ownership (a customer can only act on their own accounts).
- Customers can reset a forgotten password through a verified channel (email) without support intervention.

### 3.2 Customer Onboarding & KYC
- A prospective customer provides identity information (PAN-equivalent, address, government ID) and supporting documents.
- KYC has tiers: **Tier 0** (registered, unverified — view-only), **Tier 1** (basic KYC — capped transaction limits), **Tier 2** (full KYC — standard limits). Limits are defined in `03-functional-requirements.md §Account Service`.
- Customers can add, verify, and manage beneficiaries for transfers.
- Documents are stored securely and are retrievable for audit but never exposed in plaintext logs.

### 3.3 Accounts
- A verified customer can open a savings, current, or joint account.
- Every account has a real-time balance, an interest accrual schedule (savings only), and a statement history.
- Accounts can be frozen (by admin, by fraud rule, or by customer self-service "lock my card/account") and unfrozen through an auditable approval flow.
- A "virtual account" capability exists for wallet-style use cases that don't require full account opening.

### 3.4 Wallet
- A customer has one wallet, top-up-able from a linked account, usable for wallet-to-wallet transfers and (later) merchant-style payments.
- Wallet transactions post to the same central ledger as bank accounts — a wallet is not a second bookkeeping system, it's another ledger participant.

### 3.5 Payments & Transfers
- Customers can transfer money between their own accounts, to another FinVerse customer, or externally via simulated NEFT/RTGS/IMPS/UPI rails.
- Customers can schedule a future-dated payment or set up a recurring standing instruction (e.g. monthly rent).
- Every payment has a clearly defined status lifecycle (initiated → processing → completed/failed/reversed) visible to the customer.
- Failed or reversed payments never leave the ledger unbalanced — a reversal is a new, linked, equal-and-opposite entry, never a deletion.

### 3.6 Ledger & Transaction History
- Every movement of money is recorded as a double-entry transaction (one debit, one matching credit, always balanced).
- Customers can view a full, accurate transaction history and download statements for a date range.
- The bank performs a daily closing process that reconciles all accounts and produces an end-of-day balance snapshot.
- No transaction is ever hard-deleted; corrections happen via compensating entries.

### 3.7 Cards
- A customer can request a virtual card tied to an account, with configurable spending limits.
- A customer can freeze, unfreeze, or permanently block a card instantly.
- Card PINs and full card numbers are never stored or logged in plaintext.

### 3.8 Loans
- A customer can apply for a loan; the system produces an approval decision (simulated underwriting) and, if approved, an EMI schedule.
- EMI payments are collected on schedule; missed payments accrue a penalty per documented rules.
- Customers can prepay part or all of a loan, with the remaining schedule recalculated.

### 3.9 Fraud & Risk
- The system enforces daily transaction limits and velocity checks (too many transactions in too short a window) per customer/account.
- Repeated failed logins trigger a temporary lock and a notification to the customer.
- Every action affecting money or account state contributes to a risk score; a sufficiently high score blocks the action and creates a case for review.

### 3.10 Notifications
- Customers are notified (email/SMS/push, per their preference) of key events: successful/failed payments, new device login, large transactions, loan due dates.
- Failed notification deliveries are retried automatically and, after exhausting retries, routed to a dead-letter queue for investigation rather than silently dropped.

### 3.11 Admin & Operations
- Admins can search and view customers, accounts, and transactions; freeze/unfreeze accounts; and review flagged fraud cases.
- Sensitive admin actions (unfreezing an account, overriding a fraud flag, adjusting a limit) require the action to be recorded in the audit trail with the acting admin's identity.
- Admins have a dashboard summarizing platform health: transaction volume, active customers, open fraud cases, system alerts.

### 3.12 Audit & Compliance
- Every state-changing action across every service is recorded in an immutable audit log: who, what, when, before/after state where applicable.
- Audit records are retained indefinitely (or per a documented retention policy) and are never editable, only appendable.

### 3.13 Analytics & Reporting
- The platform can produce aggregate analytics (transaction volume trends, customer growth, risk distribution) for internal/admin use.
- Customers can generate and download account statements and simple tax-relevant reports (e.g. interest earned in a period).

### 3.14 Search
- Admins and support staff can search across customers, accounts, and transactions by common identifiers (name, account number, transaction reference) without needing exact matches.

## 4. Business rules (representative, non-exhaustive — see `03-functional-requirements.md` for the complete, testable list)

| Rule ID | Rule |
|---|---|
| BR-01 | A double-entry transaction must always have debits equal to credits. |
| BR-02 | An account with a negative-eligible product type (e.g. current account with overdraft) may go negative only up to its configured limit; savings accounts never go negative. |
| BR-03 | A Tier 0 (unverified) customer cannot move money out of the platform. |
| BR-04 | A single transfer exceeding the configured high-value threshold requires step-up OTP verification. |
| BR-05 | An account frozen by fraud or admin action rejects all debit operations but still permits credits and read access. |
| BR-06 | A missed EMI payment accrues a penalty per the documented schedule and is reported once to the risk score, not repeatedly per day. |
| BR-07 | No financial record is ever physically deleted; corrections are compensating entries. |
| BR-08 | Every admin action that changes customer-visible state must be attributable to a specific admin identity in the audit log. |

## 5. Assumptions

- All payment rails (NEFT/RTGS/IMPS/UPI) are simulated within FinVerse; no real external bank or network integration exists.
- "KYC verification" is a simulated review step (can be auto-approved in lower environments) rather than integration with a real government ID verification service.
- The system operates in a single currency (INR, chosen for narrative consistency with the NEFT/RTGS/IMPS/UPI simulation) unless multi-currency is explicitly added as a later phase.
- Regulatory compliance is followed in spirit (security, auditability, data protection) but is not formally certified (see `01-product-vision.md §6`).

## 6. Constraints

- Team size: effectively one developer (assisted by AI). Business requirements are scoped to what's buildable at that capacity within the roadmap timeline (`19-implementation-roadmap.md`).
- Budget: AWS spend is a personal-project cost, not a funded engineering budget — see `14-aws-infrastructure.md §Cost Controls` for guardrails this implies.
- No real money, real customers, or real regulatory exposure — this bounds *how real* certain integrations can be (§5 above).

## 7. Traceability

Each business capability in §3 is expanded into concrete functional requirements in `03-functional-requirements.md` using the section numbering convention `FR-<domain>-<n>` (e.g. `FR-PAY-3`), and mapped to owning services in `06-domain-driven-design.md §Bounded Contexts`.
