# Reader — Roadmap

## Strategy: Core-First

Implementation proceeds **core logic first, infrastructure last**. Reader's
value is the processing pipeline — regex selection, large-file batch
reading, extraction, validation, normalization, hashing, fault tolerance,
and reliable delivery semantics. That pipeline must be built, proven, and
stress-tested as an **independent, infrastructure-free unit** before any
database, message broker, object storage, or external API enters the
picture.

To make this possible without the core logic depending on infrastructure
that doesn't exist yet, Reader is built **port-first (hexagonal style)**:

- The core pipeline depends only on small internal **interfaces** —
  `ProgressStore` (persist/resume processing state), `MessagePublisher`
  (deliver records), `RegexSource` (supply active patterns), `FileSource`
  (supply the bytes to process).
- Early phases implement these interfaces with **local, dependency-free
  adapters**: an in-memory/JSON-file `ProgressStore`, an in-memory
  `MessagePublisher` with real retry/backoff/ack semantics, a static
  in-process `RegexSource`, and plain local filesystem `FileSource`.
- The **final phase** swaps these local adapters for real ones —
  PostgreSQL, RabbitMQ, MinIO, the Agent API — without touching core
  pipeline logic, because the core only ever talked to interfaces.

This guarantees the hardest, highest-value logic (the part that makes
Reader *reliable*, not just functional) is fully built and tested before
a single network call to external infrastructure is required to run it.

---

### Phase 0 — Project Scaffolding & Core Architecture ✅ Complete (v0.1.0, merged 2026-11-09)

**Status:** merged into `main`. See
`specs/2026-11-09-scaffolding-core-architecture/` for the full spec and
implementation status, including the caveat that automated checks were
never run in the environment that authored the code (no Go toolchain
available there) — confirmed working on a real machine after merge.

**Goal:** A buildable, testable skeleton with the architectural seams in
place — no infrastructure yet.

- Repo structure (per `tech-stack.md`), `go.mod`, Makefile, CI
  (`go build`, `go vet`, `go test`) — all runnable with zero external
  services.
- Define the core ports as Go interfaces in `internal/ports`:
  `ProgressStore`, `MessagePublisher`, `RegexSource`, `FileSource`.
- Define core domain types: `FileState`, `RegexPattern`, `RawLine`,
  `ExtractedCandidate`, `ValidatedRecord`, `UniqueURLMessage`.
- `internal/config`: env loading/validation for the parameters the core
  pipeline actually needs right now (`BATCH_SIZE`, validation length
  ranges, retry/backoff settings) — infra config (DB/MinIO/RabbitMQ/Agent
  connection details) is deferred to the Final Phase.
- `log/slog` wired for structured logging from day one.

**Exit criteria:** `go build ./... && go test ./...` passes with no
external services running. The interfaces that will isolate core logic
from infrastructure are defined and compiling against stub
implementations.

---

### Phase 1 — Core Implementation: The File-Processing Pipeline ✅ Complete (merged 2026-11-10)

**Status:** merged into `main`. See
`specs/2026-11-09-core-file-processing-pipeline/` for the full spec and
implementation status. Two follow-ups from that merge are worth
tracking: automated checks (`go build`/`vet`/`test`) still need to be
confirmed on a real machine, and `go mod tidy` needs to run once (with
network access) to properly pin `golang.org/x/net/publicsuffix` in
`go.sum`, since it was hand-added to `go.mod` without that.

**Goal:** Reader's essential functionality, working completely
independently — given a local file, it produces correct, structured,
validated output. This is the single most important phase in the
roadmap.

**In scope:**

- **Large-file, memory-bounded batch reading** — line-based streaming
  reader over a local `*os.File` (via `FileSource`), configurable
  `BATCH_SIZE`, verified to run in bounded memory regardless of file
  size (no "load whole file" path, ever — not even temporarily).
- **Regex identification & selection** — inspect the first 5,000 lines,
  test against all active patterns from `RegexSource` (backed in this
  phase by a static in-process list standing in for the future DB
  table), apply the selection/tie-break rule when multiple patterns
  match, and keep the selected pattern fixed for the rest of the file.
- **No-match handling** — if nothing matches the first 5,000 lines, mark
  the file done with `matched_count = 0`, `current_line = 0` (against
  the local `ProgressStore` stub for now), and stop — no error, no
  partial processing.
- **Data extraction** — pull URL/username/password per line using the
  selected pattern.
- **Validation** — username/password structure and configured length
  ranges; malformed required fields; reject anything not matching the
  pattern's expected structure.
- **URL normalization** — consistent scheme/host/casing/trailing-slash
  handling.
- **Domain/subdomain/suffix/port extraction** from the normalized URL.
- **Structured record transformation** — assemble the final record shape
  (`record_hash`, `username`, `password`, `url`, `domain`, `subdomain`,
  `suffix`, `port`, `modified_date`, `created_at`) exactly as it will be
  published later, even though nothing is published yet.
- **Record hashing** — deterministic `sha256("url|user|password")` using
  the *normalized* URL.
- **Unique-URL derivation** — dedupe normalized URLs within a file's
  processing and attach the file's priority, producing the second
  message shape, ready for (but not yet sent to) the Unique URL topic.
- Invalid lines are dropped and counted, never forwarded.

**Explicitly out of scope for this phase:** Agent polling, MinIO
downloads, PostgreSQL persistence, RabbitMQ publishing, the Status API.
The pipeline runs against local fixture files and writes its "progress"
and "output" to the Phase 0 local stub adapters purely so it has
something to call — none of that is real infrastructure yet.

**Exit criteria:** Given any local text file (small or huge, well-formed
or garbage), Reader can be run end-to-end — select regex, batch through
the whole file in bounded memory, extract, validate, normalize, hash, and
produce the two final record shapes — fully verified by unit and
fixture-driven tests, with zero external services running. This is the
proof that Reader's core logic works *on its own merits*, decoupled from
everything around it.

---

### Phase 2 — Processing Reliability & Fault Tolerance ✅ Complete (merged 2026-11-17)

**Status:** merged into `main` as part of the `mvp` branch. Checkpointing,
resume, and local delivery retry/failure semantics are covered by
`internal/processing/pipeline/recovery_test.go`; `go build`, `go vet`, and
`go test ./...` pass on Go 1.26.

**Goal:** Make the Phase 1 pipeline provably fault-tolerant — the
reliability guarantees from `mission.md` ("no line is ever lost, no line
is ever reprocessed") proven against the local adapters, before any real
infrastructure can obscure whether a failure is Reader's fault or the
network's.

**In scope:**

- **Checkpointing** — after every batch, persist `current_line`,
  `matched_count`, and `status` through the `ProgressStore` interface
  (still backed by the local/JSON adapter from Phase 0) so progress
  survives a process crash, not just a clean shutdown.
- **Recovery/resume workflow** — on startup, check `ProgressStore` for an
  in-progress file; if found, resume from `current_line + 1`, reuse the
  previously selected regex, and never restart from the beginning.
- **Batch-level atomicity** — a batch's progress is only committed after
  that batch's records have been handed to `MessagePublisher`
  successfully; a crash mid-batch must re-run that batch, not skip or
  duplicate it beyond what downstream dedup (`record_hash`) already
  tolerates.
- **Reliable message delivery semantics** — `MessagePublisher`'s local
  adapter implements real retry-with-backoff, delivery confirmation
  (ack/nack), and a bounded-retry failure path (what happens when
  delivery keeps failing — surfaced as an explicit error, not silently
  dropped), so this behavior is designed and tested now, independent of
  whatever broker eventually sits behind the interface.
- **Fault-injection test harness** — deliberately inject failures at
  each seam (crash mid-batch, publisher returns transient errors,
  publisher returns permanent errors, corrupt/truncated local progress
  file) and assert the pipeline's response in each case.
- **Processing continuity invariants**, proven by test:
  - No line is processed twice after a simulated crash+restart.
  - No line is skipped after a simulated crash+restart.
  - The selected regex is never re-selected or changed mid-file.
  - A file's original priority survives a restart.

**Exit criteria:** The full README §17.3 "workflow and recovery"
scenario list passes using the local adapters: resume from last line,
reuse selected regex, no reprocessing, correct status/counters after
completion, retry behavior on simulated delivery failure. Reliability is
proven before infrastructure is in the picture, not after.

---

### Phase 3 — Large-Scale & Performance Hardening ✅ Complete (merged 2026-11-19)

**Status:** merged into `main`. All 25 benchmark checks passed, along
with build, vet, unit tests, and race tests. At batch size 500, the
3 GiB distinct-URL fixture peaked at 38.71 MiB of process memory, down
from 512.04 MiB, with median throughput of 220.66 MiB/s. The example
batch size remains 500, with a supported range of 1–1,000. See
[Phase 3 validation](2026-11-19-large-scale-performance-hardening/validation.md)
for the full results and reproduction commands.

**Goal:** Stress the core pipeline the way it will actually be used —
against very large files and adversarial input — before infrastructure
adds its own latency and failure modes into the mix.

- Benchmark batch processing against large synthetic fixture files
  (multi-GB), confirming memory stays bounded and roughly flat regardless
  of file size.
- Regex-matching performance on pathological/adversarial lines (relevant
  given the RE2 engine choice in `tech-stack.md` — confirm no
  pathological slowdowns, and that the "no catastrophic backtracking"
  property actually holds for the real patterns once available).
- Tune and document safe defaults and bounds for `BATCH_SIZE` based on
  measured memory/throughput trade-offs.
- Concurrency-safety review of the batch loop and checkpoint writes (even
  though Reader processes one file at a time, confirm no shared mutable
  state issues within a single file's batch pipeline, e.g. around the
  dedup set for unique URLs).

**Exit criteria:** Documented benchmark results; `BATCH_SIZE` default and
bounds recorded in `.env.example` with rationale; no pathological
slowdowns found against representative adversarial input.

---

## Final Phase — Infrastructure & Integration

**Goal:** Wire the now-fully-proven core pipeline to real infrastructure,
one adapter at a time, by replacing each Phase 0 local stub with a real
implementation behind the same interface. Core pipeline code does not
change during this phase — only the adapters behind `ProgressStore`,
`MessagePublisher`, `RegexSource`, and `FileSource` do, plus the new
code needed to drive them (Agent polling, Status API).

Per the key requirement, **this is where Agent integration, MinIO
downloading, database implementation, and API development happen — not
before.**

**Status:** Steps A–G are complete. Step G verified the workflow with an
HTTP Agent stub and disposable PostgreSQL, MinIO, and RabbitMQ instances.
Docker-backed integration, the Docker Compose smoke check, and live Agent
interoperability remain open as validation follow-ups.

### Final Phase / Step A — PostgreSQL Persistence ✅ Complete (merged 2026-11-17)

- `docker-compose.yml` introduced for the first time: **only** `reader` +
  `postgres`, per the explicit infrastructure constraint.
- `regex` and `file_management` tables + `golang-migrate` migrations,
  including seeding the real predefined Regex patterns.
- Real `ProgressStore` adapter backed by PostgreSQL, replacing the local
  JSON-file stub — same interface, so Phase 1–3 logic and tests are
  unaffected.
- Real `RegexSource` adapter reading active patterns from the `regex`
  table, replacing the static in-process list.
- **Regression check:** re-run the Phase 2 fault-injection and recovery
  test suite, now against the real PostgreSQL-backed `ProgressStore`, to
  confirm the reliability guarantees still hold with a real database in
  the loop (transaction boundaries, connection drops, etc.).

### Final Phase / Step B — MinIO File Download ✅ Complete (merged 2026-11-17)

- Real `FileSource` adapter: streaming download from MinIO into the
  local working directory, replacing the "point at a local fixture file"
  stub.
- Local file retained until processing completes successfully (per
  `mission.md`); deleted locally on completion or on confirmed no-match,
  **never** deleted from MinIO by normal processing.

### Final Phase / Step C — Agent Integration & Single-File Concurrency ✅ Complete (merged 2026-11-17)

- Agent API client: polling loop, configurable interval, `file_id` +
  `priority` retrieval.
- Single-file-processing enforcement against the real flow: refuse/queue
  a new poll while a file is `reading`; handle the Agent's `409
  file_in_reading` response as an expected wait condition, not an error.
- File receipt workflow: persist file info, trigger Step B download,
  then hand off into the Phase 1–3 pipeline.

### Final Phase / Step D — Real RabbitMQ Delivery ✅ Complete (merged 2026-11-17)

- Real `MessagePublisher` adapter: `amqp091-go`, two topics (Full Record,
  Unique URL), `x-max-priority` queue configuration matching the Agent's
  priority range, confirm-mode publishing.
- **Regression check:** re-run the Phase 2 delivery-reliability tests
  against the real broker (including deliberately interrupting the
  RabbitMQ connection) to confirm retry/backoff/ack behavior designed
  against the local stub holds against the real client library.

### Final Phase / Step E — Reader Status API ✅ Complete (merged 2026-11-19)

- `POST /status` via `net/http.ServeMux`, API-key middleware, request/
  response shapes per README §11.
- Sources its answers from the now-real PostgreSQL `file_management`
  table (Step A).

### Final Phase / Step F — Log Rotation ✅ Complete (merged 2026-11-19)

- Agent mode schedules cleanup of completed files one calendar month
  after registration in UTC. PostgreSQL coordinates cleanup with processing
  and keeps the record until MinIO confirms object deletion or absence.
  Failed cleanup is retried on later runs.
- Build, vet, unit tests, PostgreSQL and MinIO integration tests, and a
  local scheduled-run deployment smoke test passed. See
  [Step F validation](2026-11-19-log-rotation/validation.md).

### Final Phase / Step G — End-to-End Hardening ✅ Complete (2026-11-20)

The [Step G validation report](2026-11-19-end-to-end-hardening/validation.md)
records the real-service workflow and recovery checks. The Docker-backed
integration and Compose smoke gates remain open.

- Verified Reader's workflow and restart recovery with an HTTP Agent stub,
  PostgreSQL, MinIO, RabbitMQ, and the Status API.
- Mapped the README §17 checks to the unit, integration, and workflow suites.
- Updated `.env.example`, Docker Compose, and the run documentation.
  CI runs the Docker-backed suites and Compose smoke check.

**Exit criteria (Final Phase as a whole):** Every architectural
requirement in README §15 and every guarantee in README §16 is true
against real infrastructure — not just against the local adapters — with
the Phase 2/3 reliability and performance test suites re-validated at
each integration step, not just re-written for it.

---

## Why Infrastructure Moves Last

This ordering is a deliberate trade-off, not an oversight:

- **What's hardest to get right ships first.** Batch correctness,
  recovery, retry semantics, and validation are where subtle bugs live.
  Debugging them is far easier against a local file and an in-memory
  fake than against a flaky network call to a shared RabbitMQ/MinIO
  instance.
- **Infrastructure risk is isolated, not ignored.** Every port has a real
  adapter built in the Final Phase, and every reliability/performance
  test from Phases 2–3 is explicitly re-run against each real adapter as
  it's introduced (see the "regression check" notes above) — infra
  integration doesn't get to skip the reliability bar the core already
  met.
- **The core is independently demonstrable early.** Stakeholders can see
  Reader correctly process a real large file — extraction, validation,
  normalization, hashing, crash-recovery, all of it — before a single
  line of Agent/MinIO/DB/RabbitMQ integration code exists.

## Open Items to Resolve Before/During Implementation

1. ~~**Regex tie-break rule** when multiple active patterns match~~ —
   **Resolved in Phase 1:** the pattern matching the most lines in the
   5,000-line window wins; on an exact tie, the lowest pattern `ID`
   wins. See `specs/2026-11-09-core-file-processing-pipeline/requirements.md`.
   Still provisional pending real pattern data, per that doc.
2. **Regex engine sufficiency** — confirm stdlib `regexp` (RE2) covers
   the real predefined patterns, or switch to `regexp2` (flagged in
   `tech-stack.md`). Still open as of Phase 1: the engine is isolated
   behind `internal/processing/regexengine`, so switching later touches
   only that package. Should be confirmed against real pattern data
   before Phase 3's performance work takes the current engine choice
   for granted.
3. **Test infrastructure strategy** for Final Phase integration tests
   against real RabbitMQ/MinIO/Postgres — dedicated shared test
   instances vs. `testcontainers-go` (flagged in `tech-stack.md`).
