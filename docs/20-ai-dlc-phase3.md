# 20 — AI-DLC Record: Messaging + DLQ, Tracing, Load Tests, Fraud, Audit (Phase 3 slice)

Worked through the AI-DLC loop (`.claude/skills/ai-dlc/SKILL.md`): Inception → Construction → Operations. Date: 2026-11-24.

## 1. Inception

**Intent.** Make the event-driven backbone real and observable: a consumer service with retry and a dead-letter queue, distributed tracing across the whole payment path, and measured load-test numbers.

**Requirements covered.** FR-NOTIF-1..5 (partially, see §5), NFR-AVAIL-2, NFR-OBS-1, NFR-PERF-3, NFR-PERF-5, NFR-SCALE-2, NFR-DATA-3.

**Units.** (1) `shared/go/eventbus` hardening; (2) `shared/go/telemetry`; (3) Notification Service; (4) tracing wiring in the 7 existing services; (5) `loadtest/` + `scripts/dlq_demo.py`; (6) Fraud Service + the Payment to Fraud synchronous call (FR-FRAUD-1, -2, -5, NFR-PERF-4); (7) Audit Service (FR-AUDIT-1..4).

**Decisions.**
| ID | Decision | Why |
|---|---|---|
| D-1 | Keep Redis Streams as the broker (no RabbitMQ/LocalStack) | Matches `05-system-architecture.md §5` and `18 §7`; the SQS-style semantics (visibility timeout, max receives, DLQ) are implemented in `eventbus` behind the same interface a real SQS implementation would use. |
| D-2 | DLQ is per (stream, consumer group): `<stream>:dlq:<group>` | One service's poison messages never mask or replay into another's (bulkhead). Replay is addressed (`target_group`) so other services don't see a duplicate. |
| D-3 | Notification Service uses Postgres, not MongoDB (spec §11) | Delivery log is tabular, idempotency is a UNIQUE constraint, and it avoids a third datastore for one service. Revisit if schema-flexible channel payloads become real. |
| D-4 | Tracing uses OTLP/HTTP to Jaeger; trace context is carried through the outbox (`traceparent` column) and the event envelope | The relay runs on a timer, so without storing it the event fan-out would start new traces. |
| D-5 | Fraud `CheckTransaction` is gRPC (`shared/proto/fraud/v1`), called by Payment after the payment row is recorded and before the ledger post | Matches `05-system-architecture.md` 6.1; recording first means a denial is itself auditable. |
| D-6 | Unknown fraud verdict (Fraud down/slow >300 ms) **fails closed** by default (`FRAUD_FAIL_OPEN=true` to flip) | Money-moving default. Cost: a Fraud outage blocks payments; a circuit breaker/degraded mode is future work. |
| D-7 | Fraud keeps counters in Redis only on the hot path; cases are written asynchronously (queue + outbox) and may be dropped with an error log if the queue (1024) is full | Never let case bookkeeping threaten the 80 ms budget. The verdict and counters are unaffected by a dropped case. |
| D-8 | Audit append-only is enforced by a Postgres trigger (UPDATE/DELETE/TRUNCATE raise), not just app convention | Local compose uses one superuser role, so role grants alone would prove nothing; production should additionally revoke UPDATE/DELETE from the app role. |

## 2. Construction — what was built

**Messaging with DLQ** (`shared/go/eventbus`)
- Exponential redelivery backoff (`WithBackoff`), capped; `MaxDeliveries` then dead-letter.
- `eventbus.Permanent(err)`: poison messages skip retries and go straight to the DLQ.
- DLQ entries keep the original envelope, attempt count and reason (the previous version dropped the payload, so nothing could be replayed). `DLQ.List/Replay/Discard`.
- Per-consumer `Stats()` (processed / retried / deadLettered).
- Tests (real Redis, `REDIS_TEST_ADDR`): backoff schedule, retry-then-DLQ with payload, permanent-error short circuit, transient recovery without DLQ, replay reaching only the target group.

**Notification Service** (`services/notification-service`, :8086)
- Consumes `PaymentCompleted`, `PaymentFailed`, `AccountOpened`, `AccountFrozen`; idempotent on `event_id`; a pluggable `Sender` (logging "email" with a switchable simulated outage).
- Admin API (scope `notifications:admin`, added to Auth Service): list/replay/discard dead letters, consumer stats, provider-outage switch (not registered in production). Customer inbox `GET /api/v1/notifications` (scope `notifications:read`).

**Tracing** (`shared/go/telemetry`): gin + gRPC instrumentation in all 7 services, producer/consumer spans in `eventbus`, trace context persisted on outbox rows (migration `0002_outbox_traceparent` in transaction/account/payment/wallet). Jaeger at http://localhost:16686.

**Fraud Service** (`services/fraud-service`, HTTP :8088, gRPC :9092): rules in `internal/rules` (single-transfer cap, per-minute velocity, per-day total, review threshold) over Redis counters; verdicts ALLOW / REVIEW (proceeds, opens a case) / DENY. Customer-facing reasons never reveal thresholds (tested). REVIEW/DENY cases are stored in Postgres and published as `FraudCaseOpened` via an outbox; analysts list and resolve cases (`fraud:read`, `fraud:resolve`, granted to the fraud-analyst and admin roles). Payment Service gained a `FraudChecker` port and a gRPC client with a 300 ms budget; 4 new unit tests cover deny, review, fail-closed and fail-open. Product defaults: 10 payments/min, INR 1,00,000/day, INR 50,000 per transfer, review at INR 25,000; compose loosens velocity/daily so the load test isn't throttled by its own fraud rules.

**Audit Service** (`services/audit-service`, :8087): consumes 13 event types (payments, ledger, accounts, wallets, fraud), idempotent on `event_id`, stores the full payload as JSONB; read-only `GET /api/v1/audit/records` (`audit:read`, filter by event type, keyset pagination); no write endpoint. Immutability by trigger (D-8).

## 3. Operations — verified live (docker compose, 2026-11-24)

**Machine:** Intel i7-11800H (16 threads), 16 GB RAM, Docker Desktop/WSL2; everything (services, Postgres, Redis, Jaeger, k6) on the same host, so these are *relative, single-host* numbers, not capacity claims.

**Resilience demo** (`scripts/dlq_demo.py`): with the email provider down, a payment still returned `COMPLETED` in 85 ms (NFR-AVAIL-2 demonstrated). Its `PaymentCompleted` notification was retried 4 times with backoff and dead-lettered after ~11 s; an injected malformed event was dead-lettered on the first attempt; after the outage ended, replaying the dead letter delivered it.

**Trace of one payment** (Jaeger): 13 spans across 5 services — `payment-service POST /api/v1/payments` → gRPC `PostTransaction` (payment and transaction side) → `TransactionPosted publish` → account/wallet consumers → `PaymentCompleted publish` → notification consumer spans (including each retry as its own span).

**Load test** (`loadtest/payments.js`, k6 0.54, 20 VUs, 60 s, internal transfers across 4 accounts in a ring):

| Metric | Run B (final) |
|---|---|
| Payments completed | 6,401 |
| Throughput | 106 req/s |
| Error rate | 0.00 % |
| Payment latency avg / median | 136 ms / 128 ms |
| p90 / p95 | 201 ms / 228 ms |
| p99 | < 500 ms (threshold passed; max 501 ms) |
| Balance drift | 0 (sum of deltas) |
| Projection caught up after load | 0.0 s |

NFR-PERF-3 (sync transfer p99 < 500 ms): met on this host. Run A (same config, 6,580 payments, 94 req/s, p95 221 ms, 0 failures) is consistent.

**Fraud + Audit demo** (`scripts/fraud_audit_demo.py`, all assertions passed): a INR 60,000 payment was `FAILED` with `FRAUD_DENIED` and never reached the ledger; a INR 30,000 payment completed and opened a REVIEW case an analyst then resolved; the denial, completion, case-opened and case-resolved events all appeared in Audit; `UPDATE`, `DELETE` and `TRUNCATE` on `audit_records` were each rejected by the database; with Fraud Service stopped, a payment failed closed with `FRAUD_CHECK_UNAVAILABLE` (answered in 323 ms, i.e. at the 300 ms budget) and payments succeeded again after it restarted.

**Load test with Fraud in the path** (same setup, plus Fraud, Audit and Notification all consuming): 5,438 payments, 90 req/s, 0 % errors, payment p95 287 ms (k6) and p99 389 ms (Jaeger server-side spans of the newest 1,500 requests; k6's own p(99) < 500 ms threshold passed), zero drift, projection caught up in 0.0 s. **Fraud `CheckTransaction`: p50 0.92 ms, p95 4.95 ms, p99 9.14 ms (n = 1,500 newest spans)**, well inside NFR-PERF-4's 80 ms. Throughput was lower than the 106 req/s without Fraud/Audit; the two extra services and ~13 extra consumers share the same laptop's CPU, and I did not isolate how much of the difference is the fraud hop versus contention (the hop itself is about 1 ms median).

**Payment rails, scheduling and ownership** (`scripts/rails_demo.py`, all assertions passed on the live stack): INTERNAL completes synchronously (0.1 s); UPI, IMPS and NEFT return `PROCESSING` with a `settleAt` and reach `COMPLETED` after 3.1 s, 4.1 s and 7.1 s (simulated delays 2 s / 3 s / next 10 s NEFT batch, plus up to 1 s poll latency). UPI over INR 1,00,000, RTGS under INR 2,00,000 and an unknown rail were each rejected with 400. Repeating an `Idempotency-Key` on an async rail returned the same payment. Paying from an account the caller does not hold returned 403 (this closes the ownership gap noted below in earlier revisions). A scheduled payment executed at its time; a cancelled one never did and has no payment; a past date is rejected. A DAILY standing instruction fired once and advanced its next run by exactly 24 h; pause worked and cancel is terminal (resume after cancel -> 409). Balance deltas across the three users summed to 0. The raw `FOR UPDATE SKIP LOCKED` claim queries ran correctly against real Postgres.

**Load test after rails + ownership check** (same 20 VU / 60 s ring): 6,148 payments, 100 req/s, 0 % errors, payment p95 242 ms, max 406 ms, zero drift, projection converged in 1.0 s. The ownership check adds one Account Service call per cold (user, account) pair then hits a 60 s cache, which is why throughput is within noise of the 106 req/s baseline.

**Metrics and alerting** (verified live): every service now serves `/metrics`; Prometheus scrapes all 9 (all targets `up`) and Grafana (localhost:3001) provisions three dashboards from `docker/observability/` (Platform overview, Payment funnel and ledger health, Fraud/messaging/DLQ). Metrics: RED per service and route template (`finverse_http_*`), payment funnel (`finverse_payments_total{status}`), ledger posted and balance-check failures, Fraud check latency histogram and verdicts, event-bus outcomes and per-group DLQ depth. Five alert rules from `16-observability.md` section 8 are loaded. Two were exercised for real: `DeadLetterQueueNotEmpty` was already firing for a dead letter left over from an earlier demo (notification-service / PaymentFailed), and stopping Audit made `ServiceDown` fire for `audit-service` within 25 s. Fraud `CheckTransaction` p99 read 2.4 ms from the histogram. `LedgerImbalance`, `FraudCheckLatencyHigh` and `HighErrorRate` loaded but were not triggered.

**Centralized logs** (verified live): Promtail tails every compose container through the Docker socket into Loki (7-day local retention); Grafana has a Loki datasource whose derived field turns `trace_id` in a log line into an "Open in Jaeger" link. HTTP request log lines now carry `trace_id` and `span_id`. Verified by querying Loki: all 16 compose containers appear as `service` labels, and a `{service="payment-service"} | json | trace_id != ""` query returns lines with real trace ids. Limit found: searching Loki for one payment's trace id matched only payment-service, because gRPC handlers and event consumers do not log through the HTTP middleware and so carry no trace id; the cross-service log correlation in `16-observability.md` section 2 is therefore only half done (traces in Jaeger do span all services).

**One anomaly, reported honestly:** in Run A the 10-second post-load check saw a net drift of 100 minor units (one payment's worth). The ledger itself summed to exactly 0 and the account projections matched it later, so no money was lost — one `TransactionPosted` event had not been applied by Account Service within 10 s. I did not find the root cause (a leftover pending entry from a consumer that was restarted during the earlier rebuild is the likeliest suspect; it was not reproduced in Run B). The test now polls until convergence and fails if it takes longer than 5 s (NFR-PERF-5), so a recurrence will be caught.

## 4. Reproducing

See `loadtest/README.md`. Unit/integration tests: `go test ./...` per module; `REDIS_TEST_ADDR=localhost:6390 go test ./eventbus/` in `shared/go` against a throwaway Redis.

## 5. Known gaps (not done)

- Notification: email only (logger), no SMS/push; customer inbox only resolves `customer:<id>` recipients — payment events carry no customer ID, so those notifications are visible via the admin list only; preferences API (FR-NOTIF) not built.
- Fraud: only amount/velocity/daily rules (FR-FRAUD-1, -2, -5); no geo/device checks, case-management UI or risk-score tuning (Phase 6); no circuit breaker; `FraudCaseOpened` events carry the case, but Fraud does not yet consume `LoginFailed`/`TransactionPosted` to move risk scores. Counters count denied attempts too and are fixed windows, not sliding.
- Audit: not partitioned by month yet; no explicit "admin action" events; production role grants not set (trigger only).
- Ownership check caches positive answers for 60 s, so a holder removed within the last minute can still initiate a payment for up to a minute. `GET /payments/:id` does not check that the caller owns the payment.
- Rails: the RTGS happy path and the simulated beneficiary-failure path (`RAIL_FAILURE_RATE`) are covered by unit tests but were not exercised live (the demo accounts hold less than INR 2,00,000; failure rate was 0). Rails are simulated, never real (per `01-product-vision.md` §6). Scheduled/standing payments run at one-second poll granularity and use a fixed 30 s retry; there is no notification when a schedule fails. Not started: Pact contract tests (rest of Phase 3). OTel Collector (rest of Phase 5). Logs: gRPC calls and consumer failures now carry trace ids (see `docs/21-decisions-log.md` D-1); a Loki search for one trace id finds payment, fraud and transaction log lines. Successful event consumption is deliberately not logged. I did not eyeball the Grafana Explore UI or test the Jaeger derived-field link. Metrics gap: no Alertmanager, so alerts are visible in Prometheus only and nothing is delivered; dashboards were provisioned and load-verified through the API but I did not eyeball the rendered panels; no exemplars linking metrics to traces; histograms are per-process, not per-replica aware; `/metrics` is unauthenticated (fine on the compose network, must be restricted in AWS). Phase 4 (AWS) and Phase 6.
- The existing Phase 3 "outbox relay for Transaction/Account/Payment/Customer/Auth": Customer and Auth have no outbox yet.
- HTTP calls between services (Payment → Account lookup) are not traced; only gRPC and event hops are.
- Load test covers one scenario (transfers) at one load level on a single host; no soak or spike test.
