---
name: ai-dlc
description: Run a FinVerse change through the AI-Driven Development Life Cycle (Inception, Construction, Operations) with human approval gates and roadmap/doc updates. Use when starting a roadmap phase or a cross-service feature.
---

# AI-DLC for FinVerse

AI proposes and builds; the human approves at each gate. Keep the loop short and the artifacts in `docs/`.

## 1. Inception (mob elaboration)
1. Restate the intent in one paragraph and map it to `FR-*`/`NFR-*` IDs in docs 03/04 and the phase in `docs/19-implementation-roadmap.md`.
2. Split into units (usually one per service or shared package) with: purpose, events consumed/published, data owned, acceptance criteria.
3. Record decisions that deviate from the docs (e.g. datastore, broker) as numbered decisions (`D-n`) with the reason. Do not silently override docs 05/07/08.
4. **Gate:** ask the user only about choices that change what gets built (scope, broker, skills). Otherwise proceed on documented defaults.

## 2. Construction (per unit)
1. Domain/service layer first, with unit tests against fakes (`internal/service/fakes_test.go` pattern).
2. Then repository (Postgres + `db/migrations`), transport (gin), wiring in `cmd/server/main.go` only.
3. Messaging rules: handlers are idempotent on `Envelope.EventID`; unrecoverable input returns `eventbus.Permanent(err)`; every consumer has a per-group DLQ.
4. Every service calls `telemetry.Init`, mounts `telemetry.GinMiddleware`, and uses the gRPC stats handlers.
5. Run `gofmt`, `go vet`, `go test ./...` for each touched module before moving on.

## 3. Operations (verify live, then document)
1. `docker compose -f docker/docker-compose.yml up -d --build`; exercise the real stack, not just tests.
2. Verify the exit criteria from the roadmap (for resilience claims: kill/disable the dependency and show the effect).
3. Measure, don't assert: load numbers from `loadtest/` and trace screenshots/IDs from Jaeger go into the docs with the date and the hardware.
4. Update `docs/19-implementation-roadmap.md` checkboxes in the same change; mark anything unverified as such.

## Done means
Tests pass, stack verified live, roadmap updated honestly, known gaps listed.
