module github.com/yourorg/reader

go 1.22

// NOTE on all requires below: hand-added in a sandbox with no network
// access, so go.sum was never generated and these versions are
// best-effort guesses, not verified against what's actually
// available. Run `go mod tidy` once, with network access, before
// building - it will fetch real modules, correct any wrong versions,
// and populate go.sum. This is the single most important setup step
// for this phase (specs/2026-11-10-mvp/plan.md).
require (
	golang.org/x/net v0.30.0
	github.com/jackc/pgx/v5 v5.7.1
	github.com/golang-migrate/migrate/v4 v4.18.1
	github.com/rabbitmq/amqp091-go v1.10.0
	github.com/minio/minio-go/v7 v7.0.75
)

// Test-only dependencies (specs/2026-11-10-mvp/plan.md Groups 5-9):
// ephemeral service containers for integration tests. These pull in a
// large transitive dependency tree (Docker client libraries etc.) that
// `go mod tidy` needs network access to resolve - expect this to need
// the most correction of anything in this file.
require (
	github.com/testcontainers/testcontainers-go v0.33.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.33.0
	github.com/testcontainers/testcontainers-go/modules/rabbitmq v0.33.0
	github.com/testcontainers/testcontainers-go/modules/minio v0.33.0
)
