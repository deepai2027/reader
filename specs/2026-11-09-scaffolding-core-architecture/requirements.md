# Requirements — Project Scaffolding & Core Architecture

**Roadmap phase:** Phase 0 — Project Scaffolding & Core Architecture
(`specs/roadmap.md`)
**Branch:** `feature/2026-11-09-scaffolding-core-architecture`

## Context

This is the first implementation phase of Reader. Per `specs/roadmap.md`'s
core-first strategy, Phase 0 exists to create the architectural seams —
domain types and port interfaces — that let Phase 1 (the real file-
processing pipeline) be built and tested without any external
infrastructure. Per `specs/mission.md`, every seam introduced here must
keep reliability/recoverability as a first-class concern, not an
afterthought bolted on later: the interfaces defined now are what will
let Phase 2 prove fault tolerance against local stubs before Postgres,
RabbitMQ, or MinIO ever enter the picture.

Per `specs/tech-stack.md`, this phase is where the "minimal dependencies"
bias is most visible: everything in scope here is buildable with the Go
standard library alone (`log/slog`, `os.Getenv`, `testing`), with zero
third-party dependencies required.

## Scope

In scope for this phase:

- Repository structure matching the layout in `specs/tech-stack.md`
  (`cmd/reader`, `internal/...`, `migrations/` placeholder directory,
  `go.mod`).
- Core domain types: `FileState`, `RegexPattern`, `RawLine`,
  `ExtractedCandidate`, `ValidatedRecord`, `UniqueURLMessage`.
- Core port interfaces in `internal/ports`: `ProgressStore`,
  `MessagePublisher`, `RegexSource`, `FileSource`.
- Minimal, no-op/in-memory stub implementations of each port — enough to
  make the project compile and to give Phase 1 something to inject, but
  with no real persistence or delivery logic (that's Phase 1/2's job).
- `internal/config`: loads and validates only the parameters the core
  pipeline needs in Phase 1–3 (`BATCH_SIZE`, username/password length
  ranges, retry/backoff tuning). Infra connection settings (DB, MinIO,
  RabbitMQ, Agent) are **not** part of this phase's config surface.
- `log/slog`-based structured logging, wired into `cmd/reader/main.go`.
- Makefile with `build`, `vet`, `test` targets.
- CI pipeline running `go build ./...`, `go vet ./...`, `go test ./...`
  on push/PR — no services, no containers.

## Explicit Decisions

1. **Docker/Postgres: fully deferred to the Final Phase.** Phase 0
   introduces zero containers and zero `docker-compose.yml`. This
   matches the roadmap's explicit note that Docker Compose is first
   introduced in "Final Phase / Step A," not before.
2. **Task ordering: strict dependency order.** `plan.md`'s task groups
   are small and sequential — types, then interfaces, then stub
   adapters, then config, then logging, then CI — each group building on
   the last, rather than a few large bundled groups.
3. **Definition of done: automated checks + manual review.** Merge
   requires both a green CI run (`build`/`vet`/`test`) and a human
   reviewer completing the checklist in `validation.md` — automated
   passing tests alone are not sufficient to merge this phase, since the
   main risk here is architectural (interface shape, package boundaries)
   rather than purely functional.

## Out of Scope (deferred to later phases)

- Any real implementation behind the ports (local-adapter logic is
  Phase 1/2; real infra adapters are the Final Phase).
- Regex matching/selection logic itself (Phase 1) — Phase 0 only defines
  the `RegexPattern` type and the `RegexSource` interface shape.
- Batch reading, extraction, validation, normalization, hashing logic
  (Phase 1).
- Retry/backoff/checkpoint *logic* (Phase 2) — Phase 0 only shapes the
  interfaces that logic will sit behind.
- Any env vars for Agent/MinIO/PostgreSQL/RabbitMQ connectivity (Final
  Phase).
- Docker, Docker Compose, Dockerfile (Final Phase / Step A, per decision
  above).

## Open Items Carried From the Roadmap

These remain unresolved and are **not** blockers for Phase 0, but they
constrain how two of the interfaces should be shaped so Phase 1 doesn't
have to break them later:

- **Regex engine sufficiency** (stdlib `regexp` vs. `regexp2`) is still
  open. `RegexPattern` and `RegexSource` must therefore be defined
  generically enough (e.g., matching against a `string`, not against a
  concrete `*regexp.Regexp`) that the engine choice doesn't leak into the
  interface shape.
- **Regex tie-break rule** and **test infrastructure strategy** are not
  relevant to Phase 0 and are simply re-noted here for traceability.

## Acceptance Summary

Phase 0 is complete when another engineer (or Phase 1 work) can import
`internal/ports`, `internal/domain` (or equivalent), `internal/config`,
and a logger from `main.go`, build and test the whole module with zero
external services running, and start writing Phase 1 logic against the
stub adapters without needing to touch anything defined in this phase.
