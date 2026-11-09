# Requirements — Core Implementation: The File-Processing Pipeline

**Roadmap phase:** Phase 1 — Core Implementation: The File-Processing
Pipeline (`specs/roadmap.md`)
**Branch:** `feature/2026-11-09-core-file-processing-pipeline`
**Depends on:** Phase 0 (merged into `main` as `v0.1.0`) — `internal/domain`,
`internal/ports`, `internal/ports/stub`, `internal/config`.

## Context

This is Reader's single most important phase (per `specs/roadmap.md`):
given a local file, produce correct, structured, validated output —
entirely independent of PostgreSQL, RabbitMQ, MinIO, and the Agent API.
Per `specs/mission.md`, "reliability" starts here, not in Phase 2: a
pipeline that silently mis-extracts, mis-validates, or mis-normalizes
data is just as much a mission failure as one that loses a line on
crash. Per `specs/tech-stack.md`, extraction/normalization/hashing use
only the standard library (`regexp`, `net/url`,
`golang.org/x/net/publicsuffix`, `crypto/sha256`) — no new dependency is
expected in this phase.

Phase 1 is built entirely against the Phase 0 ports and stub adapters.
It does not call real infrastructure and does not need to — that's the
point of the core-first strategy.

## Scope

In scope for this phase, per `specs/roadmap.md`'s Phase 1 description:

- **Large-file, memory-bounded batch reading** — a line-based streaming
  reader over a `ports.FileSource` stream, batched by the configured
  `BATCH_SIZE`, verified to run in bounded memory regardless of file
  size.
- **Regex identification & selection** — scan the first 5,000 lines
  against every pattern from `ports.RegexSource.ActivePatterns`, apply
  the tie-break rule (see Decisions below), and keep the selected
  pattern fixed for the rest of the file.
- **No-match handling** — if nothing matches the first 5,000 lines, the
  file is done with zero matches; no error, no partial processing.
- **Data extraction** — URL/username/password per line, via the
  selected pattern's named capture groups.
- **Validation** — username/password structure and the configured
  length bounds from `internal/config`.
- **URL normalization** — consistent scheme/host/casing/trailing-slash
  handling.
- **Domain/subdomain/suffix/port extraction** from the normalized URL
  (via `golang.org/x/net/publicsuffix`).
- **Structured record transformation** — assemble `domain.ValidatedRecord`
  in its final shape.
- **Record hashing** — deterministic `sha256(url|user|password)` using
  the *normalized* URL.
- **Unique-URL derivation** — dedupe normalized URLs within a single
  file's processing run, attach the file's priority, assemble
  `domain.UniqueURLMessage`.
- **End-to-end orchestration** — a single entry point that wires the
  above into one pipeline run against a local file, using the Phase 0
  stub adapters for progress/publishing, so the whole thing is
  demonstrable, not just unit-tested in isolation.
- Invalid lines are dropped and counted, never forwarded — this applies
  per-line, not per-file (one bad line does not fail the whole file).

## Explicit Decisions

1. **Regex tie-break rule: most-matching-lines wins.** When more than
   one active pattern matches within the first 5,000 lines, the pattern
   that matched the **greatest number of those lines** is selected —
   this is the "best empirical fit" against the file's actual content,
   not an arbitrary priority order. **Secondary tie-break:** if two or
   more patterns match the exact same (non-zero) number of lines, the
   pattern with the **lowest `ID`** wins, to keep selection fully
   deterministic. This rule is provisional until confirmed against the
   real predefined patterns (carried over from `specs/roadmap.md`'s open
   items) but is no longer a placeholder — it is the rule Phase 1
   implements and tests against.
2. **Task ordering: strict pipeline order.** `plan.md`'s groups follow
   the data's own path through the system — batch reading → regex
   selection → extraction → validation → normalization →
   hashing/unique-URL — each group building directly on the last, ending
   with an orchestration group that wires them together and a
   verification group.
3. **Definition of done: automated tests + manual review.** As in
   Phase 0, a green automated run alone is not sufficient; a human
   reviewer completes the checklist in `validation.md` before this
   phase merges, because correctness here (tie-break behavior, hash
   determinism, no credential leakage into unique-URL messages) is
   exactly the kind of thing a reviewer catches that a passing test
   suite can mask if the tests themselves are subtly wrong.

## Open Items Carried From the Roadmap — Status Update

- **Regex tie-break rule:** resolved by Decision 1 above. No longer
  open for Phase 1's purposes, though it remains provisional pending
  real pattern data.
- **Regex engine sufficiency** (stdlib `regexp` vs. `regexp2`): still
  open. Phase 1 proceeds with stdlib `regexp` (RE2), per
  `specs/tech-stack.md`'s default. The `RegexPattern`/`RegexSource`
  interfaces from Phase 0 were deliberately kept engine-agnostic for
  this reason — if the real patterns turn out to need backreferences or
  lookahead, only the matching implementation inside this phase's
  regex-selection group needs to change, not the interfaces or anything
  upstream/downstream of them.
- **Test infrastructure strategy:** not relevant to Phase 1 — this
  phase needs no infrastructure at all, real or test-double, beyond the
  Phase 0 stubs it already has.

## Out of Scope (deferred to later phases)

- Checkpointing, crash recovery, retry/backoff logic (Phase 2) — Phase 1
  processes a file start-to-finish in one run; what happens if it's
  interrupted mid-run is explicitly Phase 2's problem.
- Performance benchmarking against multi-GB files and adversarial regex
  input (Phase 3) — Phase 1 proves *correctness* and *bounded memory in
  principle*; Phase 3 proves it holds under real stress.
- Any real adapter behind `ProgressStore`, `MessagePublisher`,
  `RegexSource`, or `FileSource` (Final Phase) — Phase 1 uses the Phase 0
  stubs as-is and does not modify them.
- Agent polling, MinIO downloads, PostgreSQL persistence, RabbitMQ
  publishing, the Status API (Final Phase, per the roadmap's explicit
  deferral).

## Acceptance Summary

Phase 1 is complete when, given any local text file (well-formed,
malformed, or empty) and the hardcoded stub Regex pattern from Phase 0
(or a test-only pattern set swapped in via `RegexSource`), Reader
selects a pattern (or correctly finds no match), streams the file in
bounded memory, extracts and validates each line independently, produces
correctly normalized and hashed `ValidatedRecord`s and deduplicated
`UniqueURLMessage`s, and hands them to the stub `MessagePublisher` — all
verified by fixture-driven tests, with zero external services running.
