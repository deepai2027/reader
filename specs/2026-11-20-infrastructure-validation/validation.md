# Infrastructure Validation

| Gate | Passing evidence |
| --- | --- |
| Docker-backed integration | On a Docker-capable host or CI, run `go test -tags=integration -count=1 -v ./test/...` with `READER_TEST_COMPOSE=1` and dedicated-service overrides unset. The workflow, broker recovery, Status API, and PostgreSQL and MinIO retention suites pass against Testcontainers. |
| Compose smoke | The Compose subtest executes and passes. The service list contains only Reader and PostgreSQL. Reader starts, applies migrations, and processes a stub assignment to completion, confirmed by authenticated status. |
| Staging assignment | The live Agent supplies the expected file ID and priority. Reader registers and completes the synthetic file with expected counters and broker output. Idle and busy responses leave the ledger unchanged. |
| Restart before registration | Evidence places the interruption after assignment receipt and before the first durable Reader row. The agreed staging recovery path makes the assignment available, and Reader completes it with the original priority and expected outputs. |
| Restart after a checkpoint | Evidence shows the last committed line before interruption. Reader resumes from the next line with the saved regex and priority before polling again. The final ledger and queue results account for all valid records. Any repeated delivery is confined to uncommitted work. |

Attach CI run references or local command output, the tested revision and environment versions, and a redacted staging record with assignment IDs, checkpoints, status responses, and message counts. Run `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` for Reader fixes, and rerun affected Docker and staging cases on the final revision.

Close the follow-up once the Docker-backed suites and Compose smoke pass and the staging evidence verifies assignment and both restart scenarios. An unavailable environment, skipped scenario, or unresolved Agent contract dependency leaves the corresponding gate open.

## Baseline evidence for 2026-11-20

Reader revision `1a2febf869a01bead505d92512702790a62032c0` was checked locally with Go 1.26.11 on Windows amd64. Build, vet, unit tests, and race tests passed. The tagged run with `READER_TEST_COMPOSE=1` stopped during Testcontainers provider initialization. This host has no usable Docker provider, and the Compose subtest was never reached locally.

GitHub Actions passed build, vet, unit, and race tests for the same revision. The integration step failed with exit code 2. The public annotation gives only the exit code, and the detailed log requires GitHub sign-in. The failing assertion and whether Compose was reached are unverified. [CI run](https://github.com/deepai2027/reader/actions/runs/44865460402), [integration job](https://github.com/deepai2027/reader/actions/runs/44865460402/job/136728006052).

## Staging evidence to collect

Use a separate synthetic assignment for each scenario. For retry and restart scenarios, use a file that spans multiple batches and has known expected valid-record and unique-URL counts. Confirm the Agent's documented reissue or reconciliation behavior and its timeout before running the interruption cases.

| Scenario | Capture before restart | Capture after restart |
| --- | --- | --- |
| Normal assignment | Agent HTTP status, synthetic file ID and priority, and a ledger snapshot before polling. Record the idle and busy responses with the before and after ledger snapshots. | Authenticated status, final ledger counters, and both RabbitMQ message counts. |
| Before registration | Successful response from the Agent and evidence that Reader has reached the first insert for the same synthetic ID. A fixture-specific write barrier in the isolated database can make the point deterministic. Verify from a separate connection that the ledger has no committed row after stopping Reader. | Remove the test barrier, restart Reader, and record the actual Agent reissue or reconciliation event. Capture the new ledger row, final status and counters, and queue results. |
| After a checkpoint | Record the file ID, `current_line`, `matched_count`, `selected_regex_id`, and `priority`. Use a fixture-specific barrier on a later checkpoint to keep the file in `reading` while stopping Reader. | Record the resumed line and the first subsequent Agent poll. Compare final status, regex, priority, record hashes, and queue counts with the fixture expectations. |

Staging status is pending an authorized Agent endpoint, fixture setup, and access to the isolated Reader ledger and queues.

## Validation attempt on 2026-11-21

| Check | Observed result |
| --- | --- |
| Local build, vet, and unit tests | `go build ./...`, `go vet ./...`, and `go test ./...` exited successfully with Go 1.26.11 on Windows amd64. The tested working tree was based on `1a2febf869a01bead505d92512702790a62032c0` with uncommitted Reader changes. |
| GitHub Actions integration and Compose | The detailed failed job log remained inaccessible from this session. The GitHub job URL was blocked in the in-app browser, and the API request could not connect. The first failure, rerun result, and Compose result remain unverified. [Failed integration job](https://github.com/deepai2027/reader/actions/runs/44865460402/job/136728006052). |
| Live Agent assignment and both restart cases | Pending confirmation of an isolated Agent assignment scope, synthetic fixtures, isolated Reader services, and the Agent's reissue or reconciliation procedure and timeout. No scenario checkpoints, final statuses, or RabbitMQ output were captured on this attempt. |
