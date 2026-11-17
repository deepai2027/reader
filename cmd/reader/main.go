// Command reader is Reader's entry point. "-mode=manual" (default)
// processes one local file via the Phase 0/1 stub adapters - a dev
// trigger only. "-mode=agent" runs the real polling loop against real
// infrastructure (specs/2026-11-10-mvp/plan.md Groups 5-8).
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/delivery/local"
	"github.com/yourorg/reader/internal/objectstore"
	"github.com/yourorg/reader/internal/orchestrate"
	"github.com/yourorg/reader/internal/ports/stub"
	"github.com/yourorg/reader/internal/processing/pipeline"
	"github.com/yourorg/reader/internal/rabbitmq"
	"github.com/yourorg/reader/internal/storage"
)

func main() {
	mode := flag.String("mode", "manual",
		`"manual": process one local file given by -file, using the Phase 0 stub adapters (dev trigger only). `+
			`"agent": poll the real Agent and process against real infrastructure (requires LoadInfra's env vars).`)
	filePath := flag.String("file", "", "manual mode only: path to a local file to process")
	flag.Parse()
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
	cfg, err := config.Load()
	if err != nil {
		fallback := slog.New(slog.NewTextHandler(os.Stderr, nil))
		fallback.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("reader starting", "mode", *mode, "batch_size", cfg.BatchSize, "log_level", cfg.LogLevel)

	switch *mode {
	case "agent":
		runAgentMode(logger, cfg)
	default:
		runManualMode(logger, cfg, *filePath)
	}
}

func runManualMode(logger *slog.Logger, cfg config.Config, filePath string) {
	deps := pipeline.Deps{
		Progress:  stub.NewProgressStore(),
		Publisher: stub.NewMessagePublisher(logger),
		Regexes:   stub.NewRegexSource(),
		Files:     stub.NewFileSource(),
		Config:    cfg,
		Logger:    logger,
	}

	if filePath == "" {
		logger.Info("reader scaffolding ready; no file given " +
			"(-file PATH to run the pipeline manually, or -mode=agent for the real infra loop)")
		return
	}

	stats, err := pipeline.Run(context.Background(), filePath, filePath, 0, deps)
	if err != nil {
		logger.Error("pipeline run failed", "file", filePath, "error", err)
		os.Exit(1)
	}
	logger.Info("pipeline run complete",
		"file", filePath,
		"matched_count", stats.MatchedCount,
		"invalid_count", stats.InvalidCount,
		"unique_url_count", stats.UniqueURLCount,
	)
}

// runAgentMode wires every real infrastructure adapter added in the
// MVP phase and runs the single-file polling loop until interrupted.
//
// This is the least-verified code in this entire phase: it has never
// been compiled, let alone run against real PostgreSQL/MinIO/
// RabbitMQ/Agent instances, in the environment that wrote it (no Go
// toolchain, no network access there). The individual adapters behind
// it (internal/storage, internal/objectstore, internal/rabbitmq,
// internal/agent) each have unit tests against fakes; this function -
// the wiring itself - does not, and is correspondingly the highest-
// risk code to review before relying on it.
func runAgentMode(logger *slog.Logger, cfg config.Config) {
	infra, err := config.LoadInfra()
	if err != nil {
		logger.Error("failed to load infrastructure configuration", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", infra.DatabaseDSN)
	if err != nil {
		logger.Error("failed to open database connection", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	minioClient, err := minio.New(infra.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(infra.MinIOAccessKey, infra.MinIOSecretKey, ""),
		Secure: infra.MinIOUseSSL,
	})
	if err != nil {
		logger.Error("failed to create minio client", "error", err)
		os.Exit(1)
	}

	amqpConn, err := amqp.Dial(infra.RabbitMQURL)
	if err != nil {
		logger.Error("failed to connect to rabbitmq", "error", err)
		os.Exit(1)
	}
	defer amqpConn.Close()

	amqpChannel, err := amqpConn.Channel()
	if err != nil {
		logger.Error("failed to open rabbitmq channel", "error", err)
		os.Exit(1)
	}
	defer amqpChannel.Close()

	progress := storage.NewProgressStore(db)
	regexes := storage.NewRegexSource(db)
	files := objectstore.NewFileSource(minioClient, infra.MinIOBucket)

	rawPublisher := rabbitmq.NewPublisher(amqpChannel, rabbitmq.Topics{
		FullRecord: infra.RabbitMQFullRecordTopic,
		UniqueURL:  infra.RabbitMQUniqueURLTopic,
	})
	publisher := local.New(rawPublisher, cfg.RetryMaxAttempts, cfg.RetryBaseDelayMS)

	agentClient := agent.New(&http.Client{Timeout: 30 * time.Second}, infra.AgentBaseURL, infra.AgentAPIKey)

	deps := pipeline.Deps{
		Progress:  progress,
		Publisher: publisher,
		Regexes:   regexes,
		Files:     files,
		Config:    cfg,
		Logger:    logger,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(time.Duration(infra.AgentPollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	logger.Info("agent polling loop started", "poll_interval_seconds", infra.AgentPollIntervalSeconds)
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return
		case <-ticker.C:
			processed, err := orchestrate.PollOnce(ctx, agentClient, progress, logger, deps)
			if err != nil {
				logger.Error("poll cycle failed", "error", err)
				continue
			}
			if processed {
				logger.Info("poll cycle processed a file")
			}
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
