# Plan — Core Implementation: The File-Processing Pipeline

Task groups follow the data's own path through the pipeline: each group
depends only on the group(s) above it. Keep groups small — if a group
starts accumulating logic that belongs to the next stage, split it.

## Implementation Status (2026-11-09)

All 9 groups are implemented, each as its own commit on
`feature/2026-11-09-core-file-processing-pipeline`. As with Phase 0:
this sandbox has no Go toolchain and no network access, so none of this
was compiler-verified. All code was manually reviewed (brace/paren
balance, import-usage sweep, cross-package signature consistency), but
**`go build`/`go vet`/`go test` have not actually been run.**

Two things need to happen on a real machine before this is genuinely
done, beyond the usual `make build vet test`:

1. **`go mod tidy`** (or `go get golang.org/x/net/publicsuffix`) —
   Group 5 added `golang.org/x/net` as a real dependency by hand-editing
   `go.mod`; `go.sum` was never generated here, so the first real build
   needs network access to fetch and checksum the module.
2. Fixture files under `internal/processing/testdata/` were built as
   part of Group 8 (the end-to-end test needed them immediately), not
   held back for Group 9 as originally planned — noted here so the
   history makes sense when reading the commits in order.

| Group | Status |
|---|---|
| 1. Batch Reader | Implemented |
| 2. Regex Selection | Implemented (+ `regexengine` seam package) |
| 3. Extraction | Implemented |
| 4. Validation | Implemented |
| 5. Normalization | Implemented — **needs `go mod tidy` for go.sum** |
| 6. Hashing & Record Assembly | Implemented |
| 7. Unique-URL Derivation | Implemented |
| 8. End-to-End Pipeline Orchestration | Implemented, incl. fixtures + manual CLI trigger |
| 9. Fixture-Driven Test Suite & Phase Exit Verification | Fixtures done in Group 8; manual checklist below still needs a human pass with a real build |

---

## 1. Batch Reader ✅ implemented

**Depends on:** Phase 0 (`ports.FileSource`, `internal/config`).

- [ ] New package, e.g. `internal/processing/batch`: a line-based reader
      wrapping an `io.ReadCloser` (from `ports.FileSource.Open`),
      yielding `domain.RawLine` values, batched to `config.BatchSize`
      lines per call.
- [ ] Use `bufio.Scanner` (or `bufio.Reader` with manual line splitting,
      if a single line could plausibly exceed `bufio.Scanner`'s default
      token size — decide based on realistic max line length and
      document the choice) so no full-file read ever happens, even
      temporarily.
- [ ] Line numbers are 1-based and monotonically increasing across
      batches, so later groups (and Phase 2's checkpointing) can rely on
      them as a stable position marker.
- [ ] Unit tests: empty file, single-line file, file with no trailing
      newline, file with blank lines, a batch boundary falling exactly
      on the last line, a synthetic large file (memory-bounded —
      measure actual memory use in the test, not just assume it).

**Done when:** the reader can stream an arbitrarily large local file in
fixed, bounded memory, batch by batch, verified by test.

---

## 2. Regex Selection ✅ implemented

**Depends on:** 1 (needs lines to test patterns against), Phase 0
(`ports.RegexSource`).

- [ ] New package, e.g. `internal/processing/regexselect` (mirrors the
      eventual `internal/regexselect` real-adapter package named in
      `specs/tech-stack.md`'s layout): given the first up-to-5,000 lines
      and the active patterns from `RegexSource`, determine the
      selected pattern.
- [ ] Implement the tie-break rule from `requirements.md`: count
      matching lines per pattern within the scanned window; the
      pattern with the most matches wins; on an exact tie, the
      pattern with the lowest `ID` wins.
- [ ] No-match path: if zero patterns match any line in the window,
      return an explicit "no match" result (not an error) — the caller
      (Group 8) turns this into the `done`/zero-matches file outcome.
- [ ] Keep the regex engine itself isolated behind a small internal
      interface or a single clearly-named function (e.g.
      `compileAndCount(pattern domain.RegexPattern, lines []domain.RawLine) (count int, err error)`)
      so swapping `regexp` for `regexp2` later (open item, see
      `requirements.md`) touches only this group's internals.
- [ ] Unit tests: single pattern matches, multiple patterns match with a
      clear winner, multiple patterns tie and the lowest-ID fallback
      applies, zero patterns match, a pattern that fails to compile
      (malformed regex — must not crash the whole selection process,
      should be skipped with a logged warning and treated as
      non-matching).

**Done when:** given a fixed set of patterns and a fixed set of lines,
the selected pattern (or "no match") is deterministic and correct,
verified by test — including the tie-break edge case.

---

## 3. Extraction ✅ implemented

**Depends on:** 2 (needs a selected pattern).

- [ ] New package or file, e.g. `internal/processing/extract`: apply the
      selected `domain.RegexPattern` to a `domain.RawLine`, producing a
      `domain.ExtractedCandidate` via named capture groups (`url`,
      `username`, `password` — confirm these are the actual group names
      once real patterns are available; stub pattern in
      `internal/ports/stub` already uses this naming).
- [ ] A line that doesn't match the selected pattern at all (can happen
      after the first 5,000-line window, later in the file) is dropped
      and counted, not an error.
- [ ] Unit tests: a matching line extracts all three fields correctly, a
      non-matching line is dropped and counted, a line matching the
      pattern but missing one named group (depending on pattern design)
      is handled explicitly (dropped, not a panic).

**Done when:** extraction is pure, deterministic, and fully isolated
from file I/O and regex selection — it only depends on an already
selected pattern and a single line.

---

## 4. Validation ✅ implemented

**Depends on:** 3 (needs an `ExtractedCandidate`).

- [ ] New package or file, e.g. `internal/processing/validate`: checks
      an `ExtractedCandidate`'s username/password against
      `config.MinUsernameLength`/`MaxUsernameLength` and
      `MinPasswordLength`/`MaxPasswordLength`, plus basic structural
      sanity (non-empty URL, non-empty required fields per README §7.2
      — confirm exact structural rules against the README's validation
      section as the real patterns are finalized).
- [ ] Returns a clear valid/invalid result with a reason (for counting
      and debug logging), not just a bool — reasons matter for
      diagnosing why a file's `matched_count` came out lower than
      expected.
- [ ] Unit tests: valid candidate passes, username too short, username
      too long, password too short, password too long, empty URL,
      boundary values (exactly at min/max length, which must pass).

**Done when:** every validation rule from `specs/tech-stack.md`/README
§7.2 has a corresponding table-driven test case, including boundaries.

---

## 5. Normalization (URL + Domain/Subdomain/Suffix/Port) ⚠️ implemented, go mod tidy required

**Depends on:** 4 (only validated candidates get normalized — don't do
wasted work normalizing data that's about to be rejected).

- [ ] New package or file, e.g. `internal/processing/normalize`: given a
      validated URL string, produce a normalized form (consistent
      scheme, lowercased host, trailing-slash handling) using `net/url`.
- [ ] Extract domain/subdomain/suffix/port from the normalized URL's
      host using `golang.org/x/net/publicsuffix`, per
      `specs/tech-stack.md`.
- [ ] Decide and document edge-case behavior: URL with no scheme
      (assume `http://`? reject?), URL with an explicit default port
      (`:80`/`:443` — strip, since it's redundant?), IP-address hosts
      (no domain/suffix — `Domain`/`Subdomain`/`Suffix` become empty,
      not an error).
- [ ] Unit tests: scheme-less URL, uppercase host, trailing slash
      present/absent, explicit default port, explicit non-default port,
      multi-part suffix (e.g. `.co.uk`), IP-address host, URL with
      userinfo in it (must not leak into the normalized form if that's
      not part of the expected shape).

**Done when:** normalization is deterministic (same input always
produces the same output) and every documented edge case has a test.

---

## 6. Hashing & Record Assembly ✅ implemented

**Depends on:** 5 (needs the normalized URL and extracted domain parts).

- [ ] New package or file, e.g. `internal/processing/record`: compute
      `record_hash = sha256(normalizedURL + "|" + username + "|" + password)`
      (confirm exact delimiter/field order against README §8 — the
      roadmap's working assumption is `url|user|password`) and assemble
      the final `domain.ValidatedRecord`.
- [ ] `ModifiedDate`/`CreatedAt` are populated from whatever the caller
      provides (file metadata isn't available yet in Phase 1 — pass
      through a placeholder/current time for now, document this as a
      known gap closed properly in the Final Phase when `FileSource`
      has real MinIO metadata to offer).
- [ ] Unit tests: hash is deterministic (same inputs → same hash, always,
      across repeated calls and even across process restarts — no
      reliance on map ordering or anything non-deterministic), different
      inputs produce different hashes (no trivial collisions in test
      data), hash uses the *normalized* URL, not the raw extracted one
      (construct a test case where this would differ and assert on it).

**Done when:** `ValidatedRecord` assembly is fully deterministic and
tested, and the hash formula is pinned down by a test that would fail if
someone "fixed" the delimiter or field order later without updating the
spec.

---

## 7. Unique-URL Derivation ✅ implemented

**Depends on:** 6 (needs normalized URLs; conceptually parallel to
record assembly, not sequential after it, but implemented after so the
dedup logic has real `ValidatedRecord` data to work from in tests).

- [ ] New package or file, e.g. `internal/processing/uniqueurl`: given a
      stream of normalized URLs (with their domain/subdomain/suffix)
      across one file's processing run, track which have already been
      seen and emit a `domain.UniqueURLMessage` only for the first
      occurrence of each normalized URL.
- [ ] Attach the file's `Priority` (from `domain.FileState`) to every
      emitted message.
- [ ] Hard invariant, enforced by test: `UniqueURLMessage` never carries
      a username or password field — the struct itself already
      prevents this (Phase 0), but add a test that would fail if a
      field were ever added to the struct without updating this logic's
      call site to deliberately ignore it.
- [ ] Unit tests: same URL appearing twice in a file → one message
      emitted, two different URLs → two messages, dedup state is scoped
      to one file's processing run (verify by constructing two separate
      runs and confirming no cross-run leakage, since the real adapter
      will need this scoping too).

**Done when:** dedup is correct and scoped, and the credential-exclusion
invariant has a test guarding it, not just a comment.

---

## 8. End-to-End Pipeline Orchestration ✅ implemented

**Depends on:** 1–7 (wires every stage together).

- [ ] New package or file, e.g. `internal/processing/pipeline`: a single
      entry point (e.g. `ProcessFile(ctx, fileRef string, deps Deps) error`)
      that:
      1. Opens the file via `FileSource`.
      2. Reads the first 5,000-line window, runs regex selection
         (Group 2).
      3. On no-match: writes a `done`/zero-match `FileState` via
         `ProgressStore` and returns.
      4. On match: batches through the rest of the file (Group 1),
         extracting (3), validating (4), normalizing (5), hashing/
         assembling records (6), and deriving unique URLs (7) per line.
      5. Publishes each valid record and unique-URL message via
         `MessagePublisher` (Phase 0 stub).
      6. Writes a final `done` `FileState` via `ProgressStore`.
- [ ] This orchestration function takes its dependencies
      (`ProgressStore`, `MessagePublisher`, `RegexSource`, `FileSource`,
      `config.Config`) as explicit parameters/struct fields — never as
      globals — so Phase 2 can wrap or replace pieces of this without
      rewriting it, and so it's trivially testable with fakes.
- [ ] Wire a minimal CLI/manual entry point in `cmd/reader/main.go` (a
      flag or hardcoded path for now — a real Agent-driven trigger is
      Final Phase) so the whole pipeline can be run against a real local
      file by hand, not just by `go test`.
- [ ] Integration-style test (still zero external services, since
      everything is stub/local): run `ProcessFile` against a handful of
      fixture files (see Group 9) and assert on the full set of
      published records/messages and the final `FileState`.

**Done when:** a developer can point the binary at a local file and see
it fully processed end-to-end, with every stage from Groups 1–7
exercised in one real run, not just in isolated unit tests.

---

## 9. Fixture-Driven Test Suite & Phase Exit Verification ⚠️ blocked on real build

**Depends on:** 1–8.

- [ ] Build a small library of fixture files under, e.g.
      `internal/processing/testdata/`: a well-formed file, a file with
      mixed valid/invalid lines, a file with no matching lines, an empty
      file, a file with only blank lines, a file with duplicate URLs
      (for unique-URL dedup), a file exercising each normalization edge
      case from Group 5.
- [ ] Run through `validation.md` in full: automated checks + manual
      review checklist.
- [ ] Confirm `specs/mission.md`'s data-safety principle (no credentials
      on the Unique URL topic) and `specs/tech-stack.md`'s
      minimal-dependency bias are both visibly true in what was built.
- [ ] Open the PR into `main` only after this checklist is complete.
