# 00 — Master AI Instructions

> **Purpose.** This is the constitution for any AI assistant (Claude Code or otherwise) working on FinVerse. It is read before every non-trivial task. When a request conflicts with this document, the AI should surface the conflict rather than silently picking a side. Every other document in `docs/` is subordinate to this one on matters of process; on matters of domain content, the specific document (e.g. `08-database-design.md`) wins.

## 1. What FinVerse is, in one paragraph

FinVerse is a cloud-native digital banking platform built to demonstrate senior-level backend/platform engineering: Go microservices, event-driven architecture, CQRS where it earns its keep, Domain-Driven Design, and AWS-native infrastructure, with a SvelteKit frontend. It is a portfolio project, not a production bank — but it must be built as if it were going to production, because the entire value of the project is that it survives a senior engineer's scrutiny. Optimize for **correctness, clarity, and defensibility of decisions** over feature count.

## 2. Standing priorities, in order

When priorities conflict, resolve in this order:

1. **Money correctness.** Ledger integrity, double-entry balance, idempotency of financial operations. Never trade this away for velocity.
2. **Security.** AuthN/AuthZ, secrets handling, input validation, least privilege. See `11-security-standards.md`.
3. **Consistency with the documented architecture.** Don't quietly introduce a new pattern (a new datastore, a new sync-call between services that should be event-driven, a new auth scheme) without updating the relevant doc first.
4. **Readability / interview-defensibility.** Code and docs should hold up when the user is asked "why did you do it this way?" in an interview.
5. **Feature velocity.** Shipping the next roadmap item.

Do not silently reorder this list. If a task seems to require reordering it (e.g. "just hardcode this to ship faster"), say so explicitly and ask.

## 3. Source of truth and document hierarchy

```
00-master-ai-instructions.md      ← process rules (this file)
01-product-vision.md              ← why this exists, what "done" looks like
02-business-requirements.md       ← BRD: business capabilities, stakeholders
03-functional-requirements.md     ← FRD: what the system must do
04-non-functional-requirements.md ← NFR: how well it must do it
05-system-architecture.md         ← the big picture, C4 diagrams
06-domain-driven-design.md        ← bounded contexts, ubiquitous language
07-microservice-specifications.md ← one spec per service
08-database-design.md             ← schemas, ERDs, per-service data ownership
09-api-standards.md               ← REST/gRPC/event contract conventions
10-authentication-authorization.md
11-security-standards.md
12-coding-standards.md
13-repository-structure.md
14-aws-infrastructure.md
15-cicd-pipeline.md
16-observability.md
17-testing-strategy.md
18-development-workflow.md
19-implementation-roadmap.md
```

**Rule:** if you (the AI) are about to make a decision that a future document-reader would reasonably expect to find documented — a new endpoint, a new event type, a new table, a new AWS resource — update the relevant doc in the same change set that introduces it. Documentation drift is treated as a bug, not a nice-to-have.

## 4. How the AI should behave on this project

### 4.1 Before writing code
- Read the relevant service spec in `07-microservice-specifications.md` and the bounded context in `06-domain-driven-design.md` first. Don't infer domain rules from guesswork when they're written down.
- Check `08-database-design.md` before adding or changing a column, table, or index. Schema changes go through a migration, never a manual `ALTER` against a running environment.
- Check `09-api-standards.md` before adding an endpoint or event type. Consistency across 15 services only survives if every service follows the same envelope, error format, and naming convention.

### 4.2 While writing code
- Follow `12-coding-standards.md` without exception for Go and frontend code. If a linter rule and a convenience conflict, the linter wins; change the linter config deliberately if it's wrong, don't route around it.
- Every financial mutation (ledger entry, balance change, EMI posting) must be idempotent and wrapped in a DB transaction. See `03-functional-requirements.md §Ledger` and `08-database-design.md §Transaction Service`.
- Never call another service synchronously when an event would do — see `05-system-architecture.md §Event-Driven Communication` for the decision rule (sync = need the answer now to respond to the caller; async = everything else).
- Don't add a new AWS resource, IAM permission, or third-party dependency without checking `14-aws-infrastructure.md` and `11-security-standards.md` (least privilege).

### 4.3 Testing and verification
- Follow `17-testing-strategy.md`. Ledger, payment, and fraud logic require unit tests before the PR is considered done — not after.
- Don't claim a UI change works without actually running it (dev server + browser), per the project's standing engineering practice.
- Don't mark a task complete if tests are red, migrations are unapplied, or the build is broken.

### 4.4 Documentation and comments
- Default to no code comments. Comment only non-obvious *why* (a regulatory quirk, a workaround, an invariant).
- Do not create new top-level docs beyond the 20 listed above without asking first. If new domain knowledge needs a home, it goes inside the relevant existing doc or as a dated addendum, not a new sprawling file.
- Keep `19-implementation-roadmap.md` up to date as phases complete — it's the single place that tracks "what's actually done" versus "what's designed."

### 4.5 Scope discipline
- This is explicitly a staged build (see `19-implementation-roadmap.md`). Do not build Phase 4+ infrastructure (ECS, full AWS) while Phase 1 (local Docker Compose, Auth + Account) is still in progress, even if it seems more impressive. Depth before breadth, in phase order.
- Don't add a service, table, or AWS resource "because a real bank would have it" unless it's in the documented service catalog. Propose additions by editing the relevant doc and flagging the addition to the user — don't silently expand scope.
- Simplicity by default: no speculative abstractions, no config flags for hypothetical futures, no framework for a problem that doesn't exist yet. Three similar lines beats a premature abstraction, per standing engineering practice.

### 4.6 When the AI is unsure
- If a requirement is ambiguous and the ambiguity affects money movement, security, or a cross-service contract: **ask**, don't guess.
- If the ambiguity is a local implementation detail with no cross-cutting effect: make the reasonable, documented-convention-following call and note the decision in a commit message or PR description.
- Never silently weaken a security control (e.g., skip MFA "for now", disable a validation "temporarily") to unblock a task. Surface it instead.

## 5. Non-negotiables (hard rules)

1. No secrets, credentials, or `.env` files with real values committed to git. Ever.
2. No `--no-verify`, disabled linters, or skipped tests to force a merge.
3. No direct writes to the ledger/balance columns outside the Transaction Service's double-entry code path.
4. No destructive git operations (`push --force`, `reset --hard`, branch deletion) without explicit user confirmation in the moment.
5. No new production AWS resource created without it first appearing in `14-aws-infrastructure.md` / Terraform.
6. Every PII field (PAN, address, KYC docs) is encrypted at rest and access-logged per `11-security-standards.md`, no exceptions for "just testing."

## 6. Definition of done (applies to every task, every phase)

A task is done when:
- Code compiles/builds and passes lint.
- Relevant unit/integration tests exist and pass.
- Any new endpoint/event/table/AWS resource is reflected in the corresponding doc.
- The change was actually exercised (test run, or for UI, a real browser check) — not just "should work."
- Security and money-correctness non-negotiables (§5) are unbroken.

## 7. How this document evolves

This file changes rarely and deliberately. If the user asks to change a standing priority, a non-negotiable, or the document hierarchy, treat it as a real decision: confirm the change, then update this file in the same turn so it doesn't drift from what was actually agreed.
