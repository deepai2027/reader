# Reader Status API Validation

## HTTP contract

- With the configured key, submit several IDs in one request and verify HTTP 200, JSON content type, the files envelope, and the four documented fields with the correct types and values.
- Verify missing and incorrect keys return HTTP 401 and that neither request reaches the lookup. Inspect request logs for credential leakage.
- Verify the agreed behavior for unknown and duplicate IDs, input ordering, malformed JSON, empty input, request limits, and unsupported methods.
- Simulate a lookup failure and verify a server error without SQL, connection details, or credentials in the response.

## PostgreSQL and runtime

- Apply the existing migrations to dedicated test PostgreSQL. Seed reading and done rows, including zero counts, and verify that the HTTP response matches the persisted rows.
- Commit a checkpoint update and verify a subsequent request reports it. Verify that a status request leaves persisted rows unchanged.
- Start Reader with the documented configuration, verify the Agent can reach POST /status, and verify shutdown cleans up the HTTP listener and poll loop.

## Merge checks

- Run go build ./..., go vet ./..., go test ./..., and the tagged PostgreSQL integration test.
- Set READER_STATUS_TEST_DATABASE_DSN to a dedicated empty PostgreSQL database and run `go test -tags=integration ./test/statusintegration -run '^TestStatusAPI_PostgreSQL$' -count=1`. The suite applies the existing migrations and seeds its test rows.
- Confirm the request contract with the Agent team and run the startup and shutdown smoke check in the deployment environment. Full Agent-to-RabbitMQ workflow verification belongs to Step G.

## Results

- `go build ./...`, `go vet ./...`, and `go test ./...` passed on Go 1.26.11.
- The tagged Status API test passed against a disposable local PostgreSQL 18 instance. It verified the HTTP response before and after a committed checkpoint update and checked that the lookup did not change stored progress.
- Live Agent interoperability and normal agent-mode startup and shutdown with Docker Compose remain unverified.
