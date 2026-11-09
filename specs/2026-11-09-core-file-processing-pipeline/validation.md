# Validation — Core Implementation: The File-Processing Pipeline

Per the decision in `requirements.md`, merging this phase requires
**both** a green automated run **and** a completed manual review
checklist — same bar as Phase 0, but the manual checklist here is
mostly about *correctness* (tie-break behavior, hash determinism,
credential safety), not just architecture, because this phase is where
Reader's actual logic lives for the first time.

## 1. Automated Checks (required, CI-enforced)

```bash
make build
make vet
make test
```

Pass criteria:

- [ ] `go build ./...` succeeds with no errors or warnings.
- [ ] `go vet ./...` reports nothing.
- [ ] `go test ./...` passes, with **zero** external services running —
      every test in this phase uses local fixture files and/or the
      Phase 0 stub adapters, never a real database, broker, or object
      store.
- [ ] Every unit from `plan.md` Groups 1–7 (batch reader, regex
      selection, extraction, validation, normalization, hashing/record
      assembly, unique-URL derivation) has table-driven tests covering
      at least the specific edge cases listed in that group's plan
      entry — not just a single happy-path test per function.
- [ ] The Group 9 fixture library exists under
      `internal/processing/testdata/` and Group 8's end-to-end test
      exercises every fixture file.
- [ ] CI is green on the PR using the exact same three `make` commands.
- [ ] No new third-party dependencies beyond what's already named in
      `specs/tech-stack.md` (`golang.org/x/net/publicsuffix` is already
      accounted for there; nothing else should be needed for this
      phase).

## 2. Manual Review Checklist (required, human sign-off)

### Correctness — Regex Selection & Tie-Break

- [ ] The tie-break rule matches `requirements.md` exactly: most
      matching lines wins; on an exact tie, lowest pattern `ID` wins.
      Walk through at least one test case by hand against the code, not
      just the test's assertion — confirm the *logic* implements the
      rule, not just that the test happens to pass.
- [ ] A pattern that fails to compile is skipped with a logged warning,
      not a crash, and is correctly excluded from both the match-count
      comparison and the tie-break.
- [ ] The no-match path produces `matched_count = 0` and a `done`
      status with **no partial side effects** — no records published,
      no partial progress written, before the no-match conclusion is
      reached.

### Correctness — Extraction, Validation, Normalization

- [ ] Extraction only reads from the selected pattern's named capture
      groups — no hardcoded assumption about group order that would
      break if a real pattern defines groups in a different order.
- [ ] Validation's length bounds are read from `internal/config` (not
      hardcoded), and boundary values (exactly min, exactly max) are
      confirmed to pass, not just rejected-outside-bounds.
- [ ] Normalization's documented edge-case decisions (scheme-less URLs,
      default-port stripping, IP-address hosts) in `plan.md` Group 5 are
      each reflected in both the code and a test — flag any case the
      implementer resolved differently than planned, since that's a
      spec deviation worth a second look, not necessarily wrong.

### Correctness & Safety — Hashing, Records, Unique URLs

- [ ] `record_hash` is computed from the **normalized** URL, verified by
      a test where the raw and normalized URLs differ and the hash only
      matches the normalized-URL expectation.
- [ ] The hash is reproducible across repeated runs and process
      restarts — no dependency on map iteration order, timestamps, or
      anything else non-deterministic creeping into the hash input.
- [ ] **Hard check:** `domain.UniqueURLMessage` values published during
      the end-to-end test (Group 8) contain no username or password
      data anywhere — this is a mission-level invariant
      (`specs/mission.md`'s data-safety principle), not just a nice-to-have.
- [ ] Unique-URL dedup is correctly scoped to a single file's processing
      run — confirm by test, not just by reading the code, since
      cross-run leakage would be a real (if currently invisible) data
      problem once the Final Phase's real publisher is in place.

### Architecture & Scope Discipline

- [ ] No checkpointing, retry, or recovery logic has been added in this
      phase — that's Phase 2. If any crept in, it should move to the
      Phase 2 branch rather than merge here.
- [ ] No real infrastructure adapter (PostgreSQL, RabbitMQ, MinIO,
      Agent) has been introduced — Phase 1 still runs only against the
      Phase 0 stub adapters.
- [ ] The regex-engine seam (Group 2) is isolated enough that swapping
      `regexp` for `regexp2` later would touch only the regex-selection
      package's internals, not its exported function signatures or
      anything calling into it.
- [ ] `cmd/reader/main.go`'s new manual-run entry point is clearly a
      temporary/manual trigger (documented as such), not something that
      could be mistaken for the real Agent-driven flow.

### Code Quality & Documentation

- [ ] Every exported function/type in the new `internal/processing/...`
      packages has a doc comment.
- [ ] Known gaps called out in `plan.md` (e.g. `ModifiedDate`/`CreatedAt`
      placeholders pending real file metadata) are also reflected in
      code comments at the relevant call sites, not just in the spec.

## 3. Merge Criteria

This phase is ready to merge into `main` when:

1. All boxes in Section 1 (Automated Checks) are checked and CI is
   green on the PR.
2. All boxes in Section 2 (Manual Review Checklist) are checked by a
   reviewer who is not the author.
3. The reviewer can point to the specific test(s) proving the tie-break
   rule, hash determinism, and the no-credentials-on-unique-URL
   invariant — these three are the highest-risk correctness properties
   in this phase and should not be taken on faith.
4. Phase 2 can start immediately against the merged code without
   needing to change any exported signature in
   `internal/processing/...` — if checkpointing/recovery work looks
   like it will require reshaping `ProcessFile`'s signature right away,
   that's a signal the orchestration boundary (Group 8) needs another
   look before merging.

## 4. What This Phase Does *Not* Validate

- Crash recovery or checkpoint persistence across restarts (Phase 2) —
  Phase 1's `ProcessFile` runs start-to-finish in one call; nothing here
  proves it behaves correctly if interrupted partway through.
- Performance or memory behavior under real multi-GB files or
  adversarial regex input (Phase 3) — Phase 1's "memory-bounded" claim
  is proven by the Group 1 unit test's measurement, not by a dedicated
  stress test.
- Anything involving real infrastructure (Final Phase).

A green Phase 1 merge means "the pipeline produces correct output,
proven against local fixtures" — not "Reader survives a crash" or
"Reader survives production load." Those are Phases 2 and 3's claims to
make, respectively.
