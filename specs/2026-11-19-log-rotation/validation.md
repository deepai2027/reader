# Log Rotation Validation

## Automated checks

- Run `go build ./...`, `go vet ./...`, and `go test ./...`.
- Verify eligibility immediately before, at, and after expiry. Include January 31 in leap and non-leap years, a year boundary, and timestamps with different offsets representing the same instant.
- Run `go test -tags=integration ./test/rotationintegration -count=1 -v` against dedicated PostgreSQL and MinIO with synthetic fixtures. Set `READER_ROTATION_TEST_DATABASE_DSN` to a dedicated empty database. The suite applies the migrations. Set `READER_ROTATION_TEST_MINIO_ENDPOINT`, `READER_ROTATION_TEST_MINIO_ACCESS_KEY`, `READER_ROTATION_TEST_MINIO_SECRET_KEY`, and `READER_ROTATION_TEST_MINIO_BUCKET` to point to a dedicated empty bucket. Set `READER_ROTATION_TEST_MINIO_USE_SSL=true` for TLS. Verify both tests ran, since they skip when their service configuration is absent.
- Verify an expired eligible object is deleted before its row, while unexpired, reading, and resumable rows and objects survive. Race processing and cleanup claims in both orders and exercise competing cleanup workers.
- Inject a MinIO failure and verify the row survives and a later run succeeds. Inject failure after object deletion but before database finalization, then restart or retry and verify the missing object allows cleanup to finish. Verify ambiguous storage failures retain the row.

## Scheduled deployment smoke test

Deploy Reader against dedicated test PostgreSQL and MinIO with a short configured rotation interval. Seed an expired terminal file, a fresh terminal file, and an expired reading file, each with a synthetic object. Let the scheduler run without invoking cleanup manually. Verify from logs and both stores that only the expired terminal pair was removed. Observe a subsequent scheduled run and clean shutdown.

## Merge criteria

Record passing Go checks, dedicated integration results, and the scheduled deployment smoke result in this file before merging.

## Results

- `go build ./...`, `go vet ./...`, and `go test ./...` passed on Go 1.26.11.
- Both tagged tests passed against disposable PostgreSQL 18 and a locally built MinIO server pinned to `RELEASE.2025-04-22T22-12-26Z`. The suite checked UTC expiry boundaries, processing and cleanup contention, MinIO failure, database rollback after object removal, and retry against an absent object.
- The scheduled deployment smoke passed using the built Reader process in agent mode on Windows, with disposable PostgreSQL and MinIO and local RabbitMQ. At a `500ms` interval, the first run removed the expired terminal pair and the second removed zero pairs. Direct queries to both stores confirmed preservation of the fresh terminal and expired reading fixtures. Reader exited cleanly on Ctrl+Break. This run used a local binary deployment.
