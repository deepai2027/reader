// Package orchestrate ties the Agent client, ports.ProgressStore, and
// the core pipeline together into Reader's single-file polling loop.
// See specs/2026-11-10-mvp/plan.md Group 7.
package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/ports"
	"github.com/yourorg/reader/internal/processing/pipeline"
)

// PollOnce enforces single-file processing (README's concurrency
// requirement): if a file is already `reading`, it does nothing and
// returns (false, nil) without even polling the Agent. Otherwise it
// polls once; if nothing is available, or the Agent reports 409 (not
// expected right after confirming no file is reading, but handled
// identically either way per README's contract), it returns (false,
// nil). If a file is available, it runs the full pipeline against it
// and returns (true, err).
//
// NOTE: this assumes the Agent's file_id doubles as the object
// reference FileSource.Open expects (e.g. the MinIO object key) -
// another instance of the "built from documentation, not a real
// Agent" assumption flagged in requirements.md. Confirm this against
// the real Agent contract.
func PollOnce(ctx context.Context, agentClient *agent.Client, progress ports.ProgressStore, logger *slog.Logger, deps pipeline.Deps) (bool, error) {
	_, reading, err := progress.FindReading(ctx)
	if err != nil {
		return false, fmt.Errorf("orchestrate: checking for an in-progress file: %w", err)
	}
	if reading {
		return false, nil
	}

	file, err := agentClient.Poll(ctx)
	if errors.Is(err, agent.ErrNoFileAvailable) || errors.Is(err, agent.ErrFileInReading) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("orchestrate: polling agent: %w", err)
	}

	if logger != nil {
		logger.Info("received file from agent", "file_id", file.FileID, "priority", file.Priority)
	}

	if _, err := pipeline.Run(ctx, file.FileID, file.FileID, file.Priority, deps); err != nil {
		return true, fmt.Errorf("orchestrate: processing %q: %w", file.FileID, err)
	}
	return true, nil
}
