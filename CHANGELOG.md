# Changelog

All notable changes to Reader are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning
follows [Semantic Versioning](https://semver.org/) once a 1.0.0 release
exists — until then, 0.x releases may include breaking changes between
minor versions as the architecture settles.

## [Unreleased]

Nothing yet.

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
