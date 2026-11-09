# Plan — Project Scaffolding & Core Architecture

Task groups are ordered strictly by dependency: each group only depends
on groups above it, never below. Keep groups small — if a group starts
accumulating unrelated changes, split it.

## Implementation Status (2026-11-09)

Groups 1–7 have been implemented, each as its own commit on
`feature/2026-11-09-scaffolding-core-architecture`. Group 8 (exit
verification) is **partially blocked**: this sandbox has no Go
toolchain and no network access to install one, so `go build`,
`go vet`, and `go test` could not actually be run here. All code was
written and manually reviewed (import usage, brace/paren balance,
interface-signature consistency across packages) but has **not been
compiler-verified**. Run `make build vet test` locally (or point me at
an environment with Go, e.g. Claude Code) before treating Group 8 as
complete — see `validation.md` for the exact commands and what to
check.

| Group | Status |
|---|---|
| 1. Repository & Module Scaffolding | Implemented |
| 2. Core Domain Types | Implemented |
| 3. Core Port Interfaces | Implemented |
| 4. Minimal Stub Adapters | Implemented |
| 5. Configuration Loading | Implemented |
| 6. Structured Logging Setup | Implemented |
| 7. Continuous Integration Pipeline | Implemented (workflow file only — not yet run on a real CI provider) |
| 8. Phase Exit Verification | **Blocked on local `make build vet test` — not run** |

---

## 1. Repository & Module Scaffolding ✅ implemented

**Depends on:** nothing.

- [x] `git init` already done on `feature/2026-11-09-scaffolding-core-architecture`.
- [x] `go mod init` with the module's real import path (confirm the path
      with the team before committing — placeholder until confirmed).
- [x] Create directory layout per `specs/tech-stack.md`:
      `cmd/reader/`, `internal/agent/`, `internal/config/`,
      `internal/logrotation/`, `internal/minio/`, `internal/processing/`,
      `internal/rabbitmq/`, `internal/regexselect/`, `internal/statusapi/`,
      `internal/storage/`, `migrations/` (empty, placeholder `.gitkeep`).
      Only create the directories this phase actually populates
      (`cmd/reader`, `internal/config`, plus the new `internal/ports` and
      a domain-types package below); leave the rest as empty
      placeholders with a `.gitkeep` so the layout is visible without
      pretending those packages have content yet.
- [ ] `.gitignore` (binaries, `.env`, IDE files).
- [ ] `Makefile` with stub targets: `build`, `vet`, `test` (filled in as
      later groups land, not left broken).

**Done when:** `go build ./...` succeeds on an empty `main.go` that just
logs "reader starting" and exits.

---

## 2. Core Domain Types ✅ implemented

**Depends on:** 1.

- [ ] New package (e.g. `internal/domain`) with the core types:
      - `FileState` (processing status + progress fields: `FileID`,
        `Status`, `CurrentLine`, `MatchedCount`, `SelectedRegexID`,
        `Priority` — mirrors the eventual `file_management` row, but as
        a plain struct with no persistence awareness).
      - `RegexPattern` (`ID`, `Name`, `Pattern string`, `IsActive bool`)
        — `Pattern` stays a plain string, not a compiled
        `*regexp.Regexp`, to stay engine-agnostic (see
        `requirements.md`, open items).
      - `RawLine` (line number + raw text, as read from a batch).
      - `ExtractedCandidate` (unvalidated URL/username/password pulled
        from a `RawLine`).
      - `ValidatedRecord` (the full structured record shape: hash,
        username, password, url, domain, subdomain, suffix, port,
        modified_date, created_at).
      - `UniqueURLMessage` (url, priority, domain, subdomain, suffix —
        no username/password, per the mission's data-safety principle).
- [ ] Doc comments on every exported type explaining its role in the
      pipeline (these are read by whoever implements Phase 1/2, and by
      the Phase 0 manual reviewer).
- [ ] Table-driven unit test per type's zero-value/construction
      behavior, if there's any validation worth testing at this stage
      (most of these are plain data carriers — don't invent tests that
      don't test anything real).

**Done when:** the domain package compiles standalone with no
dependency on `internal/ports` or anything infra-related.

---

## 3. Core Port Interfaces ✅ implemented

**Depends on:** 2.

- [ ] New package `internal/ports` defining:
      - `ProgressStore` — methods to save/load a `domain.FileState` by
        file ID, and to query for an in-progress (`reading`) file on
        startup.
      - `MessagePublisher` — methods to publish a `ValidatedRecord` and
        a `UniqueURLMessage` independently, with a return/error shape
        that can represent transient vs. permanent failure (Phase 2
        needs this distinction for retry logic).
      - `RegexSource` — a method to list active `domain.RegexPattern`s.
      - `FileSource` — a method to open a readable stream for a given
        file reference (local path for now; MinIO object later — the
        interface shouldn't care).
- [ ] Doc comments stating explicitly, for each interface, which phase
      implements the real adapter (Phase 1/2 for local stubs, Final
      Phase for real infra) — this traceability matters for reviewers
      later.
- [ ] No implementations yet in this group — interfaces only.

**Done when:** `internal/ports` compiles, imports only `internal/domain`
and the standard library, and every method signature is justified by a
concrete need already described in `specs/roadmap.md` (no speculative
methods "just in case").

---

## 4. Minimal Stub Adapters ✅ implemented

**Depends on:** 3.

- [ ] For each port, one minimal stub implementation sufficient to
      satisfy the interface and let the project build/test — explicitly
      **not** the real Phase 1/2 logic:
      - `ProgressStore`: in-memory map, no file persistence yet (Phase 2
        decides whether the local adapter is JSON-file-backed or purely
        in-memory for tests).
      - `MessagePublisher`: logs what would have been published and
        always returns success.
      - `RegexSource`: returns an empty slice or a single hardcoded
        test pattern.
      - `FileSource`: opens a local `*os.File` by path.
- [ ] Each stub lives near its interface or in a clearly-named
      `.../stub` subpackage/file — never mixed into what will become the
      Phase 1/2 real local adapters, so it's obvious what's temporary.
- [ ] A short package-level comment on every stub: "Placeholder for
      Phase 0 compilation only; replaced in Phase 1/2."

**Done when:** `cmd/reader/main.go` can construct one of each stub, wire
them together, and run without error against zero external services.

---

## 5. Configuration Loading (Core-Only Parameters) ✅ implemented

**Depends on:** 1 (independent of 2–4, but ordered after repo setup).

- [ ] `internal/config` package: a typed `Config` struct covering only
      what Phase 1–3 need — `BatchSize`, username/password length
      bounds, retry/backoff parameters (max attempts, base delay).
- [ ] `Load()` function: reads from `os.Getenv`, validates required
      fields and numeric ranges, fails fast with a clear, aggregated
      error (not just the first failure) if anything is missing or
      invalid.
- [ ] `.env.example` with just these Phase 0–relevant variables —
      explicitly **not** yet including DB/MinIO/RabbitMQ/Agent variables
      (those are added file-by-file as each Final Phase step lands).
- [ ] Unit tests: valid config loads correctly; missing required var
      fails with a named error; out-of-range numeric value fails with a
      named error.

**Done when:** `Load()` is fully unit-tested without touching any real
environment beyond what the test sets up itself (no reliance on
developer-machine env state).

---

## 6. Structured Logging Setup ✅ implemented

**Depends on:** 5 (log level comes from config).

- [ ] `log/slog` logger constructed in `cmd/reader/main.go` from the
      Phase 0 config's log-level setting (add a `LogLevel` field to
      `Config` in group 5 if not already present).
- [ ] Confirm JSON output handler for anything that isn't an interactive
      local run (text handler acceptable for local dev, but don't make
      this a hardcoded choice — drive it from config too).
- [ ] One smoke log line on startup ("reader starting", with version/
      build info if easily available) — this becomes the heartbeat
      Phase 1+ builds on.

**Done when:** running the binary locally produces one structured log
line and exits cleanly.

---

## 7. Continuous Integration Pipeline ✅ implemented (unverified on real CI)

**Depends on:** 1–6 (needs something real to build/vet/test).

- [ ] CI workflow (e.g. GitHub Actions) triggered on push and PR,
      running, in order: `go build ./...`, `go vet ./...`,
      `go test ./...`.
- [ ] CI runs with zero service containers — this phase has nothing
      that needs one, and that's the point.
- [ ] Badge or status check wired so a PR can't merge with a red run.

**Done when:** a PR opened from this branch shows a green CI run with no
infrastructure dependencies in the workflow file.

---

## 8. Phase Exit Verification ⚠️ blocked — see status note above

**Depends on:** 1–7.

- [ ] Run through `validation.md` in full: automated checks + manual
      review checklist.
- [ ] Confirm `specs/mission.md`'s reliability framing and
      `specs/tech-stack.md`'s minimal-dependency bias are both visibly
      reflected in what was built (no unexplained new dependencies in
      `go.mod`, no infra leaking into this phase's config or packages).
- [ ] Open the PR into `main` only after this checklist is complete.
