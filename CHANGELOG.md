# Changelog

All notable changes to Reader are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning
follows [Semantic Versioning](https://semver.org/) once a 1.0.0 release
exists — until then, 0.x releases may include breaking changes between
minor versions as the architecture settles.

## [Unreleased]

MVP: reliability + full infrastructure integration (see
`specs/2026-11-10-mvp/`). Pending a real build/test pass before this
section moves to a versioned release - see that spec's `plan.md`
status notes.

### Added

- Checkpointing after every batch and crash-recovery/resume
  (`internal/processing/pipeline`), proven by fault-injection tests.
- Reliable delivery: a retrying publisher wrapper with transient/
  permanent failure semantics (`internal/delivery/local`).
- Real PostgreSQL persistence (`internal/storage`, `migrations/`),
  replacing the Phase 0 stub `ProgressStore`/`RegexSource`.
- Real MinIO file download (`internal/objectstore`), replacing the
  stub `FileSource`.
- Agent polling client and single-file concurrency enforcement
  (`internal/agent`, `internal/orchestrate`), built from the README's
  documented contract (not yet verified against a real Agent).
- Real RabbitMQ delivery (`internal/rabbitmq`).
- `cmd/reader -mode=agent`: the real end-to-end polling loop.
- `docker-compose.yml` and `Dockerfile` (reader + postgres only, per
  the infrastructure constraint in `specs/tech-stack.md`).

### Known Gaps (intentional, see `specs/2026-11-10-mvp/validation.md`)

- Unique-URL dedup state is not persisted across a restart - a URL may
  be re-emitted to the Unique URL topic after a crash/resume.
- A batch that fails partway through is fully reprocessed on resume,
  which can re-publish already-succeeded records within that batch
  (downstream `record_hash` dedup is the intended safety net).
- Status API, Log Rotation, and Phase 3 performance hardening are not
  included.
- The Agent, RabbitMQ topic, and MinIO wiring have not been run
  against real instances in the environment that wrote them - no Go
  toolchain or network access there. `test/integration/mvp_test.go`
  and `cmd/reader`'s `-mode=agent` wiring are the highest-risk,
  least-verified code in this release.

## [0.1.0] - 2026-11-09

Phase 0 — Project Scaffolding & Core Architecture (see
`specs/roadmap.md` and
`specs/2026-11-09-scaffolding-core-architecture/`).

### Added

- Repository and Go module scaffolding (`go.mod`, directory layout,
  `Makefile`, CI workflow).
- Core domain types (`internal/domain`): `FileState`, `RegexPattern`,
  `RawLine`, `ExtractedCandidate`, `ValidatedRecord`, `UniqueURLMessage`.
- Core port interfaces (`internal/ports`): `ProgressStore`,
  `MessagePublisher`, `RegexSource`, `FileSource`.
- Minimal stub adapters (`internal/ports/stub`) satisfying each port,
  for Phase 1+ to build against before any real infrastructure exists.
- Configuration loading (`internal/config`) for core-pipeline
  parameters (batch size, validation bounds, retry/backoff tuning),
  with aggregated validation errors.
- Structured JSON logging via `log/slog`, wired into `cmd/reader/main.go`.
- GitHub Actions CI pipeline (`build`/`vet`/`test`, no service
  containers).

### Known Gaps

- No real pipeline logic yet — Reader does not process files. That's
  Phase 1.
- `.env` is not auto-loaded; environment variables must be exported
  manually (the `godotenv` dependency named in `specs/tech-stack.md`
  for this purpose hasn't been wired in yet).
- Automated checks (`go build`/`vet`/`test`) were written but not
  compiler-verified in the environment they were authored in; first
  real build confirmed working by a human on 2026-11-09.
