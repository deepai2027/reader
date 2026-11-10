# Plan — MVP: Reliability + Full Infrastructure Integration

Reliability is built and proven against the Phase 0/1 local adapters
first (Groups 1–4), then each real infrastructure adapter replaces its
local counterpart one at a time, in the roadmap's existing A–D order
(Groups 5–8) — every infra group ends by re-running the reliability
suite from Groups 1–4 against the thing that was just made real, so a
real adapter never gets to skip the bar the local one already cleared.

---

## 1. Checkpointing

**Depends on:** Phase 1 (`internal/processing/pipeline`, `ports.ProgressStore`).

- [ ] Change `pipeline.ProcessFile` so `Deps.Progress.Save` is called
      after **every batch**, not just once at the end — update
      `CurrentLine` and `MatchedCount` incrementally as each batch
      completes, with `Status` staying `reading` until the file is
      fully processed.
- [ ] The first `Save` call (once regex selection completes) must
      persist `SelectedRegexID` and `Status = reading` *before* any
      batch is processed, so a crash during the very first batch still
      has something to resume from.
- [ ] Unit tests: `Save` is called once per batch with correct
      cumulative `CurrentLine`/`MatchedCount`, the first `Save` happens
      before any record is published, a zero-match file still writes a
      final `Save` (as it does today, unchanged by this group).

**Done when:** a test can inspect the sequence of `Save` calls made
during a `ProcessFile` run and confirm progress is durable at
batch-granularity, not just at completion.

---

## 2. Recovery / Resume Workflow

**Depends on:** 1.

- [ ] On startup (or as a new `pipeline.Resume` entry point distinct
      from `ProcessFile`'s fresh-start path), call
      `Deps.Progress.FindReading`. If a file is found:
      - Reopen the file via `FileSource.Open` using the same `fileRef`
        (never re-download at this stage — that's Step B's concern
        once MinIO is real; for now this just proves the seek-and-
        resume logic against the local adapter).
      - Use `batch.New` as usual, then discard lines up to
        `CurrentLine` (or implement a seek — decide based on whether
        the underlying stream supports efficient seeking; document the
        choice) before resuming normal batch processing.
      - Reuse `SelectedRegexID` from the loaded `FileState` — regex
        selection must **not** run again on resume.
      - Resume `UniqueURLMessage` dedup state is **not** preserved
        across a restart in the MVP (Phase 1's `uniqueurl.Dedup` is
        in-memory and file-scoped, not persisted) — document this as a
        known MVP gap: a restart may re-emit a unique-URL message for a
        URL already emitted before the crash. This is a correctness
        gap worth flagging in `validation.md`, not silently accepted.
- [ ] Unit/integration tests: kill a simulated run mid-batch, resume,
      assert no line in `[1, CurrentLine]` is reprocessed, no line is
      skipped, `SelectedRegexID` is unchanged, `Priority` is unchanged.

**Done when:** README §17.3's core recovery scenarios pass against the
local adapters: resume from last line, reuse selected regex, no
duplicate processing of completed lines, local file retained until
success.

---

## 3. Reliable Message Delivery (Local Adapter)

**Depends on:** 1 (needs checkpointing in place so a mid-batch publish
failure has somewhere correct to leave `CurrentLine`).

- [ ] Build a new local `MessagePublisher` adapter (distinct from the
      Phase 0 stub, which stays as-is per its own doc comment) that
      wraps a sink with real retry/backoff logic using
      `config.RetryMaxAttempts` and `config.RetryBaseDelayMS`
      (exponential backoff).
- [ ] The adapter distinguishes `ports.ErrTransient` (retry up to
      `RetryMaxAttempts`, then surface as a hard failure) from
      `ports.ErrPermanent` (never retried, surfaced immediately) — test
      doubles in this group's tests should be able to inject either.
- [ ] A batch's progress (Group 1's `Save`) is only committed *after*
      that batch's records are successfully published — a batch that
      exhausts its retries must leave `CurrentLine` at the *previous*
      batch's end, so resume (Group 2) reprocesses exactly that batch,
      never more.
- [ ] Unit tests: a transient failure retries and eventually succeeds,
      a transient failure exhausts retries and surfaces an error
      without advancing `CurrentLine`, a permanent failure surfaces
      immediately without retrying, backoff delay grows as configured
      between attempts.

**Done when:** retry/backoff/failure-surfacing behavior is fully
proven against a local in-memory sink, independent of any real broker.

---

## 4. Fault-Injection Test Harness & Reliability Proof

**Depends on:** 1–3.

- [ ] A test harness that can inject failures at each seam: process
      crash mid-batch (simulate by stopping the test's call sequence
      partway through and starting a fresh `Resume` call), publisher
      returns transient errors, publisher returns permanent errors,
      corrupt/truncated local progress state.
- [ ] Prove, by test, every invariant from `specs/mission.md`:
      - No line is processed twice after a simulated crash+restart.
      - No line is skipped after a simulated crash+restart.
      - The selected regex is never re-selected or changed mid-file.
      - A file's priority survives a restart.
- [ ] Document the Group 2 unique-URL-dedup gap's test coverage here
      too: a test that demonstrates (not just asserts away) the
      re-emission behavior on restart, so it's visible and intentional
      rather than silently passing unnoticed.

**Done when:** the full README §17.3 recovery/workflow scenario list
passes against local adapters — this is the reliability bar every
subsequent infra swap (Groups 5–8) must clear again.

---

## 5. Final Phase / Step A — PostgreSQL Persistence

**Depends on:** 1–4 (reliability proven locally first).

- [ ] `docker-compose.yml` introduced for the first time — **only**
      `reader` + `postgres`, per the infrastructure constraint carried
      since Phase 0.
- [ ] `regex` and `file_management` tables + `golang-migrate`
      migrations, including seeding the real predefined Regex patterns
      (coordinate with whoever owns the actual pattern content — this
      spec provides the seeding mechanism, not pattern authorship).
- [ ] Real `ProgressStore` adapter backed by PostgreSQL (`pgx`),
      replacing the local adapter from Groups 1–4 behind the same
      `ports.ProgressStore` interface.
- [ ] Real `RegexSource` adapter reading active patterns from the
      `regex` table, replacing the Phase 0 stub's hardcoded list.
- [ ] **Regression check:** re-run every Group 4 fault-injection test
      against the real PostgreSQL-backed `ProgressStore` (via
      `testcontainers-go`), including a scenario that drops the DB
      connection mid-write, to confirm the reliability guarantees still
      hold with a real database and real transaction boundaries in the
      loop.

**Done when:** Group 4's full test suite passes a second time, this
time against a real, ephemeral PostgreSQL instance.

---

## 6. Final Phase / Step B — MinIO File Download

**Depends on:** 5 (file identity/metadata now has somewhere real to be
recorded alongside download).

- [ ] Real `FileSource` adapter: streaming download from MinIO
      (`minio-go`) into the local working directory, replacing the
      Phase 0 stub's local-path `os.Open`.
- [ ] Local file retained until processing completes successfully (per
      `specs/mission.md`); deleted locally on completion or confirmed
      no-match; **never** deleted from MinIO by normal processing.
- [ ] Update Group 2's resume workflow: on resume, confirm the local
      file still exists before reusing it; if it was lost (e.g. disk
      wiped between restarts), re-download from MinIO rather than
      failing — document this fallback explicitly, since it's a
      behavior Groups 1–4 couldn't test without a real object store.
- [ ] Integration tests (via `testcontainers-go`'s MinIO support):
      download succeeds and the local file matches the object's
      content, a file already present locally on resume is reused
      without re-downloading, a missing local file on resume triggers a
      fallback re-download.

**Done when:** a real file can be pulled from a real (ephemeral, test)
MinIO bucket and handed into the Phase 1 pipeline unmodified.

---

## 7. Final Phase / Step C — Agent Integration & Single-File Concurrency

**Depends on:** 5, 6 (needs somewhere to record file state and
somewhere to download from before there's anything meaningful to poll
*for*).

- [ ] Agent API client: polling loop, configurable interval,
      `file_id`/`priority` retrieval, built strictly against the
      README's documented contract (see `requirements.md`'s new open
      item on this).
- [ ] Single-file-processing enforcement against the real flow: check
      `ProgressStore.FindReading` before polling; if a file is already
      `reading`, don't poll.
- [ ] Handle the Agent's `409 file_in_reading` response as an expected
      wait condition (log at debug, wait for the next poll interval),
      not an error.
- [ ] File receipt workflow: on a successful poll, persist initial
      `FileState`, trigger Group 6's download, then hand off into the
      Phase 1 pipeline (now checkpointed per Groups 1–4).
- [ ] Integration tests against an `httptest.Server` fake Agent (per
      `requirements.md` Decision 5, not `testcontainers-go` — there's no
      real Agent container): happy-path poll, `409` handled as a wait
      not an error, malformed/unexpected response handled without a
      crash.

**Done when:** Reader can poll a fake-but-contract-accurate Agent
endpoint, receive a file reference, and correctly refuse to poll again
while a file is `reading`.

---

## 8. Final Phase / Step D — Real RabbitMQ Delivery

**Depends on:** 1–4 (reliability/retry semantics), 7 (something now
triggers real processing end-to-end).

- [ ] Real `MessagePublisher` adapter: `amqp091-go`, two topics (Full
      Record, Unique URL), `x-max-priority` queue configuration matching
      the Agent's priority range, confirm-mode publishing — replacing
      the Group 3 local adapter behind the same `ports.MessagePublisher`
      interface and the same `ErrTransient`/`ErrPermanent` contract.
- [ ] **Regression check:** re-run Group 3's and Group 4's delivery-
      reliability tests against a real broker (via `testcontainers-go`),
      including a scenario that interrupts the RabbitMQ connection
      mid-publish, to confirm retry/backoff/failure-surfacing behavior
      designed against the local adapter holds against the real client
      library.

**Done when:** Group 3/4's delivery tests pass a second time against a
real, ephemeral RabbitMQ instance, and published messages can be
independently consumed and inspected in the test.

---

## 9. MVP End-to-End Integration & CI

**Depends on:** 1–8.

- [ ] A full real-infrastructure integration test: fake Agent
      (`httptest`) → real MinIO → Reader → real PostgreSQL → real
      RabbitMQ, covering the README §14 workflow for everything in this
      MVP's scope (explicitly not touching Status API or Log Rotation,
      which don't exist yet).
- [ ] Include at least one full crash-and-resume scenario in this
      end-to-end test, not just in the narrower Groups 1–4/5/8
      regression checks — proving the *whole* chain survives an
      interruption, not just each piece individually.
- [ ] CI pipeline updated to spin up `testcontainers-go`-managed
      PostgreSQL, RabbitMQ, and MinIO for this test — the first time
      this project's CI has used service containers (a deliberate,
      documented break from Phase 0/1's zero-container CI, per
      `requirements.md` Decision 6).
- [ ] `.env.example` updated with the new infra connection variables
      (DB, MinIO, RabbitMQ, Agent) introduced across Groups 5–8.
- [ ] `CHANGELOG.md` updated and the version bumped (coordinate the
      exact version number with whoever owns versioning policy — see
      the open question raised after the Phase 0/1 merges about whether
      versions track roadmap phases or something else).

**Done when:** `validation.md` in full passes — automated tests only,
including the real-infrastructure end-to-end test — and the PR is ready
to open against `main`.
