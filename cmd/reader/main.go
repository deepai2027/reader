// Command reader is Reader's entry point. In Phase 0 it only wires
// together configuration, structured logging, and the stub adapters -
// enough to prove the scaffolding compiles and runs end-to-end with
// zero external services. Phase 1+ replaces the stub wiring here with
// real pipeline logic built against internal/ports.
package main

import (
	"log/slog"
	"os"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/ports/stub"
)

func main() {
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

	// Phase 0 wiring: stub adapters only. Phase 1/2 replace these with
	// real local adapters; the Final Phase replaces them again with
	// real infrastructure adapters. Core pipeline code (not yet
	// written) will depend only on the internal/ports interfaces these
	// satisfy, never on this package directly.
	_ = stub.NewProgressStore()
	_ = stub.NewMessagePublisher(logger)
	_ = stub.NewRegexSource()
	_ = stub.NewFileSource()

	logger.Info("reader scaffolding ready; no pipeline logic yet (see specs/roadmap.md Phase 1)")
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
