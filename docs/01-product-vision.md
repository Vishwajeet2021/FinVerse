# 01 — Product Vision & Business Goals

## 1. Vision statement

> FinVerse is a cloud-native digital banking platform, architected the way a modern fintech would build one: Domain-Driven Design, Event-Driven Architecture, Microservices, CQRS where it earns its keep, and AWS-native infrastructure — built as a public, production-quality demonstration of senior backend and platform engineering.

It is not "a banking app." It is a system that answers, concretely, the questions a senior engineer or hiring manager would ask in an interview: *why microservices here, why events there, how do you guarantee the ledger balances, how do you know it's fast enough, how do you know it's secure.*

## 2. Why this project exists

The primary goal is career capital: the strongest possible public proof of engineering ability for senior backend / platform engineering roles. Two things make banking a good vehicle for that:

- **Money forces rigor.** Ledgers, idempotency, consistency, and fraud controls are hard to fake — either the double-entry books balance under concurrent load or they don't. This is a domain where hand-waving is visible.
- **Banking naturally exercises the full stack of "serious" system design**: many bounded contexts, real consistency tradeoffs (strong vs. eventual), async workflows, observability needs, and compliance-shaped constraints (audit trails, KYC, data protection) — without requiring an actual banking license or real money.

## 3. Target audience

| Audience | What they're looking for | Where they meet FinVerse |
|---|---|---|
| Hiring managers / senior engineers reviewing a portfolio | Evidence of architectural judgment, not just working code | GitHub repo, README, architecture docs |
| Interviewers doing system design rounds | A real system to interrogate ("what happens if this service is down?") | Live demo + docs, this project as talking point |
| LinkedIn technical audience | Genuine engineering narrative, diagrams, benchmarks | LinkedIn Content Strategy (see §7) |
| The user (developer) themself | A vehicle to actually learn Go, AWS, EDA, DDD at production depth, not tutorial depth | The build process itself |

There is no real end-customer audience — FinVerse does not take real money or real customers. Every "customer" persona below exists to keep functional requirements honest, not because there's a go-to-market.

## 4. Product goals

1. **Architectural credibility.** Every service boundary, datastore choice, and communication pattern must have a written rationale (see `06-domain-driven-design.md`, `05-system-architecture.md`) that survives a "why not just a monolith?" question.
2. **Operational credibility.** The system is observable, testable, and deployable the way a real fintech's would be — tracing, metrics, logs, CI/CD, IaC — not just "it runs on my machine."
3. **Domain correctness.** Double-entry ledger balances at all times; money is never created or destroyed by a bug; every financial state transition is auditable.
4. **Narrative value.** The build process produces artifacts (diagrams, benchmarks, write-ups) usable for LinkedIn content and interview conversation as it happens, not reconstructed after the fact.
5. **Personal skill growth.** The user should come out the other side genuinely fluent in Go, AWS, EDA, and distributed-systems tradeoffs — not just having assembled a checklist of buzzwords.

## 5. Success metrics

| Metric | Target |
|---|---|
| Services running end-to-end locally via Docker Compose | All Phase 1–3 services (see `19-implementation-roadmap.md`) |
| Ledger correctness under concurrent load | Zero balance-drift in load tests (k6); double-entry invariant holds at all times |
| Test coverage on domain/business logic (ledger, payments, fraud) | ≥ 80% |
| P99 latency, account balance read (cached) | < 100ms |
| P99 latency, fund transfer (write path) | < 500ms end-to-end (sync leg only; async settlement may take longer, by design) |
| CI pipeline | Green build required to merge; no `--no-verify` merges |
| Documentation | All 20 foundational docs kept current as the system evolves (§4.4 of `00-master-ai-instructions.md`) |
| Public artifacts | ≥ 6 LinkedIn posts tied to real milestones, each with a diagram/snippet/benchmark |

## 6. Scope boundaries

**In scope:**
- Simulated versions of NEFT/RTGS/IMPS/UPI (protocol-shaped, not integrated with real payment rails or banks).
- Real double-entry ledger logic, real fraud-rule logic, real MFA/JWT auth — the *engineering* is real even though the money and payment rails are simulated.
- AWS deployment of the full system (dev/staging, and a demonstrable prod-shaped environment), cost-managed (see `14-aws-infrastructure.md §Cost Controls`).

**Out of scope (explicitly, to prevent scope creep):**
- Real money movement, real banking licenses, real integration with NPCI/SWIFT/card networks.
- Regulatory certification (PCI-DSS, RBI/Basel compliance as formal audits) — FinVerse follows the *spirit* of these controls for credibility, but does not pursue formal certification.
- Multi-currency / cross-border FX (unless added as a clearly-scoped later phase).
- Native mobile apps (web-first via SvelteKit; responsive, not native iOS/Android).

## 7. Relationship to content strategy

Documentation and build milestones double as source material for public technical writing. Each roadmap phase completion (see `19-implementation-roadmap.md`) is a natural checkpoint for a LinkedIn post: an architecture decision, a benchmark, a diagram, or a lesson learned. This is a first-class output of the project, not an afterthought — when a phase finishes, producing the write-up material (diagram export, benchmark numbers, a short "why" narrative) is part of "done," alongside the working code.

## 8. Timeline expectation

4–6 months of part-time build effort, staged per `19-implementation-roadmap.md`. The timeline is a planning aid, not a hard deadline — depth and correctness (§2 of `00-master-ai-instructions.md`) take priority over hitting a date.

## 9. Non-goals for the AI assistant

When working on this project, an AI assistant should not:
- Treat this as "just a CRUD app" and reach for shortcuts that a real fintech engineering team wouldn't take (see `00-master-ai-instructions.md §5`).
- Add services, features, or AWS resources not justified by this vision or the documented roadmap, in the name of impressiveness — impressiveness comes from depth and correctness, not surface area.
