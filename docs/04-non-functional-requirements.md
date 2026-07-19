# 04 — Non-Functional Requirements (NFR)

## 1. Purpose

Functional requirements (`03-functional-requirements.md`) say *what* the system does. This document says *how well*. Every NFR here should be measurable and, where practical, verified by an automated test or a load-test run (see `17-testing-strategy.md`).

## 2. Performance

| ID | Requirement |
|---|---|
| NFR-PERF-1 | Cached account balance read: P99 < 100ms. |
| NFR-PERF-2 | Uncached account balance read (DB path): P99 < 250ms. |
| NFR-PERF-3 | Fund transfer, synchronous leg (validate + fraud check + ledger post): P99 < 500ms. |
| NFR-PERF-4 | Fraud synchronous check call (gRPC): P99 < 80ms — it sits on the critical path of every payment. |
| NFR-PERF-5 | Event end-to-end propagation (publish → all interested consumers processed): P99 < 5s under normal load. |
| NFR-PERF-6 | Search query (admin console): P99 < 300ms for typeahead-style queries. |
| NFR-PERF-7 | Frontend initial page load (authenticated dashboard): < 2s on a broadband connection, measured via Lighthouse in CI. |

## 3. Scalability

| ID | Requirement |
|---|---|
| NFR-SCALE-1 | Each service must be independently horizontally scalable (stateless application tier; state lives in the datastore) — no service may hold in-memory state that would break correctness if scaled to N > 1 instances. |
| NFR-SCALE-2 | The Transaction Service must sustain at least 200 posted transactions/sec in a load test without balance drift (see `17-testing-strategy.md §Load Testing`). This is a portfolio-scale target, not a production-bank target — chosen to be demonstrably measured, not to imply real-bank throughput. |
| NFR-SCALE-3 | ECS services must be configured with auto-scaling policies (target CPU/memory or request-count based) rather than fixed task counts, even if scaled to zero-to-one in early phases for cost reasons. |
| NFR-SCALE-4 | Read-heavy paths (balance lookups, statements) must be able to scale independently of write-heavy paths — this is the primary justification for the CQRS split described in `05-system-architecture.md §CQRS`. |

## 4. Availability & Reliability

| ID | Requirement |
|---|---|
| NFR-AVAIL-1 | Target availability for customer-facing write paths (payments, transfers): 99.5% (portfolio-appropriate; documented as a target, not contractually guaranteed — there is no SLA counterparty). |
| NFR-AVAIL-2 | No single service outage may corrupt the ledger — a downstream failure (e.g. Notification down) must never block or roll back a financial transaction; it is handled via async retry/DLQ (FR-NOTIF-3/4). |
| NFR-AVAIL-3 | Every inter-service async interaction must be retryable and idempotent; every sync interaction must have a timeout and a defined fallback (fail closed for money-movement, fail open for non-critical reads like a nice-to-have UI widget). |
| NFR-AVAIL-4 | RDS must run Multi-AZ in staging/prod; automated backups with point-in-time recovery enabled (`14-aws-infrastructure.md §Backup & DR`). |
| NFR-AVAIL-5 | Recovery Point Objective (RPO): ≤ 5 minutes for transactional data. Recovery Time Objective (RTO): ≤ 1 hour for a full-region service restoration in staging/prod. |

## 5. Consistency & Data Integrity

| ID | Requirement |
|---|---|
| NFR-DATA-1 | The ledger (Transaction Service) is strongly consistent: reads that follow a committed write within the same service must observe it (no read-your-own-write violations). |
| NFR-DATA-2 | Cross-service projections (Account balance cache, Analytics, Search index) are eventually consistent, with a documented and monitored staleness bound (target: < 5s under normal load, alerting above 30s). |
| NFR-DATA-3 | The double-entry invariant (sum of all ledger legs per transaction = 0) must be enforced at the database constraint level, not only in application code. |
| NFR-DATA-4 | All monetary amounts are stored as fixed-point integers (minor units, e.g. paise) — floating point is prohibited for any monetary field, in code and in the database schema. |

## 6. Security

Full detail lives in `11-security-standards.md` and `10-authentication-authorization.md`. NFRs referenced here for completeness:

| ID | Requirement |
|---|---|
| NFR-SEC-1 | All external traffic is TLS 1.2+ only; internal service-to-service traffic within the VPC is encrypted in transit. |
| NFR-SEC-2 | All PII and monetary data is encrypted at rest (RDS encryption, S3 SSE, EBS encryption where applicable). |
| NFR-SEC-3 | No credential, API key, or secret is ever stored in source control, container images, or logs — secrets are sourced from Secrets Manager/Parameter Store at runtime. |
| NFR-SEC-4 | Every authenticated request is authorized against RBAC scopes before any handler business logic executes. |
| NFR-SEC-5 | The system must withstand the OWASP Top 10 classes of attack as verified by the security checklist in `11-security-standards.md §Verification`. |

## 7. Observability

| ID | Requirement |
|---|---|
| NFR-OBS-1 | Every request is traceable end-to-end across services via a propagated trace ID (OpenTelemetry), visible in Jaeger. |
| NFR-OBS-2 | Every service exposes Prometheus-scrapeable metrics including, at minimum: request rate, error rate, and latency histograms per endpoint (the RED method). |
| NFR-OBS-3 | Every log line is structured JSON, includes trace/span ID and service name, and is shippable to Loki without reformatting. |
| NFR-OBS-4 | Critical business events (large transaction, fraud case opened, account frozen) must be alertable, not just logged — see `16-observability.md §Alerting`. |
| NFR-OBS-5 | Mean time to detect (MTTD) a fully-broken critical path (e.g. Transaction Service down) via automated alerting: < 5 minutes. |

## 8. Maintainability & Extensibility

| ID | Requirement |
|---|---|
| NFR-MAINT-1 | A new engineer (or the user, returning after a break) must be able to understand a service's purpose and contracts from its `07-microservice-specifications.md` entry and README without reading all of its code. |
| NFR-MAINT-2 | Adding a new domain event consumer must not require changes to the publishing service (publish-subscribe via EventBridge, not hardcoded destinations). |
| NFR-MAINT-3 | All services follow the same project layout and conventions (`12-coding-standards.md`) so switching context between services costs no additional ramp-up. |
| NFR-MAINT-4 | Test coverage on domain/business logic (ledger, fraud rules, EMI calculation) must be ≥ 80%, enforced in CI. |

## 9. Usability

| ID | Requirement |
|---|---|
| NFR-UX-1 | Core customer flows (login, check balance, transfer money, view statement) must be completable without documentation — self-evident UI. |
| NFR-UX-2 | The frontend must be responsive down to mobile viewport widths (no native app in scope, but the web UI must not break on a phone). |
| NFR-UX-3 | Error messages shown to customers must be actionable and non-technical (map internal error codes to human copy — see `09-api-standards.md §Error Format`). |
| NFR-UX-4 | The UI must meet WCAG 2.1 AA for color contrast and keyboard navigation on primary flows. |

## 10. Compliance & Data Protection (spirit-of, per scope boundary in `01-product-vision.md §6`)

| ID | Requirement |
|---|---|
| NFR-COMP-1 | Every access to a customer's PII or financial record by an admin/support role is itself an auditable event (who viewed what, when). |
| NFR-COMP-2 | Data retention: audit records retained indefinitely; customer data retained until account closure + a documented grace period; deletion requests honored per a documented process (right-to-erasure spirit, adapted for a non-production system). |
| NFR-COMP-3 | KYC documents are stored with restricted IAM access (least privilege) and are never included in application logs or error messages. |

## 11. Cost

| ID | Requirement |
|---|---|
| NFR-COST-1 | The AWS footprint must be scalable down to near-zero cost when not actively being demoed (e.g. ECS services scaled to 0, RDS stopped or downsized) — see `14-aws-infrastructure.md §Cost Controls`. |
| NFR-COST-2 | No managed service is adopted whose minimum cost is disproportionate to the project's demo/portfolio nature without an explicit, documented justification. |

## 12. Portability

| ID | Requirement |
|---|---|
| NFR-PORT-1 | The full system must run locally via Docker Compose with functional parity (minus scale) to the AWS deployment, so development never strictly requires cloud access. |

## 13. Traceability

NFRs are verified via: automated tests (unit/integration/contract — `17-testing-strategy.md`), load tests (k6, `17-testing-strategy.md §Load Testing`), CI gates (`15-cicd-pipeline.md`), and observability dashboards (`16-observability.md`). Each roadmap phase in `19-implementation-roadmap.md` lists which NFRs it's expected to satisfy.
