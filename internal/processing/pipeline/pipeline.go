// Package pipeline orchestrates Reader's core file-processing run:
// resume-or-start -> select regex -> batch -> extract -> validate ->
// normalize -> hash/assemble -> dedupe unique URLs -> publish ->
// checkpoint after every batch. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md (original
// pipeline) and specs/2026-11-10-mvp/plan.md Groups 1-2 (checkpointing
// and recovery, added on top of it).
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
// rather than as package-level globals.
type Deps struct {
	Progress  ports.ProgressStore
	Publisher ports.MessagePublisher
	Regexes   ports.RegexSource
	Files     ports.FileSource
	Config    config.Config
	Logger    *slog.Logger
}

// Stats summarizes the outcome of one Run call.
type Stats struct {
	MatchedCount    int64
	InvalidCount    int64
	UniqueURLCount  int64
	Resumed         bool
	SelectedPattern *domain.RegexPattern
}

// Run processes fileID end-to-end. If ProgressStore already has an
// in-progress (reading) run for fileID, Run resumes it (MVP Group 2)
// rather than starting over; if ProgressStore shows the file already
// done, Run is a no-op (idempotent against a duplicate trigger).
// Otherwise it starts fresh, exactly as Phase 1 originally did.
//
// Progress is checkpointed after every batch (MVP Group 1), not just
// at completion, so a crash mid-file leaves a precise resume point
// rather than forcing a full restart.
//
// Known MVP limitation (see specs/2026-11-10-mvp/plan.md Group 2 and
// validation.md section 2): unique-URL dedup state is in-memory and
// scoped to a single Run call. A resumed run starts dedup over, so a
// URL already published to the Unique URL topic before a crash may be
// published again after resume. This is accepted and tested (see
// pipeline/recovery_test.go), not silently hidden.
//
// Known MVP limitation (batch-level, not line-level, atomicity): if a
// batch fails partway through (some lines published, then an error),
// its checkpoint is not written, so resume reprocesses the *whole*
// batch from its start - lines that had already published
// successfully within that failed batch may be published again.
// Downstream record_hash-based dedup is the safety net for this, by
// design (see specs/mission.md).
func Run(ctx context.Context, fileID, fileRef string, priority int, deps Deps) (Stats, error) {
	existing, found, err := deps.Progress.Load(ctx, fileID)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: loading existing state for %q: %w", fileID, err)
	}
	if found && existing.Status == domain.FileStatusDone {
		return Stats{}, nil // idempotent: already finished
	}

	stream, err := deps.Files.Open(ctx, fileRef)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: open %q: %w", fileRef, err)
	}
	defer stream.Close()

	reader := batch.New(stream, deps.Config.BatchSize)

	if found && existing.Status == domain.FileStatusReading {
		if deps.Logger != nil {
			deps.Logger.Info("resuming in-progress file",
				"file_id", fileID, "current_line", existing.CurrentLine)
		}
		return resume(ctx, fileID, priority, existing, reader, deps)
	}
	return startFresh(ctx, fileID, priority, reader, deps)
}

// startFresh runs the original Phase 1 flow: scan the selection
// window, choose a regex pattern, persist that choice before
// processing any line, then drain the rest of the file.
func startFresh(ctx context.Context, fileID string, priority int, reader *batch.Reader, deps Deps) (Stats, error) {
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
			FileID: fileID, Status: domain.FileStatusDone,
			CurrentLine: 0, MatchedCount: 0, Priority: priority,
			CreatedAt: now, UpdatedAt: now,
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
		return Stats{}, fmt.Errorf("pipeline: recompiling selected pattern %d: %w", selection.Pattern.ID, err)
	}

	selectedID := selection.Pattern.ID

	// Persist selection + reading status BEFORE processing any line
	// (MVP Group 1): a crash during the very first batch still has
	// something correct to resume from.
	initial := domain.FileState{
		FileID: fileID, Status: domain.FileStatusReading,
		CurrentLine: 0, MatchedCount: 0,
		SelectedRegexID: &selectedID, Priority: priority,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := deps.Progress.Save(ctx, initial); err != nil {
		return Stats{}, fmt.Errorf("pipeline: saving initial file state: %w", err)
	}

	rs := &runState{
		fileID: fileID, priority: priority, selectedID: selectedID,
		compiled: compiled, dedup: uniqueurl.New(priority),
		createdAt: now, deps: deps,
	}

	selectedPattern := selection.Pattern
	stats, err := rs.drain(ctx, append(window, pending...), reader)
	if err != nil {
		return Stats{}, err
	}
	stats.SelectedPattern = &selectedPattern
	return stats, nil
}

// resume continues a file whose FileState shows Status = reading.
func resume(ctx context.Context, fileID string, priority int, existing domain.FileState, reader *batch.Reader, deps Deps) (Stats, error) {
	if existing.SelectedRegexID == nil {
		return Stats{}, fmt.Errorf("pipeline: resume: file %q has status reading but no SelectedRegexID recorded", fileID)
	}

	patterns, err := deps.Regexes.ActivePatterns(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: resume: loading active patterns: %w", err)
	}
	var selected *domain.RegexPattern
	for i := range patterns {
		if patterns[i].ID == *existing.SelectedRegexID {
			selected = &patterns[i]
			break
		}
	}
	if selected == nil {
		return Stats{}, fmt.Errorf(
			"pipeline: resume: previously selected pattern %d not found among active patterns",
			*existing.SelectedRegexID)
	}

	compiled, err := regexengine.Compile(selected.Pattern)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: resume: recompiling pattern %d: %w", selected.ID, err)
	}

	// Discard lines already processed before the crash. This is a
	// read-and-discard approach, O(file size) rather than a true seek,
	// a deliberate simplification (specs/2026-11-10-mvp/plan.md
	// Group 2): correctness first, revisit for performance in Phase 3
	// if profiling shows it matters.
	var discarded int64
	for discarded < existing.CurrentLine {
		b, done, err := reader.NextBatch(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: resume: discarding already-processed lines: %w", err)
		}
		discarded += int64(len(b))
		if done {
			break
		}
	}
	if discarded < existing.CurrentLine {
		return Stats{}, fmt.Errorf(
			"pipeline: resume: file %q has fewer lines (%d) than its recorded current_line (%d); it may have changed since the crash",
			fileID, discarded, existing.CurrentLine)
	}

	rs := &runState{
		fileID: fileID, priority: priority, selectedID: selected.ID,
		compiled: compiled, dedup: uniqueurl.New(priority),
		createdAt: existing.CreatedAt, deps: deps,
	}
	rs.lastLine = existing.CurrentLine
	rs.stats.MatchedCount = existing.MatchedCount

	stats, err := rs.drain(ctx, nil, reader)
	if err != nil {
		return Stats{}, err
	}
	stats.SelectedPattern = selected
	stats.Resumed = true
	return stats, nil
}

// runState carries the per-run state shared across drain/processBatch/
// processLine/checkpoint, so none of it needs to be threaded through
// every call explicitly.
type runState struct {
	fileID     string
	priority   int
	selectedID int64
	compiled   *regexengine.Pattern
	dedup      *uniqueurl.Dedup
	createdAt  time.Time
	deps       Deps

	stats    Stats
	lastLine int64
}

// drain processes firstBatch (if non-empty - the already-buffered
// selection window on a fresh start, or nil on resume), checkpointing
// afterward, then continues reading and processing batches from reader
// until exhausted, checkpointing after every batch. It never holds
// more than one batch in memory at a time, preserving the
// memory-bounded guarantee from internal/processing/batch.
func (r *runState) drain(ctx context.Context, firstBatch []domain.RawLine, reader *batch.Reader) (Stats, error) {
	// firstBatch is the buffered regex-selection window on a fresh
	// start, which can be much larger than BatchSize. Split it into
	// real batches so checkpointing remains batch-granular and a crash
	// here resumes from the last completed batch, not the file start.
	if len(firstBatch) > 0 {
		batchSize := r.deps.Config.BatchSize
		if batchSize <= 0 {
			batchSize = len(firstBatch)
		}
		for start := 0; start < len(firstBatch); start += batchSize {
			end := start + batchSize
			if end > len(firstBatch) {
				end = len(firstBatch)
			}
			if err := r.processBatch(ctx, firstBatch[start:end]); err != nil {
				return Stats{}, err
			}
		}
	}

	for {
		b, done, err := reader.NextBatch(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: reading batch: %w", err)
		}
		if len(b) > 0 {
			if err := r.processBatch(ctx, b); err != nil {
				return Stats{}, err
			}
		}
		if done {
			break
		}
	}

	// A final explicit "done" checkpoint. This is occasionally a
	// second Save for the last batch (already checkpointed as
	// "reading" inside processBatch) - a deliberate, cheap
	// simplification rather than threading "is this definitely the
	// last batch" detection into the read loop above.
	if err := r.checkpoint(ctx, domain.FileStatusDone); err != nil {
		return Stats{}, fmt.Errorf("pipeline: final checkpoint: %w", err)
	}
	return r.stats, nil
}

// processBatch processes every line in one batch, then checkpoints.
// If any line's publish fails, it returns before checkpointing, so
// CurrentLine stays at the end of the *previous* batch - resume will
// reprocess this whole batch, never skip ahead past it.
func (r *runState) processBatch(ctx context.Context, lines []domain.RawLine) error {
	for _, line := range lines {
		if err := r.processLine(ctx, line); err != nil {
			return fmt.Errorf("pipeline: %w", err)
		}
		r.lastLine = line.LineNumber
	}
	if err := r.checkpoint(ctx, domain.FileStatusReading); err != nil {
		return fmt.Errorf("pipeline: checkpointing: %w", err)
	}
	return nil
}

func (r *runState) processLine(ctx context.Context, line domain.RawLine) error {
	candidate, ok := extract.Candidate(r.compiled, line)
	if !ok {
		r.stats.InvalidCount++
		return nil
	}

	result := validate.Candidate(r.deps.Config, candidate)
	if !result.Valid {
		r.stats.InvalidCount++
		if r.deps.Logger != nil {
			r.deps.Logger.Debug("line failed validation",
				"file_id", r.fileID, "line", line.LineNumber, "reason", result.Reason)
		}
		return nil
	}

	normalized, err := normalize.URL(candidate.URL)
	if err != nil {
		r.stats.InvalidCount++
		if r.deps.Logger != nil {
			r.deps.Logger.Debug("line failed normalization",
				"file_id", r.fileID, "line", line.LineNumber, "error", err)
		}
		return nil
	}

	rec := record.Assemble(
		candidate.Username, candidate.Password, normalized.URL,
		normalized.Domain, normalized.Subdomain, normalized.Suffix, normalized.Port,
		r.createdAt, r.createdAt,
	)
	if err := r.deps.Publisher.PublishRecord(ctx, rec); err != nil {
		return fmt.Errorf("publishing record for line %d: %w", line.LineNumber, err)
	}
	r.stats.MatchedCount++

	if msg, isNew := r.dedup.Offer(normalized.URL, normalized.Domain, normalized.Subdomain, normalized.Suffix); isNew {
		if err := r.deps.Publisher.PublishUniqueURL(ctx, msg); err != nil {
			return fmt.Errorf("publishing unique url for line %d: %w", line.LineNumber, err)
		}
		r.stats.UniqueURLCount++
	}
	return nil
}

// checkpoint persists current cumulative progress. status stays
// "reading" after every batch until the caller finalizes with
// FileStatusDone.
func (r *runState) checkpoint(ctx context.Context, status domain.FileStatus) error {
	state := domain.FileState{
		FileID:          r.fileID,
		Status:          status,
		CurrentLine:     r.lastLine,
		MatchedCount:    r.stats.MatchedCount,
		SelectedRegexID: &r.selectedID,
		Priority:        r.priority,
		CreatedAt:       r.createdAt,
		UpdatedAt:       time.Now().UTC(),
	}
	return r.deps.Progress.Save(ctx, state)
}
