//go:build integration

package integration

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"github.com/yourorg/reader/internal/domain"
	readerRabbitMQ "github.com/yourorg/reader/internal/rabbitmq"
	"github.com/yourorg/reader/internal/storage"
	"github.com/yourorg/reader/test/integrationinfra"
)

func TestMVP_EndToEnd_HappyPathAndCrashResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	services := integrationEndpoints(t, ctx)
	dsn, rmqURL, minioEndpoint := services.dsn, services.broker, services.minio
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	amqpConn, err := amqp.Dial(rmqURL)
	if err != nil {
		t.Fatal(err)
	}
	defer amqpConn.Close()
	amqpChannel, err := amqpConn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer amqpChannel.Close()
	t.Run("delivery retry and confirmation", func(t *testing.T) {
		checkBrokerDelivery(t, ctx, rmqURL, amqpChannel)
	})
	t.Run("provisions missing output queues", func(t *testing.T) {
		topics := readerRabbitMQ.Topics{FullRecord: "test.provision.full_record", UniqueURL: "test.provision.unique_url"}
		stream := readerRabbitMQ.NewProvisioningChannel(rmqURL, topics, 10)
		defer stream.Close()
		publisher := readerRabbitMQ.NewPublisher(stream, topics)
		if err := publisher.PublishRecord(ctx, domain.ValidatedRecord{RecordHash: "provisioned"}); err != nil {
			t.Fatal(err)
		}
		if err := publisher.PublishUniqueURL(ctx, domain.UniqueURLMessage{URL: "https://example.com", Priority: 5}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{topics.FullRecord, topics.UniqueURL} {
			message, found, err := amqpChannel.Get(name, true)
			if err != nil || !found {
				t.Fatalf("expected a confirmed message in %q: %v", name, err)
			}
			if name == topics.UniqueURL && message.Priority != 5 {
				t.Fatalf("unique URL priority = %d, want 5", message.Priority)
			}
		}
	})

	const fullRecordQueue = "test.full_record"
	const uniqueURLQueue = "test.unique_url"
	declareQueues := func() {
		t.Helper()
		for _, queue := range []string{fullRecordQueue, uniqueURLQueue} {
			if _, err := amqpChannel.QueueDeclare(queue, true, false, false, false, amqp.Table{"x-max-priority": int32(10)}); err != nil {
				t.Fatalf("declaring queue %q: %v", queue, err)
			}
		}
	}

	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds: credentials.NewStaticV4(services.accessKey, services.secretKey, ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	const bucket = "reader-files"
	if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("creating bucket: %v", err)
	}

	const fileID = "e2e-test-file.txt"
	const noMatchID = "no-match.txt"
	const fileContent = "https://EXAMPLE.com:alice:password123\ninvalid line\nhttp://test.org:bob:hunter2pass\n"
	for id, content := range map[string]string{fileID: fileContent, noMatchID: "no matching records\n"} {
		if _, err := minioClient.PutObject(ctx, bucket, id, strings.NewReader(content), int64(len(content)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	var polls atomic.Int32
	allowNoMatch := make(chan struct{})
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/reader/new_file/" || r.Header.Get("X-API-Key") != "test-agent-key" {
			t.Error("invalid Agent poll")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch polls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusNoContent)
		case 2:
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"file_in_reading"}`)
		case 3:
			_, _ = io.WriteString(w, `{"file_id":"e2e-test-file.txt","priority":5}`)
		default:
			select {
			case <-allowNoMatch:
				_, _ = io.WriteString(w, `{"file_id":"no-match.txt","priority":7}`)
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		}
	}))
	defer agentServer.Close()

	workDir := t.TempDir()
	exe := buildReader(t)
	settings := map[string]string{
		"BATCH_SIZE": "1", "MIN_USERNAME_LENGTH": "1", "MAX_USERNAME_LENGTH": "50",
		"MIN_PASSWORD_LENGTH": "1", "MAX_PASSWORD_LENGTH": "50",
		"RETRY_MAX_ATTEMPTS": "3", "RETRY_BASE_DELAY_MS": "100", "LOG_LEVEL": "debug",
		"DATABASE_DSN": dsn, "MINIO_ENDPOINT": minioEndpoint, "MINIO_BUCKET": bucket,
		"MINIO_ACCESS_KEY": services.accessKey, "MINIO_SECRET_KEY": services.secretKey,
		"MINIO_USE_SSL": "false", "RABBITMQ_URL": rmqURL, "RABBITMQ_FULL_RECORD_TOPIC": fullRecordQueue,
		"RABBITMQ_UNIQUE_URL_TOPIC": uniqueURLQueue, "AGENT_BASE_URL": agentServer.URL,
		"AGENT_API_KEY": "test-agent-key", "AGENT_POLL_INTERVAL_SECONDS": "1",
		"READER_API_KEY": "test-status-key", "READER_API_ADDR": "127.0.0.1:0", "READER_WORK_DIR": workDir,
		"LOG_ROTATION_INTERVAL": "24h",
	}
	// A second Reader can serve HTTP while ownership is held, but cannot poll.
	release, err := storage.NewRotationStore(db).LockPolling(ctx)
	if err != nil || release == nil {
		t.Fatalf("reserving polling ownership: %v", err)
	}
	defer release()
	contender := startReader(t, exe, settings)
	eventually(t, func() bool {
		return contender.pollingContended.Load()
	})
	if got := polls.Load(); got != 0 {
		t.Fatalf("contending Reader called Agent %d times", got)
	}
	contender.stop(t)
	// Seed a checkpoint failure after the second valid line has already been
	// delivered. Restart must replay that batch and skip the committed lines.
	if _, err := db.ExecContext(ctx, `
		CREATE FUNCTION reject_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.current_line = 3 THEN
				RAISE EXCEPTION 'test checkpoint failure';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON file_management
			FOR EACH ROW EXECUTE FUNCTION reject_checkpoint();`); err != nil {
		t.Fatal(err)
	}

	release()
	first := startReader(t, exe, settings)
	checkStatus(t, first.address, "", http.StatusUnauthorized)
	checkStatus(t, first.address, "wrong", http.StatusUnauthorized)
	eventually(t, func() bool { return first.deliveryExhausted.Load() })
	status := checkStatus(t, first.address, "test-status-key", http.StatusOK)
	if len(status) != 1 || status[0] != (fileStatus{fileID, "reading", 0, 0}) {
		t.Fatalf("failed delivery advanced the checkpoint: %v", status)
	}
	declareQueues()
	eventually(t, func() bool {
		result := checkStatus(t, first.address, "test-status-key", http.StatusOK)
		queue, err := amqpChannel.QueueInspect(fullRecordQueue)
		if err != nil {
			t.Fatal(err)
		}
		return len(result) == 1 && result[0] == (fileStatus{fileID, "reading", 2, 1}) && queue.Messages >= 2
	})
	progress := storage.NewProgressStore(db)
	saved, _, err := progress.Load(ctx, fileID)
	if err != nil || saved.SelectedRegexID == nil || saved.Priority != 5 {
		t.Fatalf("checkpoint: %+v, %v", saved, err)
	}
	if entries, err := os.ReadDir(workDir); err != nil || len(entries) != 1 {
		t.Fatalf("retained working copy: %v, %v", entries, err)
	}
	first.stop(t)
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_checkpoint ON file_management"); err != nil {
		t.Fatal(err)
	}

	second := startReader(t, exe, settings)
	eventually(t, func() bool {
		result := checkStatus(t, second.address, "test-status-key", http.StatusOK)
		return len(result) == 1 && result[0] == (fileStatus{fileID, "done", 3, 2})
	})
	final, _, err := progress.Load(ctx, fileID)
	if err != nil || final.SelectedRegexID == nil || *final.SelectedRegexID != *saved.SelectedRegexID || final.Priority != saved.Priority {
		t.Fatalf("restart changed regex or priority: %+v, %v", final, err)
	}
	close(allowNoMatch)
	eventually(t, func() bool {
		result := checkStatus(t, second.address, "test-status-key", http.StatusOK)
		entries, err := os.ReadDir(workDir)
		if err != nil {
			t.Fatal(err)
		}
		return len(result) == 2 && result[1] == (fileStatus{noMatchID, "done", 0, 0}) && len(entries) == 0
	})
	var modifiedAt time.Time
	for _, id := range []string{fileID, noMatchID} {
		object, err := minioClient.StatObject(ctx, bucket, id, minio.StatObjectOptions{})
		if err != nil {
			t.Fatalf("source %s: %v", id, err)
		}
		if id == fileID {
			modifiedAt = object.LastModified
		}
	}
	assertMessages(t, amqpChannel, fullRecordQueue, uniqueURLQueue, modifiedAt)
	second.stop(t)
	if os.Getenv("READER_TEST_COMPOSE") == "1" {
		t.Run("Compose", func(t *testing.T) { checkCompose(t, settings) })
	}
}

type endpoints struct {
	dsn, broker, minio, accessKey, secretKey string
}

func integrationEndpoints(t *testing.T, ctx context.Context) endpoints {
	t.Helper()
	config := endpoints{
		dsn:       os.Getenv("READER_WORKFLOW_TEST_DATABASE_DSN"),
		broker:    os.Getenv("READER_WORKFLOW_TEST_RABBITMQ_URL"),
		minio:     os.Getenv("READER_WORKFLOW_TEST_MINIO_ENDPOINT"),
		accessKey: os.Getenv("READER_WORKFLOW_TEST_MINIO_ACCESS_KEY"),
		secretKey: os.Getenv("READER_WORKFLOW_TEST_MINIO_SECRET_KEY"),
	}
	if config != (endpoints{}) {
		if config.dsn == "" || config.broker == "" || config.minio == "" || config.accessKey == "" || config.secretKey == "" {
			t.Fatal("set all READER_WORKFLOW_TEST_* variables or use the default Testcontainers setup")
		}
		return config
	}

	config.dsn = integrationinfra.PostgreSQL(t)

	rmq, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine",
		rabbitmq.WithAdminUsername("reader-test"), rabbitmq.WithAdminPassword("reader-test-password"))
	if err != nil {
		t.Fatalf("starting RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = rmq.Terminate(context.Background()) })
	config.broker, err = rmq.AmqpURL(ctx)
	if err != nil {
		t.Fatal(err)
	}

	config.minio, config.accessKey, config.secretKey = integrationinfra.MinIO(t)
	return config
}
