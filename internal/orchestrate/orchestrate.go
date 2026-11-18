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
