# Validation — MVP: Reliability + Full Infrastructure Integration

Per the decision in `requirements.md`, this phase's merge bar is
**automated tests only** — a deliberate departure from Phase 0/1's
"automated + manual review" bar. There is no separate human-checklist
gate here; CI itself has to carry the full verification weight. That
means the CI pipeline's scope (what it actually spins up and runs) *is*
the validation bar, not a proxy for it.

## 1. Automated Checks (required, CI-enforced)

```bash
make build
make vet
make test                 # unit tests, zero external services
make test-integration      # new target: spins up testcontainers-go
                            # Postgres/RabbitMQ/MinIO, runs integration tests
```

Pass criteria:

- [ ] `go build ./...` succeeds with no errors or warnings.
- [ ] `go vet ./...` reports nothing.
- [ ] `go test ./...` (unit tests) passes with zero external services —
      this must still work exactly as it does today for Phase 0/1 code;
      nothing in this phase should make the plain `go test ./...` path
      start requiring infrastructure.
- [ ] A new, separately tagged integration test suite (e.g. `//go:build
      integration`) passes via `make test-integration`, using
      `testcontainers-go` to provide ephemeral PostgreSQL, RabbitMQ, and
      MinIO — no dependency on externally provisioned shared test
      infrastructure.
- [ ] CI runs **both** `make test` and `make test-integration` on every
      PR — this is the first time this project's CI uses service
      containers (see `requirements.md` Decision 6); the CI workflow
      file itself should make clear which containers are test-only
      scaffolding versus anything resembling the production
      `docker-compose.yml` (which remains `reader` + `postgres` only).

## 2. Reliability Guarantees (required, proven by test — see plan.md Groups 1–4)

- [ ] **Checkpointing:** a test can observe `ProgressStore.Save` being
      called after every batch, not just at file completion.
- [ ] **Recovery:** a simulated crash-and-restart mid-file resumes from
      the correct line, reuses the previously selected regex, and
      reprocesses zero already-completed lines — proven first against
      local adapters (Group 4), then again against real PostgreSQL
      (Group 5's regression check).
- [ ] **Reliable delivery:** transient publish failures retry per
      `config.RetryMaxAttempts`/`RetryBaseDelayMS` and eventually either
      succeed or surface a clear failure without silently dropping data;
      permanent failures surface immediately without retrying — proven
      first against a local sink (Group 3), then again against real
      RabbitMQ, including a connection-interruption scenario (Group 8's
      regression check).
- [ ] **Known gap is tested, not just documented:** the unique-URL
      re-emission-on-restart gap (flagged in `plan.md` Group 2) has a
      test that demonstrates the actual behavior, so it's a visible,
      intentional limitation rather than something that could silently
      regress further (e.g. into reprocessing full records, not just
      re-emitting unique-URL messages).

## 3. Infrastructure Integration (required, proven by test — see plan.md Groups 5–9)

- [ ] **PostgreSQL:** migrations apply cleanly against a fresh database
      (via `testcontainers-go`); seeded Regex patterns are present and
      `IsActive`-filterable; `file_management` rows reflect real state
      transitions through a full processing run.
- [ ] **MinIO:** a file downloads correctly from a real (ephemeral,
      test) bucket; the local-file retention rule (keep until success,
      never delete the MinIO original during normal processing) holds;
      the "local file missing on resume" fallback re-download path is
      exercised by test, not just implemented.
- [ ] **Agent integration:** the polling client works against an
      `httptest.Server` fake built to the README's documented contract;
      `409 file_in_reading` is handled as a wait condition, confirmed by
      a test that asserts no error is surfaced and the next poll is
      correctly delayed; single-file enforcement is proven by a test
      that attempts a second poll while a file is `reading` and asserts
      it's refused.
- [ ] **RabbitMQ:** both topics receive independently published
      messages (never combined); priority queue configuration
      (`x-max-priority`) matches the Agent's priority range; a message
      published to the Unique URL topic is inspected by test and
      confirmed to carry no username/password field — this is the same
      hard invariant from Phase 1's validation, now re-proven against a
      real broker instead of a capturing fake.

## 4. End-to-End (required, see plan.md Group 9)

- [ ] One full-chain integration test: fake Agent → real MinIO → Reader
      → real PostgreSQL → real RabbitMQ, covering the complete MVP
      scope.
- [ ] That same end-to-end test includes at least one crash-and-resume
      scenario spanning the whole chain, not just an individual
      component's regression check.
- [ ] `.env.example` reflects every new variable this phase introduces
      (DB, MinIO, RabbitMQ, Agent connection settings), and
      `config.Load()` (or its MVP-phase extension) fails fast with a
      clear error if any required one is missing — consistent with
      Phase 0's config-loading behavior.

## 5. Merge Criteria

This phase is ready to merge into `main` when:

1. All boxes in Sections 1–4 are checked and **both** `make test` and
   `make test-integration` are green on the PR.
2. The PR description explicitly lists what's *not* included (Status
   API, Log Rotation, Phase 3 performance hardening) so this is never
   mistaken for "Reader is fully done" by anyone reading the merge
   commit later.
3. The PR description also explicitly lists the known gaps this spec
   intentionally accepted (unique-URL re-emission on restart, Agent
   contract assumptions unverified against a real Agent instance) —
   not as blockers, but as documented, intentional scope boundaries.

No separate manual review checklist is required to merge (per
`requirements.md` Decision 3) — but a reviewer should still read the PR
and confirm points 2 and 3 above are actually present in its
description, since those aren't things CI can check for itself.

## 6. What This Phase Does *Not* Validate

- Reader's behavior under real production load or multi-GB files —
  that's Phase 3, not this MVP.
- Correct behavior with real (not fixture/test) Regex patterns, since
  the real pattern content is a separate content/coordination concern
  from this spec (see `plan.md` Group 5).
- Anything involving the Status API or Log Rotation, since neither
  exists yet.
- Compatibility with the real Agent deployment specifically — only with
  the Agent contract as documented in the README (see
  `requirements.md`'s new open item on this).

A green MVP merge means "Reader runs end-to-end against real
infrastructure and survives a crash correctly" — not "Reader is
production-hardened" or "Reader is feature-complete." Those remain
later work.
