# Changelog

All notable changes to Reader are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning
follows [Semantic Versioning](https://semver.org/) once a 1.0.0 release
exists — until then, 0.x releases may include breaking changes between
minor versions as the architecture settles.

## [Unreleased]

## [0.2.1] - 2026-11-19

### Added

- Implemented the Reader Status API. The Agent can use authenticated
  `POST /status` requests to look up committed processing counts for
  multiple files in PostgreSQL.


## [0.2.0] - 2026-11-17

MVP reliability and infrastructure integration.

### Added

- Phase MVP / Group 9: wiring, config, CI, docker-compose, end-to-end test, status notes
- Phase MVP / Group 8: real RabbitMQ delivery adapter
- Phase MVP / Group 7: Agent integration + single-file concurrency
- Phase MVP / Group 6: MinIO file download (FileSource adapter)
- Phase MVP / Group 5: PostgreSQL persistence (storage adapters + migrations)
- Phase MVP / Group 3: reliable delivery (local retrying publisher)
- Phase MVP / Groups 1-2: checkpointing + crash recovery/resume
- Add MVP feature spec: reliability + full infra integration (requirements, plan, validation)

## [0.1.1]  - 2026-11-15
Phase 1 — Core Implementation: The File-Processing Pipeline
The core pipeline is fully functional and testable against local files
and the Phase 0 stub adapters, with zero external services.

### Added

- `internal/processing/batch` — line-based, memory-bounded streaming
  reader over a `ports.FileSource` stream, batched by `BATCH_SIZE`, with
  1-based line numbers that persist across batches (verified bounded by
  a live-heap measurement test).
- `internal/processing/regexselect` — pattern selection over the first
  5,000 lines: most-matching-lines wins, lowest `ID` breaks exact ties;
  malformed patterns are skipped with a logged warning. The regex engine
  is isolated behind `compileAndCount` so a `regexp2` swap touches only
  this package.
- `internal/processing/extract` — per-line extraction via the selected
  pattern's named capture groups (`url`/`username`/`password`), read by
  group name (order-independent); non-matching and group-less lines are
  dropped, not errors.
- `internal/processing/validate` — username/password length bounds from
  `internal/config` (boundaries inclusive) plus required-field checks;
  returns a valid/invalid result with a machine-readable reason.
- `internal/processing/normalize` — canonical URL form (scheme assumed
  http when absent, host lowercased, userinfo stripped, default port
  removed, query kept, fragment dropped) and domain/subdomain/suffix/
  port split via `golang.org/x/net/publicsuffix`.
- `internal/processing/record` — deterministic `record_hash =
  sha256(url|user|password)` over the *normalized* URL, and final
  `ValidatedRecord` assembly.
- `internal/processing/uniqueurl` — per-file-run dedup of normalized
  URLs, attaching the file's priority; credentials are structurally and
  test-enforced excluded from `UniqueURLMessage`.
- `internal/processing/pipeline` — `ProcessFile`, the single
  end-to-end entry point wiring every stage together against the ports
  (`ProgressStore`, `MessagePublisher`, `RegexSource`, `FileSource`,
  config) as explicit `Deps` (no globals), ready for Phase 2 to extend
  with checkpointing/resume.
- `internal/processing/testdata` — fixture library (well-formed,
  mixed valid/invalid, no-match, empty, blank-only, duplicate-URL,
  normalization edge cases) exercised by the pipeline's end-to-end test.
- `cmd/reader/main.go` — a **temporary, manual** pipeline trigger
  (`-file`/`-priority`) so the pipeline can be run against a local file
  by hand; the Agent-driven flow is Final Phase.
- Dependency: `golang.org/x/net` (`publicsuffix`), already named in
  `specs/tech-stack.md`.

## [0.1.0] - 2026-11-09

Phase 0 — Project Scaffolding & Core Architecture

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
