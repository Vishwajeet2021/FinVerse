# 21 — Decisions log (unattended session, 2026-11-24)

You asked me to finish gRPC logging, complete the frontend against the backend, make the
necessary decisions myself, and explain them here. Each entry says what I chose, why, what I
rejected, and how to undo it. The standing constraint was "do not break any existing feature",
so wherever there was a choice I picked the one that leaves existing behaviour unchanged.

## How I checked I did not break anything

- Unit tests pass in `shared/go` and all nine services.
- After the changes I rebuilt the stack and re-ran all three live demos: `scripts/rails_demo.py`,
  `scripts/fraud_audit_demo.py` and `scripts/dlq_demo.py`. All passed.
- Frontend: `npm run check` (svelte-check) reports 0 errors and 0 warnings, `npm run build`
  succeeds, and `/`, `/login`, `/dashboard` and `/scheduled` return 200 from the dev server.
- CORS preflight from `http://localhost:5173` succeeds for the new calls (POST, DELETE on
  Payment Service, POST on Customer Service).
- **Not done: no real-browser end-to-end test.** I have no browser available to drive, so the
  frontend was verified by type-check, build, page loads, CORS and the same API calls through
  the backend demos. Please click through it once (steps at the end).

## D-1. gRPC and consumer logs carry trace ids

**Chose:** a unary server interceptor (`shared/go/logging/grpc.go`) that logs one line per gRPC
call with `trace_id`, `span_id`, method, status code and duration; installed on Transaction and
Fraud, the only two gRPC servers. Event consumers add the same ids to their *failure* log lines
(retry and dead-letter).

**Why:** it finishes the log-to-trace correlation the previous commit left half done. Verified in
Loki: searching one trace id now finds log lines from payment, fraud and transaction services.

**Rejected:** logging every successfully consumed event. At load-test volume (about 100 payments
per second, each fanning out to several consumers) that is thousands of lines per second for no
diagnostic value; traces already show successful consumption. Failures are the lines you search
for, and those now carry ids.

**Undo:** remove the `grpc.ChainUnaryInterceptor(...)` option in the two `main.go` files.

## D-2. Frontend scope: what I built and what I deliberately did not

**Built:**
1. Payment method (rail) selector on the transfer form, with a hint per rail.
2. Asynchronous payment handling: UPI/IMPS/NEFT/RTGS return `PROCESSING`; the form shows "clearing"
   and polls once per second for up to 60 s, then reports success, failure, or "still clearing".
3. A "Scheduled & recurring" page (`/scheduled`): create one-time scheduled payments and
   standing instructions (daily, weekly, monthly), list them, cancel, pause and resume.
4. Customer profile creation form on the dashboard (previously a dead end: "we don't have a
   profile yet" with no way to make one).
5. Friendly failure messages (fraud denial, rail rejection) instead of a raw reason string.

**Not built, and why:**
- **Notification inbox.** The backend inbox only matches `customer:<id>` recipients, and payment
  events carry no customer id, so the inbox would be empty for nearly every real user. Shipping a
  page that always looks broken is worse than shipping none. The fix is a backend change (events
  carrying the customer id), which is larger than a frontend task and touches every producer.
- **Wallet UI.** Wallet Service has top-up and transfer endpoints, but a wallet screen next to the
  account screen is a product decision (two balances, two ways to pay) I should not invent
  while you are away. The API client config is already in place if you want it.
- **Admin / fraud-analyst screens** (fraud cases, DLQ replay). These are operator tools with
  their own roles and layout; the APIs work and are exercised by the demo scripts.
- **KYC document upload and beneficiaries.** Not part of what you asked me to connect; the
  backend exists but the flow (document types, review states) needs your product input.

**Undo:** the new page and API functions are additive. Deleting `src/routes/scheduled/` and the
"Scheduled & recurring" button removes the feature without affecting anything else.

## D-3. The rail parameter is optional and defaults to INTERNAL

**Chose:** `initiatePayment(from, to, amount, rail?)` only sends `rail` when given, and the server
treats a missing rail as INTERNAL.

**Why:** every existing caller keeps working unchanged, and the default selection in the form is
"Instant (FinVerse)", which behaves exactly as transfers did before.

## D-4. Polling, not push, for payment completion

**Chose:** poll `GET /payments/:id` every second, up to 60 s.

**Why:** the backend has no websocket or server-sent-events channel; building one would be a new
backend feature. Rails clear in 2 to 10 s here, so polling is cheap and simple. After 60 s the UI
says the payment is still clearing and points to the transaction history rather than claiming
failure (the payment may still succeed).

**Trade-off:** one extra request per second per in-flight payment. Fine for this scale.

## D-5. Customer-facing error text

**Chose:** Payment Service failure reasons look like `CODE: customer-safe text`. The UI shows the
text after the colon; anything not in that shape becomes "The transfer could not be completed. No
money was moved."

**Why:** `docs/09-api-standards.md` says customers should never see raw internal errors. The
backend already writes the part after the colon for customers (for example the fraud reason never
reveals thresholds). "No money was moved" is true for every failure path, because failures are
recorded before or instead of ledger posting.

## D-6. Existing uncommitted frontend work was kept and built on

When I started, the working tree already had uncommitted frontend edits that were not mine
(restyled UI components, `Logo.svelte`, `payments.ts` switching the payment field to
`toAccountNumber`). I did not revert or overwrite any of that; I built on it. I also ran Prettier
on the frontend files I touched, so a few of those files show formatting-only changes in the diff.
Because I extended `payments.ts` and the dashboard, that earlier work is in the frontend commit.

## D-7. Datetime inputs

**Chose:** the schedule form uses the browser's `datetime-local` input and converts to an ISO
UTC timestamp before sending; it refuses a time in the past before calling the API.

**Why:** it needs no date-picker dependency and works on every evergreen browser. The server
validates "in the future" again, so the client check is only for a faster message.

## D-8. Things I did not touch

Auth, Customer, Account, Wallet and Transaction behaviour, database schemas, and the public API
contracts are unchanged in this session. The only backend edits were the logging interceptor and
the consumer log fields.

## Known limitations still open

- No real-browser test of the frontend (see above).
- Notification inbox, wallet UI, admin screens, KYC upload: not built (D-2).
- No Alertmanager, so alerts are visible in Prometheus but nothing is delivered.
- Pact contract tests are still not written.
- Phases 4 (AWS) and 6 are not started; they need your AWS account and decisions I should not
  make for you.

## Quick click-through to confirm the frontend

1. `docker compose -f docker/docker-compose.yml up -d`, then in `frontend/`: `npm run dev`.
2. Register, verify the email (the verification link is printed in the auth-service logs), log in.
3. Create your profile, open a savings account.
4. Send a transfer using "UPI" to another account number: you should see "clearing" then success.
5. Open "Scheduled & recurring", schedule a payment two minutes ahead, and confirm it flips to
   EXECUTED.
