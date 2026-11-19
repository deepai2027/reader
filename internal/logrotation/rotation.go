package logrotation

import (
	"context"
	"log/slog"
	"time"
)

const (
	batchSize     = 100
	runTimeout    = time.Minute
	deleteTimeout = 15 * time.Second
)

type Store interface {
	Candidates(context.Context, string, time.Time, int) ([]string, error)
	RemoveExpired(context.Context, string, time.Time, func(context.Context, string) error) (bool, error)
}

// Run executes one bounded batch per tick until ctx is canceled. The cursor
// wraps so repeatedly failing files cannot monopolize the candidate batch.
func Run(ctx context.Context, interval time.Duration, store Store, removeObject func(context.Context, string) error, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var after string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			runCtx, cancel := context.WithTimeout(ctx, runTimeout)
			after = sweep(runCtx, store, removeObject, logger, after, time.Now())
			cancel()
		}
	}
}

func sweep(ctx context.Context, store Store, removeObject func(context.Context, string) error, logger *slog.Logger, after string, now time.Time) string {
	ids, err := store.Candidates(ctx, after, now, batchSize)
	if err != nil {
		logger.Warn("log rotation candidate lookup failed")
		return after
	}

	var removed, failed, skipped int
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		attempt, cancel := context.WithTimeout(ctx, deleteTimeout)
		deleted, err := store.RemoveExpired(attempt, id, now, removeObject)
		cancel()
		after = id
		switch {
		case err != nil:
			failed++
			logger.Warn("log rotation could not remove file", "file_id", id)
		case deleted:
			removed++
		default:
			skipped++
		}
	}
	logger.Info("log rotation run completed", "removed", removed, "failed", failed, "skipped", skipped)
	if removed+failed+skipped == len(ids) && len(ids) < batchSize {
		return ""
	}
	return after
}
