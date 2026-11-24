package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/delivery/local"
	"github.com/yourorg/reader/internal/logrotation"
	"github.com/yourorg/reader/internal/objectstore"
	"github.com/yourorg/reader/internal/orchestrate"
	"github.com/yourorg/reader/internal/ports/stub"
	"github.com/yourorg/reader/internal/processing/pipeline"
	"github.com/yourorg/reader/internal/rabbitmq"
	"github.com/yourorg/reader/internal/statusapi"
	"github.com/yourorg/reader/internal/storage"
	"github.com/yourorg/reader/migrations"
)

func main() {
	mode := flag.String("mode", "manual",
		`"manual": process one local file given by -file, using the Phase 0 stub adapters (dev trigger only). `+
			`"agent": poll the real Agent and process against real infrastructure (requires LoadInfra's env vars).`)
	filePath := flag.String("file", "", "manual mode only: path to a local file to process")
	flag.Parse()
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		// A missing .env file is expected (e.g. in containers, where env
		// vars come from the orchestrator); only warn on other load errors.
		log.Println("failed to load .env file, using environment variables:", err)
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
		if err := runAgentMode(logger, cfg); err != nil {
			logger.Error("agent mode failed", "error", err)
			os.Exit(1)
		}
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

func runAgentMode(logger *slog.Logger, cfg config.Config) error {
	infra, err := config.LoadInfra()
	if err != nil {
		return fmt.Errorf("loading infrastructure configuration: %w", err)
	}
	if err := migrations.Up(infra.DatabaseDSN); err != nil {
		return fmt.Errorf("applying database migrations: %w", err)
	}

	db, err := sql.Open("pgx", infra.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("opening database connection: %w", err)
	}
	defer db.Close()

	minioClient, err := minio.New(infra.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(infra.MinIOAccessKey, infra.MinIOSecretKey, ""),
		Secure: infra.MinIOUseSSL,
	})
	if err != nil {
		return fmt.Errorf("creating MinIO client: %w", err)
	}

	topics := rabbitmq.Topics{
		FullRecord: infra.RabbitMQFullRecordTopic,
		UniqueURL:  infra.RabbitMQUniqueURLTopic,
	}
	amqpChannel := rabbitmq.NewProvisioningChannel(infra.RabbitMQURL, topics, infra.RabbitMQMaxPriority)
	defer amqpChannel.Close()

	progress := storage.NewProgressStore(db)
	rotation := storage.NewRotationStore(db)
	regexes := storage.NewRegexSource(db)
	workDir := infra.WorkDir
	if workDir == "" {
		workDir = filepath.Join(os.TempDir(), "reader-work")
	}
	files := objectstore.NewWorkingFileSource(minioClient, infra.MinIOBucket, workDir)

	rawPublisher := rabbitmq.NewPublisher(amqpChannel, topics)
	publisher := local.New(rawPublisher, cfg.RetryMaxAttempts, cfg.RetryBaseDelayMS)

	agentClient := agent.New(&http.Client{Timeout: 30 * time.Second}, infra.AgentBaseURL, infra.AgentAPIKey)

	deps := pipeline.Deps{
		LockProcessing: rotation.LockProcessing,
		Progress:       progress,
		Publisher:      publisher,
		Regexes:        regexes,
		Files:          files,
		Config:         cfg,
		Logger:         logger,
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	listener, err := net.Listen("tcp", infra.ReaderAPIAddr)
	if err != nil {
		return fmt.Errorf("binding Reader Status API: %w", err)
	}
	server := &http.Server{
		Handler:           statusapi.New(progress, infra.ReaderAPIKey, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
		cancel()
	}()
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Warn("status API shutdown timed out", "error", err)
			_ = server.Close()
		}
	}()
	logger.Info("status API listening", "address", listener.Addr().String())

	rotationDone := make(chan struct{})
	go func() {
		defer close(rotationDone)
		logrotation.Run(ctx, infra.LogRotationInterval, rotation, objectstore.ObjectRemover(minioClient, infra.MinIOBucket), logger)
	}()
	defer func() {
		cancel()
		<-rotationDone
	}()

	ticker := time.NewTicker(time.Duration(infra.AgentPollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	logger.Info("agent polling loop started", "poll_interval_seconds", infra.AgentPollIntervalSeconds)
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			select {
			case err := <-serverDone:
				if !errors.Is(err, http.ErrServerClosed) {
					return fmt.Errorf("serving Reader Status API: %w", err)
				}
			default:
			}
			return nil
		case <-ticker.C:
			if ctx.Err() != nil {
				continue
			}
			processed, err := func() (bool, error) {
				release, err := rotation.LockPolling(ctx)
				if err != nil {
					return false, err
				}
				if release == nil {
					logger.Debug("another Reader owns polling")
					return false, nil
				}
				defer release()
				return orchestrate.PollOnce(ctx, agentClient, progress, logger, deps)
			}()
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
