# 16 — Observability

## 1. Philosophy

Observability is built in from each service's first commit, not retrofitted (`00-master-ai-instructions.md §4.3`). The concrete bar: given a customer complaint ("my transfer didn't show up"), it must be possible to find the specific request in Jaeger by trace ID, see every service it touched, and read the correlated logs from each — without SSHing into anything or guessing.

## 2. The three pillars and their tools

| Pillar | Tool | Purpose |
|---|---|---|
| Traces | OpenTelemetry SDK → OTel Collector → Jaeger | Follow one request across services |
| Metrics | OpenTelemetry SDK / Prometheus client → Prometheus → Grafana | Aggregate health and performance over time |
| Logs | Zap (structured JSON) → Promtail → Loki → Grafana | Detailed, searchable record of what happened, correlated to traces |

All three correlate via `trace_id`: every log line and every metric exemplar carries it, so Grafana can jump from "this latency spike" → exemplar trace → Jaeger, or from "this error log" → its trace → every other service's logs for that same request.

## 3. Tracing

- Every service is instrumented with the OpenTelemetry Go SDK; Gin/gRPC middleware (`shared/go/otelmw`) auto-creates a span per incoming request and propagates `traceparent` to outgoing calls (HTTP, gRPC, and the event envelope's `traceId` field per `09-api-standards.md §5`).
- Span naming: `<verb> <resource>` for HTTP (`POST /payments`), `<Service>/<Method>` for gRPC (`FraudService/CheckTransaction`).
- Span attributes include: `customer_id` (never PII beyond the ID), `account_id`, `transaction_id` where applicable — enough to filter Jaeger by business entity, not just by service name.
- Sampling: 100% in dev/staging (low volume, maximum debuggability); tail-based sampling in prod favoring traces that include an error or exceed a latency threshold, at a base rate sufficient to catch real patterns without unbounded storage cost.

## 4. Metrics

- Every service exposes `/metrics` (Prometheus format). The RED method is the baseline for every endpoint: **R**ate, **E**rrors, **D**uration — via `shared/go/otelmw`'s automatic histogram/counter instrumentation, so no handler has to remember to instrument itself.
- Business metrics beyond RED, per service, are added deliberately where they matter for the NFRs in `04-non-functional-requirements.md`: e.g. Transaction Service exports `finverse_ledger_transactions_posted_total`, `finverse_ledger_balance_check_failures_total` (should always be zero — see §6); Fraud exports `finverse_fraud_check_duration_seconds` (tracked against the 80ms budget) and `finverse_fraud_cases_opened_total`.
- Metric naming: `finverse_<service>_<noun>_<unit>_<total|suffix>` (Prometheus convention), consistent across all 15 services so a Grafana dashboard template works unmodified against any of them.

## 5. Logging

- Every log line: JSON, includes `service`, `trace_id`, `span_id`, `level`, `msg`, plus structured fields (`12-coding-standards.md §1.5`).
- Promtail ships container stdout (ECS awslogs driver → CloudWatch Logs in AWS; Promtail tails Docker logs directly in local Compose) into Loki.
- Retention: 30 days hot in Loki for staging/prod, matching a portfolio project's realistic debugging window; CloudWatch Logs (the AWS-native backstop, §7) retains per a documented, cost-conscious policy (e.g. 14 days) since Loki is the primary query surface.

## 6. Dashboards (Grafana)

| Dashboard | Audience | Contents |
|---|---|---|
| Platform overview | Everyone | Request rate/error rate/latency across all services, one row per service |
| Ledger health | Engineering | Transactions posted/sec, balance-check-failure count (alarm if non-zero — NFR-DATA-3), daily closing status |
| Payment funnel | Engineering | Initiated → fraud-checked → posted → completed counts, so a drop-off is visible at a glance |
| Fraud & risk | Engineering + fraud analyst persona | Cases opened/resolved, risk score distribution, `CheckTransaction` latency vs. the 80ms budget |
| Infra | Engineering | ECS task health, RDS CPU/connections, Redis memory, SQS queue depth and DLQ counts |

This is deliberately separate from the **Admin Service dashboard** (`07-microservice-specifications.md §13`) — Grafana is an engineering observability tool over infrastructure/service metrics; the Admin dashboard is a business/operations tool over domain data (customers, accounts, cases). They serve different audiences and different data, even though both are "a dashboard."

## 7. AWS-native backstop

CloudWatch is configured as the AWS-native layer underneath the self-hosted stack: Container Insights for ECS-level metrics, CloudWatch Alarms on infrastructure-level signals (RDS storage/CPU, SQS DLQ depth, ALB 5xx rate) that must page even if the Prometheus/Grafana stack itself is down — an observability stack should not have itself as its only single point of failure.

## 8. Alerting

| Alert | Condition | Severity |
|---|---|---|
| Ledger imbalance | Any `finverse_ledger_balance_check_failures_total` increment | Critical — pages immediately, per NFR-DATA-3 this should never happen |
| DLQ depth > 0 | Any per-service DLQ has ≥1 message | High |
| Fraud check latency P99 > 80ms sustained 5 min | Breach of NFR-PERF-4 | High |
| Service error rate > 5% over 5 min | Any service | High |
| Service down (health check failing) | Any service | Critical |
| RDS storage > 80% | Any environment | Medium |
| AWS budget threshold crossed | Per `14-aws-infrastructure.md §9` | Medium |

At this project's scale, "paging" means a notification (email/SMS/Slack webhook via SNS), not a formal on-call rotation — but the alert rules and severities are defined as if they would feed one, so the muscle memory and the wiring are real (NFR-OBS-4/5).

## 9. Health and readiness

Every service exposes `/health` (liveness — is the process up) and `/ready` (readiness — can it serve traffic, i.e. DB connection pool healthy) separately, per FR-X-4. ECS uses `/health` for container health checks; the ALB target group uses `/ready` for routing decisions — a service that's up but can't reach its DB should stop receiving traffic without being killed and restarted (which wouldn't fix a DB connectivity issue anyway).

## 10. Local development parity

The full observability stack (OTel Collector, Jaeger, Prometheus, Grafana, Loki) runs in `docker/docker-compose.yml` alongside the services (`13-repository-structure.md §2`), so tracing/metrics/logs are inspectable locally exactly as in staging/prod — an engineer should never have to deploy to staging just to see whether their tracing instrumentation works.

## 11. Traceability

This document implements NFR-OBS-1 through NFR-OBS-5 (`04-non-functional-requirements.md §7`) and is the concrete mechanism behind the "definition of done" observability expectation in `00-master-ai-instructions.md §6`.
