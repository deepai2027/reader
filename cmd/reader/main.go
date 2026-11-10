// Command reader is Reader's entry point. It wires configuration,
// structured logging, the Phase 0 stub adapters, and the Phase 1 core
// pipeline together, with zero external services. The -file flag is a
// manual development trigger only; the real Agent-driven flow and real
// infrastructure adapters are introduced in the Final Phase.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/ports/stub"
	"github.com/yourorg/reader/internal/processing/pipeline"
)

func main() {
	// -file is a Phase 1 manual trigger only, for running the pipeline
	// by hand against a local file during development. It is NOT the
	// real Agent-driven flow - that's wired in the Final Phase, once
	// polling and a 409-aware client exist. Do not mistake this flag
	// for production behavior.
	filePath := flag.String("file", "", "path to a local file to process manually (Phase 1 dev trigger only)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		// The real logger depends on cfg.LogLevel and can't be built
		// yet, so a config error is reported directly to stderr with a
		// plain text handler and the process exits non-zero.
		fallback := slog.New(slog.NewTextHandler(os.Stderr, nil))
		fallback.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	logger.Info("reader starting",
		"batch_size", cfg.BatchSize,
		"log_level", cfg.LogLevel,
	)

	// Phase 0/1 wiring: stub adapters only. Phase 2 replaces these with
	// real local adapters; the Final Phase replaces them again with
	// real infrastructure adapters. The pipeline depends only on the
	// internal/ports interfaces these satisfy, never on this package.
	deps := pipeline.Deps{
		Progress:  stub.NewProgressStore(),
		Publisher: stub.NewMessagePublisher(logger),
		Regexes:   stub.NewRegexSource(),
		Files:     stub.NewFileSource(),
		Config:    cfg,
		Logger:    logger,
	}

	if *filePath == "" {
		logger.Info("reader scaffolding ready; no file given " +
			"(-file PATH to run the pipeline manually against a local file)")
		return
	}

	stats, err := pipeline.ProcessFile(context.Background(), *filePath, *filePath, 0, deps)
	if err != nil {
		logger.Error("pipeline run failed", "file", *filePath, "error", err)
		os.Exit(1)
	}

	logger.Info("pipeline run complete",
		"file", *filePath,
		"matched_count", stats.MatchedCount,
		"invalid_count", stats.InvalidCount,
		"unique_url_count", stats.UniqueURLCount,
	)
}

// newLogger builds a structured slog.Logger at the configured level,
// emitting JSON so it is friendly to log aggregation from the start.
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
