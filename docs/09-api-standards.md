# 09 — API Standards

## 1. Scope

Three contract types exist in FinVerse and each has its own convention: **external REST** (customer/admin-facing, via API Gateway), **internal gRPC** (the small set of latency-sensitive sync calls identified in `05-system-architecture.md §4`), and **domain events** (EventBridge/SQS). This document is the single place all three are standardized so that 15 services feel like one API, not fifteen.

## 2. External REST conventions

### 2.1 Versioning and base path
`https://api.finverse.dev/api/v1/<resource>`. Version bumps to `v2` only on a breaking change; additive changes (new optional field, new endpoint) never require a version bump.

### 2.2 Resource naming
- Plural nouns, kebab-case for multi-word resources: `/accounts`, `/standing-instructions`.
- Nesting reflects ownership, max two levels: `/accounts/{accountId}/statements`.
- Actions that aren't naturally RESTful use a verb sub-resource: `POST /accounts/{id}/freeze`, `POST /payments/{id}/reverse`.

### 2.3 Request/response envelope

Success:
```json
{
  "data": { "...": "resource or array" },
  "meta": { "requestId": "uuid", "traceId": "..." }
}
```
List responses add pagination to `meta`:
```json
{ "data": [ ... ], "meta": { "page": 1, "pageSize": 20, "totalCount": 143, "requestId": "...", "traceId": "..." } }
```

### 2.4 Error format

```json
{
  "error": {
    "code": "ACCOUNT_FROZEN",
    "message": "This account is currently frozen and cannot accept debits.",
    "details": {},
    "requestId": "uuid",
    "traceId": "..."
  }
}
```
- `code` is a stable, machine-readable, SCREAMING_SNAKE_CASE identifier — frontend code and tests key off `code`, never off `message` text.
- `message` is human-readable and safe to show a customer (NFR-UX-3) — it never leaks stack traces, SQL, or internal identifiers.
- Every service maintains its own error code registry in `<service>/docs/error-codes.md`; codes are namespaced by domain concept, not by service, so `ACCOUNT_FROZEN` means the same thing regardless of which service happens to return it.

### 2.5 HTTP status code mapping

| Situation | Status |
|---|---|
| Success (read, or write with body) | 200 |
| Success (resource created) | 201 |
| Success (accepted, async processing) | 202 |
| Success (no body, e.g. delete) | 204 |
| Client sent invalid input | 400 |
| Not authenticated | 401 |
| Authenticated but not authorized (RBAC) | 403 |
| Resource not found or not owned by caller | 404 (never leak existence of another customer's resource via a 403) |
| Conflict (e.g. duplicate idempotency key with different payload) | 409 |
| Business rule violation (e.g. insufficient balance, frozen account) | 422 |
| Rate limited | 429 |
| Unexpected server error | 500 |
| Downstream dependency unavailable / circuit open | 503 |

### 2.6 Idempotency

Every state-changing endpoint that has financial or otherwise non-repeatable side effects requires an `Idempotency-Key` header. The server stores `(caller, key) → result` for at least 24 hours; a repeated key with an identical payload returns the original result (200/201, not a new side effect); a repeated key with a *different* payload returns `409 IDEMPOTENCY_KEY_CONFLICT`. This directly implements FR-X-1 and backs FR-TXN-3.

### 2.7 Pagination, filtering, sorting

- Cursor-based pagination for high-volume lists (transaction history): `?cursor=<opaque>&limit=50`.
- Offset-based (`?page=&pageSize=`) permitted only for small, admin-facing, non-high-volume lists.
- Filtering via explicit query params (`?status=COMPLETED&from=2026-01-01`), never a generic query-DSL param.
- Sorting via `?sort=-createdAt` (`-` prefix = descending); default sort is always documented per endpoint, never left implicit.

### 2.8 Authentication header

`Authorization: Bearer <JWT>` on every request except `/auth/register`, `/auth/login`, `/auth/refresh`, and health checks. Full detail in `10-authentication-authorization.md`.

### 2.9 Rate limiting

Every response includes `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`. Limits are enforced at the API Gateway (coarse, per-IP/per-token) and, for money-movement endpoints specifically, re-checked inside Fraud Service's velocity rules (finer-grained, business-aware) — the two are complementary, not redundant (`11-security-standards.md §Rate Limiting`).

### 2.10 Contract location

Each service publishes an OpenAPI 3.1 spec at `services/<service>/api/openapi.yaml`, which is the source of truth for that service's REST contract — hand-written route comments in code must not drift from it (CI lints for drift, see `15-cicd-pipeline.md`).

## 3. Internal gRPC conventions

Reserved for the specific calls identified in `05-system-architecture.md §4` (Payment↔Fraud, Payment↔Transaction, Wallet↔Transaction, Loan↔Payment, Loan↔Fraud) — not a default for all internal calls.

- Proto files live in `shared/proto/<domain>/v1/*.proto`, versioned by directory, generated code checked in per-language.
- Every RPC takes a request message with an explicit `idempotency_key` field where it triggers a side effect, mirroring §2.6.
- Every RPC has a documented deadline; callers set a `context.WithTimeout` matching the NFR latency budget for that call (e.g. 80ms for `FraudService.CheckTransaction`) and treat deadline-exceeded as a definite failure requiring the caller's own retry/circuit-breaker logic, never a silent retry inside the callee.
- Errors use standard gRPC status codes (`INVALID_ARGUMENT`, `FAILED_PRECONDITION`, `UNAVAILABLE`, etc.), with a structured detail message carrying the same `code`/`message` shape as §2.4 for consistency when logged or surfaced.

Example (`shared/proto/fraud/v1/fraud.proto` sketch):
```protobuf
syntax = "proto3";
package fraud.v1;

service FraudService {
  rpc CheckTransaction(CheckTransactionRequest) returns (CheckTransactionResponse);
}

message CheckTransactionRequest {
  string idempotency_key = 1;
  string customer_id = 2;
  int64 amount_minor = 3;
  string currency = 4;
  string transaction_type = 5;
}

message CheckTransactionResponse {
  Verdict verdict = 1;
  string case_id = 2; // set only when verdict == REVIEW or BLOCK
}

enum Verdict {
  VERDICT_UNSPECIFIED = 0;
  ALLOW = 1;
  REVIEW = 2;
  BLOCK = 3;
}
```

## 4. Domain event conventions

### 4.1 Envelope

Every event published to EventBridge uses this envelope, regardless of publishing service:

```json
{
  "eventId": "uuid",
  "eventType": "TransactionPosted",
  "version": 1,
  "occurredAt": "2026-07-19T10:15:00Z",
  "sourceService": "transaction-service",
  "traceId": "...",
  "data": { "...": "event-specific payload" }
}
```

- `eventId` is the dedupe key every consumer must check before processing (FR-NOTIF-5 and its equivalent in every other consumer).
- `version` increments only on a breaking payload change; consumers should ignore unknown fields (tolerant reader pattern) rather than fail on additive changes.
- `traceId` propagates the originating request's trace so the whole fan-out is visible as one trace in Jaeger (NFR-OBS-1).

### 4.2 Naming

Past tense, PascalCase, domain-meaningful: `TransactionPosted`, not `PostTransaction` or `transaction_created`. The full catalog lives in `06-domain-driven-design.md §5` — a new event type is added there before it's added to code.

### 4.3 EventBridge routing

One custom Event Bus (`finverse-domain-events`) per environment. Rules match on `eventType`; each interested consumer service owns one SQS queue subscribed via its own rule, plus a DLQ with a redrive policy (max receive count 5, then DLQ). No consumer ever subscribes directly to another consumer's queue.

### 4.4 Schema evolution

Additive-only within a version (new optional field). A field removal, type change, or semantic change requires a new `eventType` version and a deprecation window during which the publisher emits both, documented as a dated entry in `06-domain-driven-design.md`.

## 5. Cross-cutting: request tracing

Every REST request, gRPC call, and event carries/propagates a W3C Trace Context (`traceparent` header, or the `traceId` envelope field for events), instrumented via OpenTelemetry per `16-observability.md`. This is not optional per service — a service without trace propagation breaks the platform-wide "one story per request" observability goal.

## 6. Traceability

REST/gRPC endpoints listed per-service in `07-microservice-specifications.md §Key APIs` must have a corresponding OpenAPI/proto definition following this document's conventions before being considered implemented (`00-master-ai-instructions.md §4.1`).
