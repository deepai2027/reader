# Requirements — MVP: Reliability + Full Infrastructure Integration

**Roadmap coverage:** `specs/roadmap.md` Phase 2 (Processing Reliability
& Fault Tolerance) + Final Phase Steps A–D (PostgreSQL, MinIO, Agent,
RabbitMQ). Explicitly **excludes** Final Phase Step E (Status API), Step
F (Log Rotation), and Phase 3 (Large-Scale & Performance Hardening).
**Branch:** `mvp`
**Depends on:** Phase 0 (`v0.1.0`) and Phase 1 (merged into `main`) —
`internal/domain`, `internal/ports`, `internal/ports/stub`,
`internal/config`, and the full `internal/processing/*` pipeline
(`batch`, `regexengine`, `regexselect`, `extract`, `validate`,
`normalize`, `record`, `uniqueurl`, `pipeline`).

## Context

Per `specs/mission.md`, Reader's defining promise is durability of
progress — "no file is ever lost, silently skipped, or double-processed
— regardless of when or how the process stops." Phase 1 proved the
pipeline produces *correct output*; it said nothing about what happens
if the process is killed mid-file. This spec closes that gap and, in
the same pass, connects the now-correct-and-resumable pipeline to real
infrastructure for the first time — because an MVP that only runs
against local stub adapters isn't something anyone outside this repo
can actually use.

Per `specs/tech-stack.md`, this is where the dependency list grows for
real: `jackc/pgx`, `golang-migrate/migrate`, `rabbitmq/amqp091-go`,
`minio/minio-go` all enter the build for the first time, alongside
Docker Compose (`reader` + `postgres` only, per the explicit
infrastructure constraint carried since Phase 0).

This spec follows the same core-first discipline the roadmap already
established: reliability is built and proven against the Phase 0/1
local adapters *first*, exactly as originally planned for Phase 2, and
only then do real adapters replace those stubs — one at a time, each
swap immediately followed by re-running the reliability test suite
against the real thing. Folding the Final Phase into the same spec as
Phase 2 doesn't change that order; it just means both now live in one
feature spec instead of two, so the "why infra moves last" reasoning in
`specs/roadmap.md` still applies group-by-group within this plan.

## Scope

**In scope** (per the chosen MVP boundary):

- **Checkpointing** — `ProgressStore.Save` after every batch (not just
  at the end, as Phase 1 does today), so progress survives a crash, not
  just a clean shutdown.
- **Recovery/resume** — on startup, check `ProgressStore.FindReading`;
  if a file is mid-processing, reopen it (never re-download once Step B
  lands), seek to `CurrentLine + 1`, reuse `SelectedRegexID`, and
  resume batching — never restart from the beginning.
- **Reliable message delivery semantics** — retry-with-backoff,
  delivery confirmation, and a bounded-retry failure path, built against
  a local adapter first (using `internal/config`'s already-defined
  `RetryMaxAttempts`/`RetryBaseDelayMS` and `ports.ErrTransient`/
  `ErrPermanent` from Phase 0), then proven again against real RabbitMQ.
- **Fault-injection test harness** proving the invariants from
  `specs/mission.md`: no line reprocessed after a crash+restart, no
  line skipped, the selected regex never changes mid-file, a file's
  priority survives a restart.
- **PostgreSQL persistence** (Final Phase Step A) — `regex` and
  `file_management` tables, `golang-migrate` migrations including
  seeding predefined patterns, real `ProgressStore` and `RegexSource`
  adapters.
- **MinIO file download** (Final Phase Step B) — real `FileSource`
  adapter, streaming download, local-file retention rules per
  `specs/mission.md` (never delete from MinIO during normal
  processing).
- **Agent integration & single-file concurrency** (Final Phase Step C)
  — polling client, `file_id`/`priority` retrieval, `409
  file_in_reading` handled as an expected wait condition, single-file
  enforcement made explicit against the real flow.
- **Real RabbitMQ delivery** (Final Phase Step D) — `amqp091-go`, both
  topics, `x-max-priority` queues, confirm-mode publishing.
- **A trimmed end-to-end integration test** proving the full included
  scope works together against real infrastructure — a narrower version
  of the roadmap's Step G, scoped to exclude anything involving the
  Status API or Log Rotation (since those aren't built yet).

**Out of scope** (deferred post-MVP, per the chosen boundary):

- Reader Status API (Final Phase Step E) — the Agent cannot yet query
  processing status via Reader; this is a real MVP limitation, not an
  oversight, and should be communicated as such to anyone depending on
  this MVP.
- Log Rotation (Final Phase Step F) — expired files/records are not
  automatically cleaned up yet. Manual/ad-hoc cleanup is the stopgap
  until this lands.
- Phase 3 (Large-Scale & Performance Hardening) — multi-GB stress
  testing and `BATCH_SIZE` tuning under load have not been done. The
  MVP is *correct and resumable*, not *proven at scale*.
- The full README §17 test checklist and complete documentation pass
  that the roadmap's Step G originally called for — only the slice of
  it relevant to what's actually built here is in scope (see
  `validation.md`).

## Explicit Decisions

1. **MVP boundary:** Phase 2 (reliability) + Final Phase Steps A–D
   (Postgres, MinIO, Agent, RabbitMQ). Status API, Log Rotation, and
   Phase 3 performance hardening are explicitly deferred — see Scope
   above.
2. **Task ordering: reliability first, then infra in the existing A–D
   order.** `plan.md` builds and proves checkpointing/recovery/retry
   against the Phase 0/1 local adapters before any real adapter is
   introduced, then swaps in Postgres, MinIO, Agent, and RabbitMQ one
   at a time in that order — each swap's group ends with re-running the
   reliability suite against the thing that was just made real.
3. **Definition of done: automated tests only.** Unlike Phase 0/1, this
   spec does not require a separate manual review checklist as a merge
   gate — the bar is a green CI run that includes integration tests
   against real service instances. This is a deliberate change from
   Phase 0/1's "automated + manual" bar, justified by the scope and
   nature of the risk: this spec changes behavior in ways unit tests
   and real-infra integration tests can directly observe (does it
   resume correctly? does it actually publish to a real queue?), rather
   than the mostly-architectural risk Phase 0's manual checklist existed
   to catch.
4. **Test infrastructure strategy, resolved:** `testcontainers-go`
   spins up ephemeral PostgreSQL, RabbitMQ, and MinIO for integration
   tests, both locally and in CI. This resolves the roadmap's
   previously-open "test infrastructure strategy" item in favor of
   self-contained CI (no dependency on externally provisioned shared
   test infrastructure, which this project doesn't have access to
   provision anyway).
5. **Agent integration test strategy:** there is no real Agent instance
   available to integrate against. The Agent client is built strictly
   against the contract documented in the README (endpoints, request/
   response shapes, the `409 file_in_reading` behavior), and tested
   against an `httptest.Server`-based fake standing in for the Agent —
   not `testcontainers-go`, since there's no Agent container to spin up.
   This is a real limitation worth flagging: the first integration
   against an *actual* Agent deployment may surface contract mismatches
   this spec cannot catch.
6. **CI gets real service containers for the first time.** Phase 0/1's
   CI explicitly ran with zero service containers; this is a deliberate
   , documented break from that pattern, justified by the fact that real
   infrastructure is finally in scope and "automated tests only" (per
   Decision 3) means CI itself has to carry the verification weight
   manual review previously carried.

## Open Items Carried From the Roadmap — Status Update

- **Test infrastructure strategy:** resolved by Decision 4
  (`testcontainers-go`) — no longer open.
- **Regex engine sufficiency:** still open, unaffected by this spec
  (regex selection logic itself doesn't change here; only its storage
  backend does, via Final Phase Step A's `RegexSource` adapter).
- **Regex tie-break rule:** already resolved in Phase 1; unaffected.

## New Open Item

- **Agent contract assumptions** (Decision 5): the Agent client is
  built from the README's documented contract with no real Agent to
  validate against. Flag any place the implementation had to guess at
  an undocumented detail (exact error response shape on `409`, retry-
  after headers, etc.) so these can be confirmed against the real Agent
  team before this MVP is actually deployed against it.

## Acceptance Summary

The MVP is complete when: a file can be killed mid-processing at any
point and resume with zero reprocessed and zero lost lines (proven
first against local adapters, then again against real Postgres); a real
file can be polled from a real Agent-shaped endpoint, downloaded from a
real MinIO bucket, processed through the full Phase 1 pipeline, and its
records observed landing in real RabbitMQ queues, with correct
`file_management` state in a real PostgreSQL database throughout — all
without a human reading status from an API (there isn't one yet) and
without automated retention cleanup (there isn't one of those yet,
either).
