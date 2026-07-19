# 18 — Development Workflow

## 1. Team model

FinVerse is built by one developer working closely with an AI coding assistant (Claude Code). This document's processes are calibrated to that reality: the ceremony of a large team (multiple reviewers, formal standups, sprint planning with story points) is deliberately skipped, while the *discipline* a large team would enforce through process (review, testing, documentation-before-drift) is kept, because that discipline is the entire point of the project (`01-product-vision.md §2`).

## 2. Branching strategy

- Trunk-based: `main` is always deployable to `dev` (`15-cicd-pipeline.md §8`).
- Short-lived feature branches: `feat/<service>-<short-description>`, `fix/<service>-<short-description>`, `chore/...`, `docs/...` — merged via PR, deleted after merge.
- No long-lived parallel branches per environment — environment differences live in Terraform/config, not in divergent code branches.

## 3. Issue tracking

- GitHub Issues, organized via a GitHub Project board with columns `Backlog → Ready → In Progress → In Review → Done`, one board scoped to the whole monorepo (not per-service — at this scale, one board is more useful than fifteen).
- Every issue is scoped to a phase from `19-implementation-roadmap.md`; issues for a later phase stay in `Backlog` rather than being started early (§4).
- Issue labels: `service:<name>`, `type:feature|bug|chore|docs`, `phase:<n>`.

## 4. Definition of ready

An issue is ready to start when:
- It references the relevant `FR-*`/`NFR-*` requirement(s) or names the doc section it implements.
- It's scoped to a single service (or explicitly flagged cross-service, in which case it should probably be split).
- It belongs to the current or an earlier roadmap phase — starting Phase 4 work while Phase 2 is incomplete is a scope-discipline violation (`00-master-ai-instructions.md §4.5`) unless there's an explicit, stated reason.

## 5. Working with the AI assistant

- Every session starts with the assistant treating `00-master-ai-instructions.md` as active context — if it hasn't been read yet in a session, it's read before non-trivial work begins.
- The developer briefs the assistant with the issue/goal; the assistant checks the relevant docs (service spec, schema, API standards) before writing code, per `00-master-ai-instructions.md §4.1`.
- The assistant flags — rather than silently resolves — any ambiguity that touches money correctness, security, or a cross-service contract (`00-master-ai-instructions.md §4.6`).
- The developer reviews AI-authored changes with the same rigor as a human PR (§6) — "the AI wrote it" is not a reason to skip review; if anything it's a reason to check it more carefully on the ledger/fraud/auth-critical paths.

## 6. Code review

Solo-developer review still happens — it just happens as a deliberate self-review pass (optionally assisted by the `/code-review` or `/security-review` skill for a second, structured look) rather than a second human. The PR template (`.github/pull_request_template.md`) prompts:
- What does this change and why (link the issue/requirement)?
- Which docs did you check/update (`13-repository-structure.md §4` for new services, or the relevant spec/schema/API doc)?
- Tests added/updated? Coverage maintained?
- Any security-relevant surface touched (new endpoint, new PII field, new AWS resource)? Checklist in `11-security-standards.md §11` run?
- For UI changes: was it actually exercised in a running browser, not just built?

A PR does not merge with a red CI check, an unresolved review comment, or an unanswered checklist item.

## 7. Local development setup

1. `docker compose -f docker/docker-compose.yml up` — brings up every service, Postgres, MongoDB, Redis, OpenSearch, EventBridge-equivalent (LocalStack or a lightweight local event bus stand-in — see note below), Jaeger, Prometheus, Grafana, Loki.
2. `scripts/seed/seed.sh` — populates realistic fake data (§8 of `17-testing-strategy.md`).
3. Frontend: `cd frontend && npm install && npm run dev`.
4. A new engineer (or the developer, returning after time away) should be able to go from clone to a working local stack following only the root `README.md`, with no undocumented tribal-knowledge steps.

**Local AWS-equivalents**: EventBridge/SQS are stood in for locally via LocalStack (or, if that proves too heavy for local dev, a minimal in-process pub/sub substitute behind the same interface used in production — a decision to be finalized in Phase 1 and recorded here once made). S3 is stood in for via LocalStack or MinIO. This keeps local development fully offline-capable (NFR-PORT-1) without requiring real AWS credentials for day-to-day coding.

## 8. Commit and PR hygiene

Per `12-coding-standards.md §4` for commit message conventions. PRs are scoped to one logical change; a PR that touches both a new feature and an unrelated refactor is split. PR titles follow the same conventional-commit prefix as their primary commit.

## 9. Documentation-in-the-loop

Per `00-master-ai-instructions.md §3–4`: a PR that introduces a new endpoint, event, table, or AWS resource without a corresponding doc update is not done, regardless of whether the code works. This is checked as part of §6's review, not left to a separate documentation pass — documentation debt compounds fast in a 15-service system and the whole value proposition (`01-product-vision.md`) depends on the docs staying trustworthy.

## 10. Release cadence

No fixed release train — a "release" is simply what's currently on `main` and deployed to `dev`/`staging`, promoted to `prod` (§5 of `15-cicd-pipeline.md`) when a roadmap phase reaches a demonstrable milestone worth showing (tying back into the LinkedIn content cadence, `01-product-vision.md §7`). Tags (`v0.<phase>.<n>`) mark these milestones in git history for narrative/portfolio reference, not for strict semver compatibility guarantees (there are no external consumers of a stable API to protect).

## 11. Traceability

This workflow is the process layer that makes `00-master-ai-instructions.md`'s standing priorities and `17-testing-strategy.md`'s testing requirements actually happen on every change, not just in principle.
