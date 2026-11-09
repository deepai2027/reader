# Reader — Mission

## Vision

Reader is the component that turns raw, unstructured text files into clean,
trustworthy, structured records — without ever losing a line of progress,
no matter what fails along the way.

Raw data goes in. Valuable insight comes out. In between, the system must
behave as if it has perfect memory: every restart, crash, or network hiccup
picks up exactly where it left off.

## Primary Value Proposition: Reliability & Fault-Tolerance

Reader's defining promise is **durability of progress, not just correctness of output**.
Throughput and extraction quality matter, but every other property in this
project is subordinate to one guarantee:

> **No file is ever lost, silently skipped, or double-processed — regardless
> of when or how the process stops.**

This shapes every design decision:

- **State lives in PostgreSQL, not memory.** The process can die at any line,
  in any batch, and recovery reconstructs exactly where it was.
- **One file at a time, with ownership made explicit.** No ambiguity about
  what is "in flight," no race between two workers claiming the same file.
- **Idempotent, resumable batches.** A batch that was interrupted mid-publish
  must not corrupt progress counters or cause duplicate downstream messages
  beyond what the architecture already tolerates (e.g., consumer-side
  deduplication via `record_hash`).
- **The source of truth in MinIO is never touched by normal processing.**
  Only the explicit, auditable Log Rotation path deletes original files —
  processing failures must never cascade into data loss upstream.
- **Degraded conditions fail loudly and safely**, not silently. A file with
  no matching Regex is explicitly marked `done` with zero matches, not left
  in an ambiguous state.

Scale and data quality are real requirements (see below), but when they are
ever in tension with recoverability or data safety, recoverability wins.

## Problem Statement

Large volumes of unstructured text files arrive continuously from an
upstream Agent. Each file may be gigabytes in size, in one of several
unpredictable layouts, and must be turned into structured
URL/username/password records plus deduplicated URL signals — without:

- loading entire files into memory,
- processing a file more than once,
- losing track of progress across restarts or deployments,
- or blocking the rest of the pipeline while one file is being handled.

## Guiding Principles

1. **Persisted state over in-memory state.** If it matters after a restart,
   it lives in PostgreSQL.
2. **One file, one owner, one path.** Single-file processing is a hard
   invariant, not a soft target.
3. **Batches are the unit of safety.** Progress is committed batch-by-batch,
   not file-by-file, so failure never costs more than one batch of rework.
4. **Separation of concerns between storage tiers.** MinIO is the permanent
   source of truth; the local disk is a disposable working copy; PostgreSQL
   is the authoritative processing ledger.
5. **Validated data only leaves the system.** Nothing unvalidated reaches
   RabbitMQ; nothing sensitive (username/password) reaches the Unique URL
   topic.
6. **Configuration over code changes.** Operational tuning (batch size,
   polling interval, retention) is an environment concern, not a
   redeploy concern.
7. **Testable by construction.** Every component that influences correctness
   or recovery (regex selection, batch math, hash generation, retention
   eligibility) is designed to be unit-testable in isolation from
   Postgres/RabbitMQ/MinIO.

## Success Criteria

Reader is successful when:

- A file can be killed mid-processing (`kill -9`, container restart, crash)
  at any point and resume with **zero reprocessed lines and zero lost
  lines**.
- Only one file is ever in `reading` state at a time, verifiably.
- Every valid record published to RabbitMQ has a deterministic,
  reproducible `record_hash`.
- The Agent can always answer "what's the state of file X?" accurately via
  the Status API, even for files completed weeks ago (within retention).
- Operators can tune batch size, polling interval, and retention without
  touching code or redeploying a new image.
- Log Rotation never deletes a file or record that is still being actively
  processed.
- The core business logic (regex selection, extraction, validation,
  normalization, hashing, batch math, retention eligibility) has automated
  test coverage independent of live infrastructure.

## Non-Goals (Explicitly Out of Scope)

- **Implementing the Agent or its API.** Reader only consumes it.
- **Operating RabbitMQ or MinIO infrastructure.** Reader connects to
  existing, externally managed instances; it does not provision or
  administer them.
- **Curating or authoring the content of Regex patterns.** Reader provides
  the mechanism (storage, activation, selection, matching); the patterns
  themselves are a data/content concern seeded via migration.
- **Consuming its own published messages.** Downstream processing of the
  Full Record and Unique URL topics is out of scope for this module.
- **Long-term analytics or reporting on processed data.** Reader reports
  live processing status only, not historical analytics.

## Primary Stakeholders

- **Agent team** — upstream producer and primary API consumer of Reader's
  Status API.
- **Downstream consumers of RabbitMQ topics** — depend on message shape,
  independence, and priority correctness, even though their implementation
  is out of scope here.
- **Operations** — depends on configurability, observability, and safe
  automated retention.
