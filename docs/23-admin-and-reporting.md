# 23 — Admin Service and Reporting Service (AI-DLC record)

Status: Inception approved by you ("admin and reporting", 2026-11-24). Construction and
Operations results are appended in sections 5 and 6 as they land.

## 1. Intent and requirement mapping

Give operators a console to act on accounts, fraud cases and KYC with a guaranteed audit trail,
and give customers (and operators) downloadable reports.

| Requirement | In scope | Notes |
|---|---|---|
| FR-ADMIN-2 freeze/unfreeze accounts, resolve fraud cases | yes | KYC review is proxied as well; limit adjustment is not (no limits are stored anywhere to adjust) |
| FR-ADMIN-3 action succeeds only after it is audited | yes | audit-then-act, see D-A2 |
| FR-ADMIN-4 dashboard | partly | volume, open fraud cases, firing alerts, DLQ depth, service health; **active customers is not available** (no endpoint exists) |
| FR-ADMIN-1 admin search | **no** | needs the Search Service (OpenSearch), a separate unit; Admin offers lookup by exact id instead |
| FR-REPORT-1 customer statements | yes, CSV | asynchronous; PDF and the interest/tax report are not built (D-R1, D-R6) |
| FR-REPORT-2 operational reports | partly | fraud case summary; daily closing summary has no data source yet |

## 2. Units

| Unit | Purpose | Owns | Consumes / calls | Publishes |
|---|---|---|---|---|
| A. Account Service change | admin freeze/unfreeze that records who froze, so a customer cannot lift an admin hold | `accounts.frozen_by` | called by Admin over a service-token internal route | `AccountFrozen` / `AccountUnfrozen` now carry `actor` admin or customer |
| B. Auth scopes | privileged scopes for admin roles and report scopes for customers | none | none | none |
| C. Admin Service (:8089) | privileged commands with audit-then-act, dashboard aggregation | `admin_actions` | Account (internal), Fraud, Customer (forwarding the admin's own token), Prometheus (read) | `AdminActionRequested`, `AdminActionResolved` |
| D. Reporting Service (:8090) | asynchronous CSV statements and the fraud summary | `report_requests`, report files | Transaction gRPC history, Account (ownership), Fraud | none |
| E. Wiring | Audit consumes the admin events; compose, DB roles, Prometheus, metrics | n/a | n/a | n/a |
| F. Frontend | `/admin` console and `/statements` page | n/a | Admin and Reporting APIs | n/a |

Acceptance criteria are the live checks in section 6 (`scripts/admin_reporting_demo.py`).

## 3. Decisions

- **D-A1 Admin identity stays in Auth.** `07 §13` lists `admin_users` and `admin_roles` tables in
  Admin Service. Auth already owns users, roles and scopes (`10 §3`), so copying them would create
  two sources of truth. Admin Service owns only what is genuinely its own: the record of every
  privileged action.
- **D-A2 Audit-then-act (FR-ADMIN-3).** Admin writes an `admin_actions` row (status `REQUESTED`)
  and an `AdminActionRequested` outbox event in one database transaction *before* calling the
  downstream service. If that write fails, the action is never attempted (fail closed). After the
  call it records the outcome and emits `AdminActionResolved`. The spec's single
  `AdminActionPerformed` event cannot express "requested but never finished", so there are two.
- **D-A3 Fraud resolve and KYC review forward the admin's own bearer token.** Those services
  already enforce `fraud:resolve` and `customers:kyc:review` and record the resolver; calling them
  with a service token would erase who acted. Freeze uses a new internal route instead, because
  the existing public freeze is gated on the caller holding the account.
- **D-A4 `frozen_by`.** A customer may freeze their own account; only an admin may lift an admin
  freeze. Without this, a customer could undo a fraud hold with one click.
- **D-A5 Dashboard degrades per section.** Each source (Fraud, Prometheus, service health) is
  fetched independently with a short timeout; a failure marks that section unavailable instead of
  failing the page.
- **D-A6 MFA for admins (`10 §6`) is not implemented** because Auth has no MFA yet. Until it does,
  this console must not be exposed beyond a trusted network. Recorded as the top gap.
- **D-R1 CSV only.** CSV is what accountants and tax tools ingest, needs no rendering library, and
  is easy to test byte for byte. PDF is deferred.
- **D-R2 Files on local disk behind a `Storage` interface; download through the authenticated
  API.** S3 and presigned URLs arrive with Phase 4. The API check on download means a leaked
  report id alone exposes nothing.
- **D-R3 Every report is asynchronous**, including small ones, through a leased worker (the same
  pattern as scheduled payments). One code path to test and no latent "large reports only"
  branch.
- **D-R4 Data comes from Transaction Service history** (the system of record), not from Account's
  projection. Opening balance is the sum of legs before the range; each row carries a running
  balance. Limits: 366 days per report, 50,000 rows.
- **D-R5 Ownership by forwarding the bearer to Account Service** (as Payment does), 404 for
  non-holders so report ids and account ids cannot be probed.
- **D-R6 Not built:** the interest-earned tax report (no interest-bearing product exists) and the
  daily closing summary (closing results are not stored anywhere queryable).
- **D-R7 CSV injection.** Any cell beginning with `=`, `+`, `-` or `@` is prefixed with an
  apostrophe so spreadsheets do not execute it. Amounts are written as decimal rupees, never
  floats.

- **D-A7 A freeze must actually stop money leaving (found during construction).** While testing
  the admin freeze I found that **no service enforced FR-ACC-6 at all**: Payment never looked at the
  paying account's status, and Wallet top-ups debit the funding account the same way. The
  customer-facing freeze that already existed was therefore cosmetic too. Fixed in this change,
  because an admin hold that does not hold is worse than none:
  - Account Service exposes `GET /internal/account-status/:id` (service token; Payment and Wallet
    only). It reveals the status and nothing else.
  - Payment checks it before recording anything, for every path into payment initiation, so
    scheduled payments and standing instructions are covered although they carry no user token.
    It checks again at settlement, so an asynchronous rail that was accepted before the freeze
    does not clear after it.
  - Wallet checks it before a top-up.
  - If the state cannot be determined, the operation is refused (fail closed). At settlement an
    unknown state retries instead of posting.
  - "May debit" is cached for 3 seconds per replica to keep a hop off the hot path; a frozen or
    closed answer is never cached. So a freeze takes effect within 3 seconds per replica.
    Measured cost: none visible (section 6).
- **D-R8 Opening balance is summed from ledger history, with a cap.** The ledger has no "balance as
  of" query, so Reporting pages the legs before the start date (at most 100,000). An account with
  more history than that gets a clear `TOO_LARGE` failure rather than a wrong number. A proper
  fix is a balance-as-of query in Transaction Service; recorded as a gap.
- **D-R9 The fraud summary renders a snapshot taken at request time**, read with the
  administrator's own token. The worker therefore needs no credentials of its own and the file
  shows exactly what the administrator could see when they asked.
- **D-R10 History needed `created_at`.** The ledger's `GetAccountHistory` declared `created_at` in
  its protobuf but never filled it, and a statement is useless without dates. Transaction Service
  now returns it. Additive: the field already existed, so existing callers are unaffected.

## 4. Threats considered

| Threat | Mitigation |
|---|---|
| Customer lifts an admin fraud hold | `frozen_by` rule (D-A4), tested |
| Admin acts with no trace | audit-then-act (D-A2); `admin_actions` is written before the action |
| Non-admin calls an admin route | scopes `admin:*` required; customers never receive them |
| Customer reads another customer's statement | bearer forwarded to Account Service, 404 otherwise |
| Report endpoint used to exhaust disk or CPU | range and row caps, per-user rate limit, one worker with leases |
| Spreadsheet formula injection through a transaction description or name | CSV escaping rule (D-R7) |
| Path traversal through report download | files are addressed by generated ids, never by client-supplied names; storage refuses any key that is not `<uuid>.csv` |
| A frozen account keeps paying out through scheduled payments or wallet top-ups | D-A7: enforced in Payment (initiation and settlement) and Wallet, fail closed |
| A stored report altered after generation | SHA-256 recorded at generation and verified on every download |

## 5. Construction

| Unit | What was built | Where |
|---|---|---|
| A | `accounts.frozen_by` (migration 0003, CHECK constraint), actor-aware `FreezeAs` / `UnfreezeAs`, `FROZEN_BY_ADMIN` (403) when a customer tries to lift an admin hold, `actor` on the events, internal routes `/internal/admin/accounts/:id` (+ `/freeze`, `/unfreeze`; admin-service only) and `/internal/account-status/:id` (payment-service, wallet-service) | `services/account-service` |
| B | scopes `admin:dashboard:read`, `admin:accounts:freeze`, `reports:admin` for admin roles; `reports:read`/`reports:write` for customers; test that customers and read-only staff never hold privileged scopes | `services/auth-service` |
| C | Admin Service on :8089 with `admin_db`: `admin_actions` (database trigger makes finished actions immutable and undeletable, and the request part immutable), outbox, audit-then-act, dashboard with per-panel degradation, per-admin rate limit (fail closed), 5 command endpoints | `services/admin-service` |
| D | Reporting Service on :8090 with `reporting_db`: `report_requests`, leased worker (`FOR UPDATE SKIP LOCKED`, lease recovery, 3 attempts), CSV statements and fraud summary, atomic local file storage, SHA-256 on download, 7-day retention sweep, active-report cap (5), range and row caps | `services/reporting-service` |
| E | Audit records `AdminActionRequested` / `AdminActionResolved`; per-service DB roles and passwords for both services; Prometheus scrape targets; `reporting-service` on Transaction's gRPC allow-list; CI workflows; `apiresponse.Accepted` (202) in `shared` | compose, `docker/`, `.github/`, `shared/go` |
| F | `/admin` console (dashboard, fraud cases, freeze/unfreeze with a confirm step, KYC review, action log, fraud export) and `/statements` page (request, poll while building, authenticated download via blob); `apiDownload` helper; Statements and Admin buttons on the dashboard (the admin button appears only if the server answers the admin probe) | `frontend/` |
| D-A7 fix | enforcement of a freeze in Payment (initiation, settlement) and Wallet (top-up) | `services/payment-service`, `services/wallet-service` |
| D-R10 | `created_at` populated in ledger history | `services/transaction-service` |

Unit tests added: Account 3 (admin hold cannot be lifted by a customer, customer hold liftable
by either, event carries the actor), Auth 2 (scope separation), Admin 8, Reporting 20 (CSV
byte-for-byte, running balance, injection guard, ownership, caps, worker retry, tampering,
storage key safety), Payment 5, Wallet 2.

## 6. Operations: live verification

Run on the Compose project `finverse-hardening` (`python scripts/admin_reporting_demo.py`).
**All checks pass** (the script prints each one and exits non-zero on any failure).

- **Access:** anonymous gets 401 and a customer gets 403 on every admin route; Account's internal
  admin route refuses a caller with no service token.
- **Freeze:** an admin freeze stops payments (`422 ACCOUNT_FROZEN`) and wallet top-ups; the
  customer's own unfreeze is refused (`FROZEN_BY_ADMIN`); the admin's unfreeze restores both.
  A second freeze returns the downstream `409` code, recorded as a FAILED action.
- **Audit:** both events reached Audit for each action. `UPDATE` and `DELETE` of a finished
  action are rejected by the database.
- **Audit-then-act:** with the `admin_actions` table renamed away (simulating an audit store
  outage), a freeze answered `503 AUDIT_UNAVAILABLE` and the account stayed ACTIVE.
- **Forwarded identity:** a fraud case resolved through Admin shows the administrator's id as
  `resolvedBy` in Fraud, not a service name. A second resolve passes through the downstream
  `409`. A KYC review of an unknown customer passes through `404` and is recorded as FAILED.
- **Statement correctness:** a 30-day statement of 2,671 legs was generated and its **closing
  balance equals the live ledger balance exactly** (-152,685.00), with every row's running
  balance consistent. An earlier run matched too (2,669 legs, -122,684.00).
- **Isolation:** another customer gets 404 (not 403) on request, view and download of someone
  else's statement; anonymous download is 401; a path-traversal id is 404.
- **Validation:** future end date, range over 366 days, reversed range, non-date and malformed
  account id are each 400. A customer cannot request or download the admin fraud summary.

**Regression** (same stack, after all changes): `scripts/security_check.py` 27/27,
`rails_demo.py` OK (money conserved), `fraud_audit_demo.py` OK, `dlq_demo.py` OK. Unit tests pass
in every touched service. Frontend `svelte-check`: 0 errors, 0 warnings.

**Load test after the freeze check** (20 VU, 60 s): 5,536 payments, **91 req/s**, 0 failures,
p95 268 ms, zero balance drift, projection converged in 0.0 s. Earlier runs on this host were
83-106 req/s, so the extra call is inside the noise (the 3 s cache is why).

A note on running the scripts: `security_check.py` deliberately trips the login rate limit
(30 per minute per IP), so run the other demos before it or wait for the next minute.

### Honest gaps

1. **No MFA for administrators (D-A6).** Auth has no MFA, so an admin account is protected by a
   password alone. This must be closed before the console is reachable outside a trusted network.
2. **Freeze latency.** A freeze takes up to 3 seconds to reach each Payment and Wallet replica
   (cache). Event-driven invalidation would make it immediate; not built.
3. **Statement size.** The opening balance is summed from history (D-R8); an account with more
   than 100,000 legs before the start date fails with `TOO_LARGE`. Needs a balance-as-of query.
4. **Not delivered:** admin search (FR-ADMIN-1, needs the Search Service), active-customers metric,
   PDF statements, interest/tax report, daily closing summary, transaction-limit adjustment.
5. **Dashboard fraud count** is capped at 200 (the page shows "200+").
6. **Reports on local disk** and not shared between replicas: with more than one Reporting
   replica a download can land on a replica without the file. S3 (Phase 4) fixes this.
7. **Admin identity** is the Auth role. There is no per-admin permission model beyond scopes and
   no four-eyes approval for destructive actions.
8. **Not exercised in a real browser:** the new pages are type-checked and their API calls are
   proven by the script, but no human or browser-driven test has clicked through them, and the
   CSP has not been observed against the blob download.
9. **Pre-existing, unchanged:** the gaps in `docs/22` section 5a still stand.
