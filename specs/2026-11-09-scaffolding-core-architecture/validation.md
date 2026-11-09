# Validation — Project Scaffolding & Core Architecture

Per the decision in `requirements.md`, merging this phase requires
**both** a green automated run **and** a completed manual review
checklist. Neither alone is sufficient: the automated checks confirm
the code builds and does what its own tests claim; the manual checklist
confirms the architecture is actually the one described in
`specs/mission.md` and `specs/tech-stack.md`, which automated tests
can't verify on their own.

## 1. Automated Checks (required, CI-enforced)

Run locally via the Makefile, enforced in CI on every push/PR:

```bash
make build   # go build ./...
make vet     # go vet ./...
make test    # go test ./...
```

Pass criteria:

- [ ] `go build ./...` succeeds with no errors or warnings.
- [ ] `go vet ./...` reports nothing.
- [ ] `go test ./...` passes, with **zero** external services running —
      no Postgres, no RabbitMQ, no MinIO, no network access. If a test
      requires any of those to pass, it does not belong in this phase.
- [ ] CI run is green on the PR, using the exact same three commands
      (no CI-only special-casing that diverges from local `make`
      targets).
- [ ] `go.mod` contains **no new third-party dependencies** beyond what
      `specs/tech-stack.md` already names for this phase (which, per
      the minimal-dependency bias, should be none — everything in Phase
      0 is standard library).

## 2. Manual Review Checklist (required, human sign-off)

A reviewer (not the author) works through this list against the actual
diff before approving:

### Architecture & Boundaries

- [ ] `internal/ports` contains **only interfaces** — no concrete logic,
      no imports of a real driver/client library (`pgx`, `amqp091-go`,
      `minio-go`, etc.).
- [ ] Stub adapters (Task Group 4) are clearly separated from — and
      clearly labeled as distinct from — what will become the real
      Phase 1/2 local adapters. A reviewer unfamiliar with the roadmap
      should be able to tell, from the code alone, that these are
      temporary.
- [ ] `internal/domain` types match the field names/shapes already
      specified in the README and `specs/mission.md`/`specs/tech-
      stack.md` — no invented fields that don't trace back to a
      requirement, no missing fields that Phase 1 will immediately need
      to add (check against README §1.1, §1.2, §8).
- [ ] `RegexPattern`/`RegexSource` do not assume a specific regex engine
      (no `*regexp.Regexp` in the interface signature) — per the open
      item in `requirements.md`.
- [ ] `MessagePublisher`'s method signatures can plausibly express
      "transient vs. permanent failure" — Phase 2 retry logic should not
      need to change this interface to express that distinction.

### Configuration & Secrets

- [ ] `internal/config` loads **only** Phase 0–relevant variables
      (batch size, validation ranges, retry tuning) — no DB/MinIO/
      RabbitMQ/Agent variables have been added yet, even as unused
      placeholders.
- [ ] `.env.example` contains no real secrets, and matches exactly what
      `internal/config.Load()` actually reads (no drift between the
      example file and the code).
- [ ] Config validation failures produce clear, actionable error
      messages (a developer misconfiguring `BATCH_SIZE` should get a
      message that says so, not a generic parse error).

### Scope Discipline

- [ ] No Docker, Dockerfile, or `docker-compose.yml` present — confirms
      the "fully deferred to Final Phase" decision was respected.
- [ ] No regex matching, batching, extraction, validation, normalization,
      or hashing *logic* has been written — only the types and
      interfaces those will eventually sit behind. If real logic has
      crept in, it belongs in the Phase 1 branch, not here.
- [ ] No retry/backoff/checkpoint *logic* has been written — only
      interface shapes that anticipate it existing in Phase 2.

### Code Quality & Documentation

- [ ] Every exported type and interface has a doc comment explaining
      its purpose and which future phase implements/consumes it.
- [ ] Package names and directory layout match `specs/tech-stack.md`'s
      project layout exactly (or any deviation is called out and
      justified in the PR description).
- [ ] Commit history on the branch is reasonably clean (squash
      fixup-style commits before requesting review, if the team's
      convention expects that).

## 3. Merge Criteria

This phase is ready to merge into `main` when:

1. All boxes in Section 1 (Automated Checks) are checked and CI is
   green on the PR.
2. All boxes in Section 2 (Manual Review Checklist) are checked by a
   reviewer who is not the author.
3. The reviewer explicitly confirms, in the PR approval, that Phase 1
   can start immediately against the merged code without needing any
   interface changes — if Phase 1 work is likely to require changing a
   port signature right away, that's a signal this phase isn't actually
   done yet, and the interface should be revisited before merging.

## 4. What This Phase Does *Not* Validate

To keep scope honest: this validation only covers scaffolding and
architecture. It does **not** validate:

- Correctness of any processing logic (nothing exists yet — that's
  Phase 1's validation).
- Fault tolerance or recovery behavior (Phase 2).
- Performance under large files (Phase 3).
- Anything involving real infrastructure (Final Phase).

A green Phase 0 merge means "the foundation is sound to build on," not
"Reader works."
