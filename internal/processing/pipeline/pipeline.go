// Package pipeline orchestrates Reader's core file-processing run:
// open -> select regex -> batch -> extract -> validate -> normalize ->
// hash/assemble -> dedupe unique URLs -> publish -> record final
// state. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 8.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
	"github.com/yourorg/reader/internal/processing/batch"
	"github.com/yourorg/reader/internal/processing/extract"
	"github.com/yourorg/reader/internal/processing/normalize"
	"github.com/yourorg/reader/internal/processing/record"
	"github.com/yourorg/reader/internal/processing/regexengine"
	"github.com/yourorg/reader/internal/processing/regexselect"
	"github.com/yourorg/reader/internal/processing/uniqueurl"
	"github.com/yourorg/reader/internal/processing/validate"
)

// Deps are the pipeline's external dependencies, injected explicitly
// rather than as package-level globals, so Phase 2 can wrap or
// replace pieces of this without rewriting it, and so ProcessFile is
// trivially testable with fakes.
type Deps struct {
	Progress  ports.ProgressStore
	Publisher ports.MessagePublisher
	Regexes   ports.RegexSource
	Files     ports.FileSource
	Config    config.Config
	Logger    *slog.Logger
}

// Stats summarizes the outcome of one ProcessFile run.
type Stats struct {
	MatchedCount   int64
	InvalidCount   int64
	UniqueURLCount int64
	// SelectedPattern is nil if no active pattern matched the file.
	SelectedPattern *domain.RegexPattern
}

// ProcessFile runs Reader's full core pipeline against one file,
// start to finish, in a single call.
//
// ProcessFile does not checkpoint progress mid-run and does not
// resume a partial run - that's Phase 2's job. A failure partway
// through this call leaves no FileState behind (Phase 1 writes
// FileState only at completion); this is intentional. Phase 1 proves
// the pipeline produces *correct output*, not that it survives
// interruption - see
// specs/2026-11-09-core-file-processing-pipeline/validation.md,
// section 4.
func ProcessFile(ctx context.Context, fileID, fileRef string, priority int, deps Deps) (Stats, error) {
	stream, err := deps.Files.Open(ctx, fileRef)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: open %q: %w", fileRef, err)
	}
	defer stream.Close()

	reader := batch.New(stream, deps.Config.BatchSize)

	// Accumulate up to the selection window size, even across several
	// batches if BatchSize < WindowSize. Any lines read past the
	// window are held in `pending` and processed after selection,
	// rather than re-read.
	window := make([]domain.RawLine, 0, regexselect.WindowSize)
	var pending []domain.RawLine
	for len(window) < regexselect.WindowSize {
		b, done, err := reader.NextBatch(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: reading selection window: %w", err)
		}
		window = append(window, b...)
		if done {
			break
		}
	}
	if len(window) > regexselect.WindowSize {
		pending = append(pending, window[regexselect.WindowSize:]...)
		window = window[:regexselect.WindowSize]
	}

	patterns, err := deps.Regexes.ActivePatterns(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: loading active patterns: %w", err)
	}

	selection, err := regexselect.Select(ctx, deps.Logger, patterns, window)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: selecting regex: %w", err)
	}

	now := time.Now().UTC()

	if !selection.Matched {
		state := domain.FileState{
			FileID:       fileID,
			Status:       domain.FileStatusDone,
			CurrentLine:  0,
			MatchedCount: 0,
			Priority:     priority,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := deps.Progress.Save(ctx, state); err != nil {
			return Stats{}, fmt.Errorf("pipeline: saving no-match file state: %w", err)
		}
		if deps.Logger != nil {
			deps.Logger.Info("no regex matched file; marking done", "file_id", fileID)
		}
		return Stats{}, nil
	}

	compiled, err := regexengine.Compile(selection.Pattern.Pattern)
	if err != nil {
		// The pattern compiled successfully during selection (that's
		// how it won), so failing to recompile it here would indicate
		// an engine inconsistency, not bad input.
		return Stats{}, fmt.Errorf("pipeline: recompiling selected pattern %d: %w", selection.Pattern.ID, err)
	}

	dedup := uniqueurl.New(priority)
	stats := Stats{}
	selectedPattern := selection.Pattern
	stats.SelectedPattern = &selectedPattern

	processLine := func(line domain.RawLine) error {
		candidate, ok := extract.Candidate(compiled, line)
		if !ok {
			stats.InvalidCount++
			return nil
		}

		result := validate.Candidate(deps.Config, candidate)
		if !result.Valid {
			stats.InvalidCount++
			if deps.Logger != nil {
				deps.Logger.Debug("line failed validation",
					"file_id", fileID, "line", line.LineNumber, "reason", result.Reason)
			}
			return nil
		}

		normalized, err := normalize.URL(candidate.URL)
		if err != nil {
			stats.InvalidCount++
			if deps.Logger != nil {
				deps.Logger.Debug("line failed normalization",
					"file_id", fileID, "line", line.LineNumber, "error", err)
			}
			return nil
		}

		rec := record.Assemble(
			candidate.Username, candidate.Password, normalized.URL,
			normalized.Domain, normalized.Subdomain, normalized.Suffix, normalized.Port,
			now, now,
		)
		if err := deps.Publisher.PublishRecord(ctx, rec); err != nil {
			return fmt.Errorf("publishing record for line %d: %w", line.LineNumber, err)
		}
		stats.MatchedCount++

		if msg, isNew := dedup.Offer(normalized.URL, normalized.Domain, normalized.Subdomain, normalized.Suffix); isNew {
			if err := deps.Publisher.PublishUniqueURL(ctx, msg); err != nil {
				return fmt.Errorf("publishing unique url for line %d: %w", line.LineNumber, err)
			}
			stats.UniqueURLCount++
		}

		return nil
	}

	var lastLine int64
	for _, line := range window {
		if err := processLine(line); err != nil {
			return Stats{}, fmt.Errorf("pipeline: %w", err)
		}
		lastLine = line.LineNumber
	}
	for _, line := range pending {
		if err := processLine(line); err != nil {
			return Stats{}, fmt.Errorf("pipeline: %w", err)
		}
		lastLine = line.LineNumber
	}

	for {
		b, done, err := reader.NextBatch(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: reading remaining batches: %w", err)
		}
		for _, line := range b {
			if err := processLine(line); err != nil {
				return Stats{}, fmt.Errorf("pipeline: %w", err)
			}
			lastLine = line.LineNumber
		}
		if done {
			break
		}
	}

	selectedID := selection.Pattern.ID
	finalState := domain.FileState{
		FileID:          fileID,
		Status:          domain.FileStatusDone,
		CurrentLine:     lastLine,
		MatchedCount:    stats.MatchedCount,
		SelectedRegexID: &selectedID,
		Priority:        priority,
		CreatedAt:       now,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := deps.Progress.Save(ctx, finalState); err != nil {
		return Stats{}, fmt.Errorf("pipeline: saving final file state: %w", err)
	}

	return stats, nil
}
