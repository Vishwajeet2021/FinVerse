# 12 — Coding Standards

## 1. Go services

### 1.1 Project layout (every service, identically)

```
services/<service-name>/
  cmd/
    server/main.go          # composition root: wiring only, no business logic
  internal/
    domain/                 # entities, value objects, domain errors — no framework imports
    service/                # business/use-case logic, orchestrates domain + repository
    repository/             # Postgres/Mongo/Redis access, implements interfaces defined in domain/service
    transport/
      http/                 # Gin handlers, request/response DTOs, route registration
      grpc/                 # gRPC server implementation (only for services with internal RPC)
    config/                 # env/config loading
  db/
    migrations/              # golang-migrate SQL files
  api/
    openapi.yaml              # REST contract (09-api-standards.md)
  Dockerfile
  go.mod
```
`internal/domain` never imports `internal/transport` or a framework package — dependencies point inward (handlers depend on services, services depend on domain interfaces, repositories implement them). This is what keeps `07-microservice-specifications.md`'s "read the spec, not the code" promise (NFR-MAINT-1) actually true — domain logic is legible without wading through Gin routing.

### 1.2 Naming

- Packages: short, lowercase, no underscores (`ledger`, not `ledger_service`).
- Exported identifiers: `PascalCase`; unexported: `camelCase`. No Hungarian-notation prefixes.
- Errors are values named `Err...` (`ErrAccountFrozen`), defined in `internal/domain`, and checked with `errors.Is`, never string-matched.
- Interfaces are named for what they do, not suffixed `-Interface` (`type Repository interface { ... }`, not `RepositoryInterface`).

### 1.3 Error handling

- Every error crossing a function boundary is wrapped with context: `fmt.Errorf("posting ledger entry: %w", err)` — never returned bare from more than one level down.
- Domain errors (business rule violations — insufficient balance, frozen account) are distinct types from infrastructure errors (DB timeout, network failure); handlers map the former to `422` and the latter to `503`/`500` per `09-api-standards.md §2.5`, and this mapping lives in one place per service, not scattered per-handler.
- No `panic` in request-handling code paths except for truly unrecoverable startup failures (e.g. missing required config) — a panic in a request handler is a bug, and `recover` middleware exists only as a last-resort safety net, not a substitute for correct error handling.

### 1.4 Context and cancellation

- Every function that does I/O takes `context.Context` as its first argument and propagates it — no `context.Background()` calls buried inside business logic.
- Timeouts are set at the call site that has the NFR budget in mind (e.g. the Payment→Fraud gRPC call sets `context.WithTimeout(ctx, 80*time.Millisecond)` per NFR-PERF-4), not defaulted deep inside a shared client.

### 1.5 Logging (Zap)

- Structured fields only, never `fmt.Sprintf`-built log messages: `logger.Info("payment completed", zap.String("payment_id", id), zap.Int64("amount_minor", amt))`.
- Every log line inside a request/event-handling path includes `trace_id` — injected once via middleware, never manually threaded per call site.
- Log levels: `Debug` (verbose, dev-only detail), `Info` (business-meaningful events — a payment completed, a case opened), `Warn` (recovered/handled abnormal conditions), `Error` (something failed that shouldn't have). No `Fatal`/`os.Exit` outside `cmd/server/main.go` startup.

### 1.6 Monetary values

- Always `int64` minor units in Go code, matching the DB representation (`08-database-design.md §3`) — never `float64` for money, anywhere, including in-memory intermediate calculations.
- A shared `shared/money` package provides a `Money{AmountMinor int64; Currency string}` type with safe arithmetic (overflow-checked) used by every service that touches an amount — this is the one place "add two money values" is implemented, so it's implemented correctly once.

### 1.7 Concurrency

- Goroutines started for fire-and-forget work (e.g. publishing an outbox event) must have their own error handling and logging — an error in a detached goroutine that's silently swallowed is a production incident waiting to happen.
- Shared mutable state is avoided in favor of passing values; where genuinely needed, protected explicitly with `sync.Mutex`/channels — no reliance on accidental goroutine scheduling behavior for correctness.

### 1.8 Linting

`golangci-lint` runs in CI with, at minimum: `govet`, `staticcheck`, `errcheck`, `gosec`, `unused`, `ineffassign`. A lint failure blocks merge (`15-cicd-pipeline.md`); linter config is checked in at `services/<service>/.golangci.yml`, inherited from a shared root config so all services enforce the same bar.

### 1.9 Testing conventions

Full strategy in `17-testing-strategy.md`; the code-level convention is table-driven tests as the default shape:

```go
func TestCalculateEMI(t *testing.T) {
    cases := []struct {
        name      string
        principal int64
        rateBps   int
        tenure    int
        wantEMI   int64
    }{
        {"basic 12-month loan", 100000_00, 1200, 12, 8885_00},
        // ...
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := CalculateEMI(tc.principal, tc.rateBps, tc.tenure)
            require.Equal(t, tc.wantEMI, got)
        })
    }
}
```

## 2. Frontend (SvelteKit / TypeScript)

- `strict: true` in `tsconfig.json`, no exceptions; no `any` without an explicit inline comment justifying it (rare — e.g. a third-party type gap).
- Component structure: presentational components take props and emit events; data-fetching lives in route `+page.ts`/`load` functions or TanStack Query hooks, never fetched ad-hoc inside a deeply nested component.
- API calls go through a single typed client module (`src/lib/api/`) generated/kept in sync with each service's OpenAPI spec — no hand-rolled `fetch` calls scattered across components.
- Styling is Tailwind utility classes; a shared `src/lib/components/ui/` holds the small set of reusable primitives (button, input, card) so visual consistency doesn't depend on every page reinventing spacing/color choices.
- State that needs to persist across navigation uses Svelte stores; state that's purely server data (account balance, transaction list) goes through TanStack Query, not a duplicated store — one cache, one source of truth for server data.
- ESLint + Prettier enforced in CI, same "lint failure blocks merge" rule as Go.

## 3. Comments policy

Default to no comments. A comment is added only when it explains a *why* that the code cannot express on its own: a regulatory or business-rule reason for an otherwise-odd branch, a workaround for a specific upstream bug (with a reference), or a non-obvious invariant a future editor could accidentally break. Comments never restate what a well-named function or variable already says.

## 4. Git commit conventions

Conventional-commit-style prefixes: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`. Commit messages explain *why*, not a restatement of the diff. Scoped to one logical change — a commit that touches Payment Service's business logic and unrelated frontend styling in the same commit should be split.

## 5. Definition of "idiomatic" for this project

When in doubt between "the clever way" and "the way the Go/Svelte community would recognize immediately," choose the latter. This project's credibility comes from being obviously well-engineered to someone skimming it, not from cleverness that requires an explanation to appreciate.

## 6. Traceability

These standards are enforced automatically where possible (§1.8, §2 linting) and reviewed manually otherwise, per `18-development-workflow.md §Code Review`. They apply uniformly across all 15 services and the frontend — a deviation in one service is a bug in that service, not a local style choice.
