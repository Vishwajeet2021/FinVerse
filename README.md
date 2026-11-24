# FinVerse

A cloud-native digital banking platform, architected the way a modern fintech would build one: Domain-Driven Design, Event-Driven Architecture, Microservices, CQRS where it earns its keep, and AWS-native infrastructure. Built as a public, production-quality demonstration of senior backend and platform engineering — see [docs/01-product-vision.md](docs/01-product-vision.md) for why.

## Start here

This repository is documentation-first. Before writing or reviewing code, read:

1. [docs/00-master-ai-instructions.md](docs/00-master-ai-instructions.md) — the standing rules for how this project is built (applies to humans and AI assistants alike).
2. [docs/19-implementation-roadmap.md](docs/19-implementation-roadmap.md) — what's built, what's next, phase by phase.
3. The rest of `docs/` as needed — each of the 20 foundational documents covers one concern (architecture, DDD, database design, API standards, security, CI/CD, etc.) and is the source of truth for that concern.

## Current status

Phase 1 (Foundation) is in progress. See [docs/19-implementation-roadmap.md §2](docs/19-implementation-roadmap.md) for the live checklist.

## Repository layout

See [docs/13-repository-structure.md](docs/13-repository-structure.md) for the full, explained tree. Summary:

```
frontend/     SvelteKit app
services/     One directory per microservice (Go)
shared/       Cross-cutting Go packages (logging, jwtauth, apiresponse) and gRPC/event contracts
docker/       Local development stack (docker-compose.yml)
terraform/    AWS infrastructure as code
docs/         The 20 foundational documents — read these first
```

## Local development

Prerequisites: Go 1.26+, Node 22+, Docker Desktop.

1. Create your local secrets once (random database passwords, the service-to-service key and the PAN encryption key; the file is git-ignored), then start the backend stack:
   ```
   python scripts/gen_env.py
   docker compose -f docker/docker-compose.yml up --build
   ```
   Compose refuses to start without `docker/.env`. If you have a Postgres volume from before this change, recreate it (`docker compose down -v`, which deletes local data) so the per-service database roles are created. Published ports bind to localhost only.
   Auth Service comes up on `:8081`, Customer Service on `:8082`. Both apply their own database migrations automatically on startup — no manual migration step is needed locally.

2. Run the frontend separately (a hot-reloading dev server gains nothing from containerization):
   ```
   cd frontend
   npm install
   npm run dev
   ```
   Copy `frontend/.env.example` to `frontend/.env` first if you need to change the default service URLs.

3. Run a service's tests directly without Docker (they use in-memory fakes, not a real database — see [docs/17-testing-strategy.md §2](docs/17-testing-strategy.md)):
   ```
   cd services/auth-service
   go test ./...
   ```

## Contributing to this repo (including AI assistants)

Read [docs/00-master-ai-instructions.md](docs/00-master-ai-instructions.md) and [docs/18-development-workflow.md](docs/18-development-workflow.md) first. The short version: check the relevant doc before changing a schema, endpoint, or architectural pattern; keep the docs in sync with the code in the same change; don't skip tests or lint to move faster.
