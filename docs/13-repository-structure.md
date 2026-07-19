# 13 — Repository Structure

## 1. Monorepo rationale

FinVerse uses a single monorepo (`finverse/`) rather than one repo per service. At this team size (effectively one developer + AI assistance), a monorepo means: one CI system to maintain, atomic cross-service changes (e.g. updating a shared proto and both services that use it in one PR), and one place a reviewer (or recruiter browsing the repo) can see the whole system. Path-filtered CI (`15-cicd-pipeline.md §Monorepo Triggers`) keeps per-service build times reasonable despite the shared repo.

## 2. Full tree

```
finverse/
├── frontend/                        # SvelteKit app
│   ├── src/
│   │   ├── routes/                  # SvelteKit file-based routing
│   │   ├── lib/
│   │   │   ├── api/                 # typed API clients per service
│   │   │   ├── components/
│   │   │   │   └── ui/              # shared primitives (button, card, input)
│   │   │   ├── stores/
│   │   │   └── utils/
│   │   └── app.html
│   ├── static/
│   ├── tests/                       # Playwright E2E
│   ├── package.json
│   └── svelte.config.js
│
├── services/
│   ├── auth-service/
│   ├── customer-service/
│   ├── account-service/
│   ├── wallet-service/
│   ├── transaction-service/
│   ├── payment-service/
│   ├── card-service/
│   ├── loan-service/
│   ├── fraud-service/
│   ├── notification-service/
│   ├── audit-service/
│   ├── admin-service/
│   ├── analytics-service/
│   ├── reporting-service/
│   └── search-service/
│       # each following the internal layout defined in 12-coding-standards.md §1.1
│
├── shared/
│   ├── proto/                       # gRPC contracts, versioned by directory (09-api-standards.md §3)
│   │   ├── fraud/v1/
│   │   └── ledger/v1/
│   ├── go/                          # shared Go modules: money, crypto, logging, otel middleware
│   │   ├── money/
│   │   ├── crypto/
│   │   ├── logging/
│   │   └── otelmw/
│   └── events/                      # canonical JSON Schema per domain event (06-domain-driven-design.md §5)
│
├── docker/
│   ├── docker-compose.yml            # full local stack: all services + Postgres + Mongo + Redis + OpenSearch + Jaeger + Prometheus + Grafana
│   ├── docker-compose.override.yml   # local-only overrides (hot reload, exposed debug ports)
│   └── otel-collector-config.yaml
│
├── terraform/
│   ├── modules/                      # reusable modules: ecs-service, rds-instance, sqs-queue, etc.
│   ├── envs/
│   │   ├── dev/
│   │   ├── staging/
│   │   └── prod/
│   └── README.md
│
├── kubernetes/                       # reserved; not used while ECS Fargate is the deploy target
│                                      # (see 05-system-architecture.md §10 — kept empty/absent until/unless
│                                      # a documented decision changes the orchestrator)
│
├── docs/                             # the 20 foundational documents (this directory)
│   ├── 00-master-ai-instructions.md
│   ├── ... (01 through 19)
│   ├── architecture/                 # exported diagrams (png/svg) referenced by the docs above
│   ├── diagrams/                     # source diagram files (mermaid/drawio) for architecture/
│   ├── runbooks/                     # incident runbooks (11-security-standards.md §10)
│   └── adr/                          # dated architecture decision records beyond the summary table in 05-system-architecture.md §10
│
├── postman/
│   └── finverse.postman_collection.json
│
├── .github/
│   ├── workflows/                    # one workflow per service + shared workflows (15-cicd-pipeline.md)
│   ├── ISSUE_TEMPLATE/
│   └── pull_request_template.md
│
├── scripts/                          # one-off dev scripts (seed data, local cert generation)
│
├── .golangci.yml                      # root lint config, inherited per-service
├── .editorconfig
├── LICENSE
└── README.md
```

## 3. Directory ownership rules

- **`services/<name>/`** is the only place that service's code lives. No service reaches into another service's directory to import code directly — cross-service reuse goes through `shared/`.
- **`shared/go/`** contains only genuinely cross-cutting, business-logic-free code (money arithmetic, crypto helpers, logging/otel middleware). Domain logic never lives in `shared/` — if two services seem to need the same domain logic, that's a sign of a bounded-context boundary question to resolve in `06-domain-driven-design.md`, not a reason to extract a shared domain package.
- **`shared/proto/`** and **`shared/events/`** are the source of truth for internal contracts; changing a message here is a breaking-change decision that follows `09-api-standards.md §4.4`.
- **`docs/`** is updated in the same change set as the code it describes, per `00-master-ai-instructions.md §3`.
- **`terraform/modules/`** holds reusable infra patterns; `terraform/envs/<env>/` composes them per environment — no copy-pasted resource blocks across environments.

## 4. Adding a new service — checklist

1. Add its entry to `06-domain-driven-design.md` (bounded context) and `07-microservice-specifications.md` (spec) first.
2. Scaffold `services/<name>/` per the layout in `12-coding-standards.md §1.1`.
3. Add its schema to `08-database-design.md` and its migrations under `services/<name>/db/migrations/`.
4. Add its OpenAPI/proto contract per `09-api-standards.md`.
5. Add a Dockerfile, a Compose entry in `docker/docker-compose.yml`, and a GitHub Actions workflow under `.github/workflows/`.
6. Add its Terraform module usage under `terraform/envs/<env>/`.
7. Update `19-implementation-roadmap.md` to reflect which phase it belongs to (it should already be scoped there if the roadmap was followed — this step is a consistency check, not a new decision).

## 5. Naming conventions

- Service directories: `<domain>-service` (matches the service names used throughout `07-microservice-specifications.md`).
- Terraform resources: `finverse-<env>-<service>-<resource>` (e.g. `finverse-staging-payment-ecs-service`).
- Docker Compose service names match the directory name minus `-service` is **not** used — keep the full `<domain>-service` name everywhere for grep-ability.

## 6. Traceability

This structure is the physical realization of the bounded contexts in `06-domain-driven-design.md` and the deploy targets in `14-aws-infrastructure.md`. A structural change here (new top-level directory, new service directory) is itself a decision that should be reflected back into this document.
