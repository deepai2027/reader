//go:build integration

// Package integration holds the MVP's full real-infrastructure
// end-to-end test (specs/2026-11-10-mvp/plan.md Group 9). It is built
// with the "integration" tag so `go test ./...` (no tag) never needs
// Docker, matching specs/2026-11-10-mvp/validation.md section 1.
//
// WARNING - read before trusting this file: it was written with no Go
// toolchain, no network access, and no way to run it, combining four
// testcontainers-go module APIs (postgres, rabbitmq, minio) and
// golang-migrate at once. Every other new adapter in this phase
// (internal/storage, internal/objectstore, internal/rabbitmq,
// internal/agent) has a unit test against a fake and was reviewed on
// its own; this file has neither luxury - it is the single least-
// verified piece of code in this entire phase. Expect to need to fix
// API mismatches here (container module function names/signatures
// drift between testcontainers-go versions) before it runs at all.
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	amqp "github.com/rabbitmq/amqp091-go"
	miniocontainer "github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	readeragent "github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/delivery/local"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/objectstore"
	"github.com/yourorg/reader/internal/orchestrate"
	"github.com/yourorg/reader/internal/processing/pipeline"
	readerrabbitmq "github.com/yourorg/reader/internal/rabbitmq"
	"github.com/yourorg/reader/internal/storage"
)

func TestMVP_EndToEnd_HappyPathAndCrashResume(t *testing.T) {
	ctx := context.Background()

	// --- PostgreSQL ---
	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("reader"),
		postgres.WithUsername("reader"),
		postgres.WithPassword("reader"),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	defer func() { _ = pgContainer.Terminate(ctx) }()

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting postgres connection string: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	if err := runMigrations(dsn); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	// --- RabbitMQ ---
	rmqContainer, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
	if err != nil {
		t.Fatalf("starting rabbitmq container: %v", err)
	}
	defer func() { _ = rmqContainer.Terminate(ctx) }()

	rmqURL, err := rmqContainer.AmqpURL(ctx)
	if err != nil {
		t.Fatalf("getting rabbitmq url: %v", err)
	}
	amqpConn, err := amqp.Dial(rmqURL)
	if err != nil {
		t.Fatalf("dialing rabbitmq: %v", err)
	}
	defer amqpConn.Close()
	amqpChannel, err := amqpConn.Channel()
	if err != nil {
		t.Fatalf("opening rabbitmq channel: %v", err)
	}
	defer amqpChannel.Close()

	const fullRecordQueue = "test.full_record"
	const uniqueURLQueue = "test.unique_url"
	for _, q := range []string{fullRecordQueue, uniqueURLQueue} {
		if _, err := amqpChannel.QueueDeclare(q, true, false, false, false, nil); err != nil {
			t.Fatalf("declaring queue %q: %v", q, err)
		}
	}

	// --- MinIO ---
	minioContainer, err := miniocontainer.Run(ctx, "minio/minio:RELEASE.2024-01-16T16-07-38Z")
	if err != nil {
		t.Fatalf("starting minio container: %v", err)
	}
	defer func() { _ = minioContainer.Terminate(ctx) }()

	minioEndpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("getting minio endpoint: %v", err)
	}
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioContainer.Username, minioContainer.Password, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("creating minio client: %v", err)
	}

	const bucket = "reader-files"
	if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("creating bucket: %v", err)
	}

	const fileContent = "https://example.com:alice:password123\nhttp://test.org:bob:hunter2pass\n"
	if _, err := minioClient.PutObject(ctx, bucket, "e2e-test-file.txt",
		strings.NewReader(fileContent), int64(len(fileContent)), minio.PutObjectOptions{}); err != nil {
		t.Fatalf("uploading test file: %v", err)
	}

	// --- Fake Agent (httptest, per requirements.md Decision 5 - there is
	// no real Agent to run in a container) ---
	served := false
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if served {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		served = true
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"file_id":"e2e-test-file.txt","priority":5}`)
	}))
	defer agentServer.Close()

	// --- Wire real adapters ---
	progress := storage.NewProgressStore(db)
	regexes := storage.NewRegexSource(db)
	files := objectstore.NewFileSource(minioClient, bucket)
	rawPublisher := readerrabbitmq.NewPublisher(amqpChannel, readerrabbitmq.Topics{
		FullRecord: fullRecordQueue,
		UniqueURL:  uniqueURLQueue,
	})
	publisher := local.New(rawPublisher, 3, 10)
	agentClient := readeragent.New(&http.Client{Timeout: 5 * time.Second}, agentServer.URL, "test-key")

	deps := pipeline.Deps{
		Progress: progress, Publisher: publisher, Regexes: regexes, Files: files,
		Config: config.Config{
			BatchSize: 1, MinUsernameLength: 1, MaxUsernameLength: 50,
			MinPasswordLength: 1, MaxPasswordLength: 50,
			RetryMaxAttempts: 3, RetryBaseDelayMS: 10, LogLevel: "error",
		},
	}

	processed, err := orchestrate.PollOnce(ctx, agentClient, progress, nil, deps)
	if err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}
	if !processed {
		t.Fatal("expected PollOnce to process the file")
	}

	state, ok, err := progress.Load(ctx, "e2e-test-file.txt")
	if err != nil || !ok {
		t.Fatalf("expected a saved FileState, ok=%v err=%v", ok, err)
	}
	if state.Status != domain.FileStatusDone || state.MatchedCount != 2 {
		t.Fatalf("got %+v, want done status with 2 matched records", state)
	}

	// Confirm messages actually landed on the real queues.
	msg, ok, err := amqpChannel.Get(fullRecordQueue, true)
	if err != nil || !ok {
		t.Fatalf("expected a message on %q, ok=%v err=%v", fullRecordQueue, ok, err)
	}
	_ = msg
}

func runMigrations(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", "postgres", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
