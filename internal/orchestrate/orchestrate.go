package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
	"github.com/yourorg/reader/internal/processing/pipeline"
)

func PollOnce(ctx context.Context, agentClient *agent.Client, progress ports.ProgressStore, logger *slog.Logger, deps pipeline.Deps) (bool, error) {
	state, reading, err := progress.FindReading(ctx)
	if err != nil {
		return false, fmt.Errorf("orchestrate: checking for an in-progress file: %w", err)
	}
	if reading {
		if err := process(ctx, state.FileID, state.Priority, deps); err != nil {
			return true, fmt.Errorf("orchestrate: resuming %q: %w", state.FileID, err)
		}
		return true, nil
	}
	if files, ok := deps.Files.(interface{ PruneWorkingCopies() error }); ok {
		if err := files.PruneWorkingCopies(); err != nil {
			return false, fmt.Errorf("orchestrate: pruning working copies: %w", err)
		}
	}

	file, err := agentClient.Poll(ctx)
	if errors.Is(err, agent.ErrNoFileAvailable) || errors.Is(err, agent.ErrFileInReading) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("orchestrate: polling agent: %w", err)
	}

	state, found, err := progress.Load(ctx, file.FileID)
	if err != nil {
		return false, err
	}
	if found && state.Status == domain.FileStatusDone {
		return false, removeWorkingCopy(file.FileID, deps)
	}
	if !found {
		now := time.Now().UTC()
		if err := progress.Save(ctx, domain.FileState{
			FileID: file.FileID, Status: domain.FileStatusReading, Priority: file.Priority,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return false, err
		}
	}

	if logger != nil {
		logger.Info("received file from agent", "file_id", file.FileID, "priority", file.Priority)
	}

	if err := process(ctx, file.FileID, file.Priority, deps); err != nil {
		return true, fmt.Errorf("orchestrate: processing %q: %w", file.FileID, err)
	}
	return true, nil
}

func process(ctx context.Context, fileID string, priority int, deps pipeline.Deps) error {
	if _, err := pipeline.Run(ctx, fileID, fileID, priority, deps); err != nil {
		return err
	}
	state, found, err := deps.Progress.Load(ctx, fileID)
	if err != nil || !found || state.Status != domain.FileStatusDone {
		return err
	}
	return removeWorkingCopy(fileID, deps)
}

func removeWorkingCopy(fileID string, deps pipeline.Deps) error {
	if files, ok := deps.Files.(interface{ RemoveWorkingCopy(string) error }); ok {
		return files.RemoveWorkingCopy(fileID)
	}
	return nil
}
