# 19 — Implementation Roadmap

## 1. How to read this document

This is the single place that tracks what's actually built versus what's designed (`00-master-ai-instructions.md §4.4`). Each phase lists: goals, services/deliverables, exit criteria, which NFRs it's expected to satisfy, and a content checkpoint. **Update the status markers as work completes — this document decays into fiction fast if it isn't kept honest.**

Status legend: `[ ]` not started, `[~]` in progress, `[x]` done.

## 2. Phase 1 — Foundation (target: ~3–4 weeks)

**Status as of 2026-07-19: complete, verified live end-to-end, and pushed to GitHub.** Two bugs found only once Phase 2 services actually integrated with these: Auth Service's JWT `kyc_tier` claim is hardcoded to `0` on every login (see the code comment in `services/auth-service/internal/service/login.go`) — harmless in Phase 1 alone since nothing read it yet, but it meant Account Service's Tier-1+ gate (Phase 2) could never pass; fixed by having Account Service call a new internal Customer Service endpoint for a live tier lookup instead of trusting the claim. Also, registering a user with no phone number stored empty string rather than SQL `NULL`, so a second passwordless-phone registration collided on the `phone` UNIQUE constraint — fixed by storing `NULL` for an omitted phone.

**Goal**: a working skeleton — repo, local infra, identity, and a customer profile — end to end.

- [x] Repository structure per `13-repository-structure.md` scaffolded; root README written.
- [x] `docker/docker-compose.yml` brings up Postgres, Redis, Auth Service, and Customer Service, with healthchecks and correct startup ordering (retry-with-backoff on the JWKS dependency too, not just Compose ordering — see `05-system-architecture.md §7`). Jaeger/Prometheus/Grafana stubs were deliberately *not* added — Phase 1's own service set has nothing to observe yet beyond basic logs, and standing up unused infrastructure would violate `00-master-ai-instructions.md §4.5`'s scope discipline. Revisit in Phase 5.
- [x] Auth Service: registration, login, JWT issuance (RS256 + JWKS), refresh rotation with reuse-detection, logout/logout-all, email verification + password reset (FR-AUTH-1 to -3, -6, -8, -10, -12). Refresh token ships as an HttpOnly cookie per `10-authentication-authorization.md §2`, not in the JSON body. MFA/OTP (FR-AUTH-4/5) deferred to Phase 3 as explicitly permitted above. Unit tests pass against in-memory fakes; `go vet`/`golangci-lint` clean.
- [x] Customer Service: profile creation, basic KYC state machine with dev auto-approve (FR-CUST-1, -3, -5), plus beneficiaries (FR-CUST-4) and document metadata behind a `Storage` interface (local disk in Phase 1, swappable for S3 later) ahead of schedule. PAN is AES-256-GCM field-encrypted per `11-security-standards.md §4`. Unit tests, `go vet`, `golangci-lint` clean. Known gap: FR-CUST-1's "address" field has no column yet in either the doc or the migration — not blocking, tracked here for a future pass.
- [x] Frontend: SvelteKit skeleton (TypeScript strict, Tailwind, TanStack Query), `/`, `/login`, `/register`, `/verify-email`, `/verify-email-sent`, `/dashboard` — dashboard degrades gracefully if Customer Service is unreachable. Build, `svelte-check`, and lint all clean.
- [x] CI: lint + unit test GitHub Actions workflows for Auth Service, Customer Service, and the frontend (`15-cicd-pipeline.md §3`), without deploy stages yet — nothing to deploy to yet.
- [~] Basic GitHub repo hygiene: issue templates, PR template, and the pushed remote are done; branch protection on `main` itself hasn't been configured yet.

**Exit criteria**: a user can register, verify email, log in, and see a profile page, fully locally, with passing CI.

**NFRs touched**: NFR-PORT-1 (local parity from day one), NFR-SEC-3/4 baseline.

**Content checkpoint**: "Why I'm building a banking platform with DDD and Go" — architecture overview post, using `05-system-architecture.md`'s diagrams.

## 3. Phase 2 — Banking Core (target: ~4–6 weeks)

**Goal**: real money movement, correctly, with a real double-entry ledger.

**Status as of 2026-07-19: complete and verified live.** Built directly (not delegated) for the ledger; Account/Wallet/Payment Service builds started as parallel background agents but were interrupted mid-build by a session usage-limit cutoff — their partial output (domain/service layers, mostly with tests already written) was reviewed and judged solid, then completed directly rather than restarted from scratch.

- [x] Transaction/Ledger Service: legs/transactions schema with DB-enforced balance invariant, idempotent posting, outbox (FR-TXN-1 to -8). This is the highest-scrutiny work in the whole roadmap — see `06-domain-driven-design.md §1` on why it's a core domain. Verified with a real Postgres integration test (testcontainers) proving the database itself — not just application code — rejects unbalanced legs and rolls back atomically.
- [x] Account Service: open account, balance projection (consuming `TransactionPosted`), freeze/unfreeze, statement (FR-ACC-1 to -9). Also shipped: `GET /accounts` (list own — a gap found wiring the frontend, not in the original spec) and a live KYC-tier lookup against Customer Service (Auth's `kyc_tier` JWT claim is hardcoded to 0 in this phase — see Phase 1 note below — so the Tier-1+ gate calls Customer Service directly instead of trusting the claim).
- [x] Wallet Service: top-up, wallet-to-wallet transfer (FR-WAL-1 to -5).
- [x] Payment Service: internal transfers end-to-end (simulated rail selection is a stub returning `INTERNAL` only; full NEFT/RTGS/IMPS/UPI simulation timing is Phase 3, as planned).
- [x] Frontend: dashboard with real balance (via TanStack Query), transfer flow, transaction history list. Wallet Service has no dedicated UI yet (verified via API only) — the roadmap only asked for account balance/transfer/history, so wallet UI is deferred rather than scope-crept in.
- [x] Property-based tests for the ledger balance invariant (`17-testing-strategy.md §2`) — 500 randomized balanced/perturbed-unbalanced cases, seeded for reproducibility.
- [x] Load test #1: concurrent transfer test proving zero balance drift (NFR-SCALE-2, NFR-DATA-3) — 20 concurrent transfers fired at the live stack; both accounts' balances and their sum matched the expected values exactly. A larger-scale, automated version of this (not just a 20-request manual burst) is still a good Phase 5 hardening item.
- [x] **Local event bus decided and built**: Redis Streams, not LocalStack — see `18-development-workflow.md §7` and `05-system-architecture.md §5` for the decision this phase had to make earlier than planned (Account/Wallet's balance projections needed it to exist now, not in Phase 3).
- [x] **Real bugs found and fixed during integration** (not just unit-tested in isolation): a GORM `Scan`-into-embedded-struct bug that silently zeroed out ledger history query results; two services' first gRPC call to Transaction Service timing out on a cold connection (fixed with eager `conn.Connect()` in all three gRPC clients); registering a second user with no phone number colliding on the `phone` UNIQUE constraint because empty string was stored instead of SQL NULL.

**Exit criteria**: two customers can transfer money to each other; the ledger balances under concurrent load; a statement can be generated.

**NFRs touched**: NFR-DATA-1 to -4 (consistency), NFR-PERF-1 to -3.

**Content checkpoint**: "Building double-entry accounting correctly: lessons from fintech systems" — with the property-based test and load-test numbers as evidence.

## 4. Phase 3 — Distributed Systems (target: ~4–5 weeks)

**Goal**: the event-driven backbone is real, not aspirational — services react to each other without direct coupling.

- [~] (Redis Streams chosen; outbox relay exists for Transaction/Account/Payment/Wallet, not yet Customer/Auth) EventBridge-equivalent wired locally (LocalStack or the interim substitute decided in `18-development-workflow.md §7`), outbox relay implemented for Transaction/Account/Payment/Customer/Auth.
- [~] Notification Service: consumes Payment/Account events, email via a logging sender, idempotent, retry with exponential backoff + per-service DLQ with inspect/replay/discard admin API — verified live (`docs/20-ai-dlc-phase3.md`). Gaps: SMS/push, preferences, customer inbox for payment events (FR-NOTIF partially).
- [x] Fraud Service (basic): single-transfer/daily/velocity/review rules, `CheckTransaction` gRPC wired into Payment's synchronous path (fail-closed by default), REVIEW/DENY cases with analyst list/resolve and `FraudCaseOpened`/`Resolved` events (FR-FRAUD-1, -2, -5). Verified live; CheckTransaction p99 9.1 ms vs the 80 ms NFR-PERF-4 budget (`docs/20-ai-dlc-phase3.md`).
- [x] Audit Service: consumes 13 event types, append-only (DB trigger-enforced), idempotent, read-only query API (FR-AUDIT-1 to -4). Verified live. Admin-action events (`AdminActionRequested`/`Resolved`) are recorded since `docs/23`. Not yet: monthly partitioning.
- [x] Payment rail simulation (UPI/IMPS/NEFT/RTGS limits, PROCESSING->COMPLETED/FAILED with per-rail delays, NEFT batches), scheduled payments, standing instructions (daily/weekly/monthly, pause/resume/cancel), caller-ownership check on `fromAccountId` (FR-PAY-1 to -3). Verified live; see `docs/20-ai-dlc-phase3.md`. Gaps: RTGS happy path and failure injection only unit-tested.
- [x] Freeze enforcement (`docs/23` D-A7): a frozen or closed account can no longer pay or fund a wallet top-up (FR-ACC-6 was documented but unenforced before this). Payment checks at initiation and settlement, Wallet at top-up, fail closed.
- [x] Security hardening pass (docs/22): headers and body limits, service-to-service tokens, payment ownership, validation, rate limiting, least-privilege DB roles, secrets from env; 27 live checks in `scripts/security_check.py`. Gaps listed in docs/22 section 5a.
- [ ] Contract tests (Pact) stood up for the gRPC calls that now exist (Payment↔Fraud, Payment↔Transaction).

**Exit criteria**: a payment triggers a fraud check, a ledger post, a notification, and an audit record — and killing the Notification Service doesn't affect payment success (NFR-AVAIL-2 demonstrated, not just claimed).

**NFRs touched**: NFR-AVAIL-2/3, NFR-OBS-1 (trace propagation across the event fan-out).

**Content checkpoint**: "Scaling asynchronous notifications using SQS-style queues and retry/DLQ handling" — with a killed-service demo as the hook.

## 5. Phase 4 — Cloud Native (target: ~3–4 weeks)

**Goal**: the system runs on real AWS infrastructure, deployed via CI/CD, not just locally.

- [ ] Terraform modules + `dev` environment stood up per `14-aws-infrastructure.md` (VPC, ECS cluster, RDS, S3, basic IAM).
- [ ] GitHub Actions per-service pipelines (`15-cicd-pipeline.md §3`) deploying to `dev`, then `staging`.
- [ ] CloudFront + S3 frontend deploy.
- [ ] API Gateway fronting the deployed services with JWT validation at the edge.
- [ ] Secrets Manager/Parameter Store wired in, replacing local `.env` for deployed environments.
- [ ] Migration gate (`15-cicd-pipeline.md §7`) enforced in the pipeline.

**Exit criteria**: a merge to `main` deploys Phase 1–3 services to a real, reachable `staging` URL, with migrations applied automatically and secrets sourced correctly — no manual AWS console steps.

**NFRs touched**: NFR-AVAIL-4, NFR-SEC-1/2/3 in a real cloud context, NFR-COST-1.

**Content checkpoint**: "Deploying Go microservices to AWS ECS with Terraform and GitHub Actions" — architecture diagram of the deployed topology.

## 6. Phase 5 — Platform Engineering (target: ~3–4 weeks)

**Goal**: the system is observable and its performance claims are measured, not assumed.

- [~] OpenTelemetry instrumentation across all Phase 1–4 services — tracing done for all 7 services + event hops, verified in Jaeger (13-span payment trace); metrics not yet. (Original text:) (if not already present incrementally — this phase is where it's verified end-to-end, not where it starts).
- [x] Prometheus + Grafana dashboards per `16-observability.md §6` (Platform overview, Payment funnel + ledger health, Fraud/messaging/DLQ), with the section 8 alert rules loaded in Prometheus (no Alertmanager delivery yet). Verified live; see `docs/20-ai-dlc-phase3.md`.
- [x] Loki + Promtail centralized logging: all containers; HTTP, gRPC and consumer-failure log lines carry `trace_id`, linked to Jaeger from Grafana. Verified live (see `docs/20-ai-dlc-phase3.md`).
- [ ] Alerting rules per `16-observability.md §8` wired to a real notification channel.
- [~] Formal k6 load testing suite — `loadtest/payments.js` (106 req/s, p95 228 ms, 0 errors, zero drift on one host; see `docs/20-ai-dlc-phase3.md`); no spike/soak yet. (Original text:) (automatable, not just the manual Phase 2 run) against a dedicated temporary stack.
- [ ] Backup/restore drill executed and documented (`14-aws-infrastructure.md §10`).

**Exit criteria**: a deliberately broken service (e.g. kill Transaction Service's DB connection) is detected by an alert within the NFR-OBS-5 5-minute target, and the trace/log/metric correlation actually works when investigating it.

**NFRs touched**: NFR-OBS-1 to -5 fully, NFR-AVAIL-5 (RPO/RTO verified by the drill).

**Content checkpoint**: "Observability from day one: tracing requests across microservices with OpenTelemetry and Grafana" — screenshots of a real traced request.

## 7. Phase 6 — Advanced Features (target: ~6–8 weeks, the longest phase — by design, since it's breadth over a now-solid foundation)

**Goal**: the remaining services and the differentiating, story-worthy features.

- [ ] Card Service (FR-CARD-1 to -5).
- [ ] Loan Service, full EMI lifecycle (FR-LOAN-1 to -7).
- [ ] Fraud Service, advanced: case management UI (via Admin), risk score tuning, geo/device checks (FR-FRAUD-3, -4, -6).
- [~] Admin Service (`docs/23`): dashboard, freeze/unfreeze (admin hold a customer cannot lift), fraud case resolution and KYC review forwarding the admin's own token, audit-then-act with an immutable `admin_actions` table, `/admin` console. Verified live (`scripts/admin_reporting_demo.py`). Not done: admin search (FR-ADMIN-1, needs Search), active-customers metric, **admin MFA**.
- [ ] Analytics Service (ClickHouse) — optional, explicitly called out as "later" in the original vision; build only if it earns its place over, e.g., extending Reporting further.
- [~] Reporting Service (`docs/23`): asynchronous CSV statements (closing balance verified equal to the live ledger balance), owner-only authenticated download, admin fraud summary, `/statements` page. Not done: PDF, interest/tax report, daily closing summary, S3 storage, balance-as-of for very large accounts.
- [ ] Search Service: cross-entity admin search (FR-SEARCH-1, -2).
- [ ] Stretch, evaluate before committing (each needs its own scoped addition to `02`–`08` before starting, per `00-master-ai-instructions.md §4.5`): an AI assistant for transaction insights (a natural fit for an LLM-backed feature given the audience this project targets); multi-tenancy (only if a concrete narrative reason emerges — not a default "more impressive" addition).

**Exit criteria**: all 15 catalog services (`07-microservice-specifications.md`) are live in staging; the admin console can demonstrate the full operational story (search a customer → view their accounts/cases → take an audited action).

**NFRs touched**: the full NFR set, end to end.

**Content checkpoint**: a closing series — "What I learned building a 15-service banking platform solo" retrospective, plus feature-specific posts (fraud detection, loan amortization) as each lands.

## 8. What's explicitly not scheduled

Per `01-product-vision.md §6`, the following are out of scope for all six phases unless a future decision explicitly adds them (with the corresponding doc updates): real payment rail integration, formal regulatory certification, multi-currency/FX, native mobile apps.

## 9. Phase-gate discipline

Per `00-master-ai-instructions.md §4.5`: do not begin a Phase N+1 deliverable while a Phase N exit criterion is unmet, unless there's a stated, deliberate reason (e.g. a Phase 4 infra piece genuinely blocks finishing a Phase 3 item). Depth before breadth, in phase order — the roadmap's shape is itself a statement about engineering judgment, and skipping around undermines exactly the credibility this project exists to build (`01-product-vision.md §1`).

## 10. Traceability

Every checklist item above cites the `FR-*`/`NFR-*` it implements. When a phase completes, update this document's checkboxes in the same PR that completes the last item — not as a separate later cleanup pass.
