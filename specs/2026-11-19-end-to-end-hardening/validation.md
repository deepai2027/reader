# End-to-End Hardening Validation

## Observable acceptance criteria

| Scenario | Passing evidence |
| --- | --- |
| Startup and Agent polling | Migrations apply and seeded regexes compile and match representative fixtures. Idle and `409 file_in_reading` responses create no work. A valid assignment starts processing and polling resumes after completion. Two Reader instances cannot process simultaneously. |
| Successful and no-match files | A mixed, multi-batch fixture produces expected counters, independently published Full Record and Unique URL messages, expected normalized URLs and hashes, and inherited URL priority. Invalid input produces no message. A no-match file finishes with zero matches, line zero, and no messages. Both paths remove their local copies and leave their MinIO sources available. |
| Checkpoint and restart | Before a dependency failure or interruption, PostgreSQL reflects only batches with successful delivery. Restart uses the saved regex and priority and resumes at the next uncommitted line before taking new work. Final counters and record identities account for all valid lines. Any repeated deliveries are confined to the uncommitted batch and use the expected stable identities. |
| Delivery and checkpoint failures | A transient broker interruption recovers within the configured retry budget. Exhausted retries surface an error and leave the failed batch uncommitted. A failed PostgreSQL checkpoint leaves the previous committed state recoverable. |
| Status API and retention | Missing or invalid keys are rejected. Authenticated responses reflect persisted reading and done states. Expired completed files are removed from MinIO and PostgreSQL in the correct order, active files are preserved, and failed deletion is retried. |
| Compose smoke | `docker compose config --services` lists Reader and PostgreSQL only. With isolated external test endpoints, `docker compose up --build -d` starts Reader in agent mode, applies migrations, and processes a stub assignment whose completion is visible through authenticated `POST /status`. Capture observations and tear down the isolated test project. |

## Merge checks

- Run `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` using the repository's Go version and a race-capable host.
- Run `go test -tags=integration -count=1 -v ./test/...` with Docker and `READER_TEST_COMPOSE=1`. The suites provision isolated Testcontainers services by default. Confirm the workflow, broker recovery, Status API, PostgreSQL and MinIO retention, and Compose subtest actually ran. An unavailable Docker provider or an image-pull failure leaves the gate open.
- Reuse the [Phase 3 protocol and numeric gates](../2026-11-19-large-scale-performance-hardening/validation.md). Check the 2 and 3 GiB distinct-URL cases at batch size 500 for expected counts, the 96 MiB per-run peak ceiling, the 24 MiB scaling limit, and the 155 MiB/s median throughput floor on a comparable host. Check the seeded-regex near-miss series against the documented timing gates. Record the host, fixture, commands, and results. Extend coverage to affected cases if a fix changes an unmeasured Phase 3 workload.
- Attach the §17 coverage map, executed check results, benchmark measurements, and Compose smoke evidence to the implementation PR. Merge after the required checks pass and documentation reproduces the verified procedure.

## README §17 coverage

| Checklist | Checks |
| --- | --- |
| §17.1 Regex selection, extraction, validation, normalization, domain parts, and hashing | Unit suites in `internal/processing/regexselect`, `extract`, `validate`, `normalize`, and `record`. The tagged workflow checks migration-seeded captures, normalized output, hashes, and MinIO metadata in the message. |
| §17.1 Batch calculations, deduplication, priority, and progress | Unit suites in `internal/processing/batch`, `uniqueurl`, and `pipeline`. The resume regression restores committed URL membership and the performance fixture checks expected counts and bounded memory. |
| §17.1 Retention eligibility and period | `TestPostgreSQLRetention` exercises the production SQL around month boundaries, leap years, and UTC cutoffs. |
| §17.2 PostgreSQL, MinIO, RabbitMQ, Status API, and Agent | `TestMVP_EndToEnd_HappyPathAndCrashResume`, its delivery subtest, and `TestStatusAPI_PostgreSQL`. The workflow observes startup migrations, assignment, download, saved state, both broker payloads, priority, authentication, and Agent idle and busy responses. |
| §17.3 Batch recovery, single-file ownership, no match, counters, and local cleanup | The workflow denies a competing Reader the polling lock, exhausts broker retries before the first checkpoint, forces a later PostgreSQL checkpoint failure, and restarts the process with the working copy. Unit recovery coverage includes a restart with a changed batch size and suppression of already committed URLs. |
| §17.3 Active-object protection and retryable deletion | `TestPostgreSQLRetention` and `TestMinIORetention` cover the ledger and real object lifecycle under processing contention and deletion failure. |
| §17.4 Isolation and reproducibility | Testcontainers by default, documented dedicated-service overrides, a synthetic Agent stub, and a uniquely named Compose smoke project. `make test-integration` runs all tagged suites. |

## Validation results on 2026-11-19

- Build, vet, unit tests, race tests, and tagged vet and compilation passed with Go 1.26.11 on Windows amd64. The race run used LLVM-MinGW with CGO enabled.
- The tagged suites passed using disposable local PostgreSQL 18, RabbitMQ 4.4.1, and MinIO instances with the documented endpoint overrides and HTTP Agent stub. The default container-backed run could not initialize a Docker provider on this Windows host. CI is configured to run the Docker-backed suites and the Compose smoke. That gate is open.
- Live Agent interoperability is tracked separately. In particular, the Agent's handling of a crash between assignment receipt and the first durable Reader registration still requires verification against its real assignment contract.

### Targeted Phase 3 regression

Ran the Phase 3 synthetic distinct-URL workload with batch size 500 and three fresh processes per fixture. Used Go 1.26.11, Windows amd64, AMD EPYC-Rome, `GOMAXPROCS=4`, and `GOGC=100`. Fixture generation was outside the timed runs. All output counters passed.

| Fixture | Lines | Throughput across runs | Median | Largest peak working set |
| --- | ---: | --- | ---: | ---: |
| 2 GiB | 482,202 | 105.62, 209.69, 214.76 MiB/s | 209.69 MiB/s | 38.60 MiB |
| 3 GiB | 723,304 | 182.47, 217.51, 198.91 MiB/s | 198.91 MiB/s | 38.46 MiB |

The absolute peak difference was 0.13 MiB. The seeded-regex near-miss medians at 16 KiB, 256 KiB, and 1 MiB minus one byte were 2.97 ms, 40.43 ms, and 172.62 ms. The growth ratios were 13.61× and 4.27×. Both memory and throughput gates and all targeted regex timing gates passed.

Use the [Phase 3 benchmark protocol](../2026-11-19-large-scale-performance-hardening/validation.md) for fixture generation and process working-set sampling. The focused run used `READER_BENCH_DISTINCT=1`, `READER_BENCH_BATCH=500`, and `-test.run=^TestPipelinePerformance$`, followed by `-test.run=^$ -test.bench=^BenchmarkSeededRegex$/./^near-miss$ -test.benchtime=100ms -test.count=3` on the compiled pipeline test binary.
