package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
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
	// LockProcessing optionally reserves the file before inspecting its state
	// or opening its source. A nil release means another worker owns it.
	LockProcessing func(context.Context, string) (release func(), err error)
	Progress       ports.ProgressStore
	Publisher      ports.MessagePublisher
	Regexes        ports.RegexSource
	Files          ports.FileSource
	Config         config.Config
	Logger         *slog.Logger
}

// Stats summarizes the outcome of one Run call.
type Stats struct {
	MatchedCount    int64
	InvalidCount    int64
	UniqueURLCount  int64
	Resumed         bool
	SelectedPattern *domain.RegexPattern
}

func Run(ctx context.Context, fileID, fileRef string, priority int, deps Deps) (Stats, error) {
	if deps.LockProcessing != nil {
		release, err := deps.LockProcessing(ctx, fileID)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: locking file %q: %w", fileID, err)
		}
		if release == nil {
			return Stats{}, nil
		}
		defer release()
	}
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

	var modifiedAt time.Time
	if file, ok := stream.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: reading metadata for %q: %w", fileID, err)
		}
		modifiedAt = info.ModTime().UTC()
	}
	if found && existing.Status == domain.FileStatusReading {
		if existing.SelectedRegexID == nil && existing.CurrentLine == 0 && existing.MatchedCount == 0 {
			return startFresh(ctx, fileID, existing.Priority, stream, existing.CreatedAt, modifiedAt, deps)
		}
		if deps.Logger != nil {
			deps.Logger.Info("resuming in-progress file",
				"file_id", fileID, "current_line", existing.CurrentLine)
		}
		return resume(ctx, fileID, existing, batch.New(stream, deps.Config.BatchSize), modifiedAt, deps)
	}
	return startFresh(ctx, fileID, priority, stream, time.Now().UTC(), modifiedAt, deps)
}

func startFresh(ctx context.Context, fileID string, priority int, stream io.Reader, createdAt, modifiedAt time.Time, deps Deps) (Stats, error) {
	patterns, err := deps.Regexes.ActivePatterns(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: loading active patterns: %w", err)
	}

	// A seekable source can replay the selection window directly. For a
	// streaming-only source, capture every byte read, including scanner read-ahead.
	input := stream
	replay := stream
	rewind, seekable := stream.(io.Seeker)
	var startOffset int64
	if seekable {
		startOffset, err = rewind.Seek(0, io.SeekCurrent)
		seekable = err == nil
	}
	if !seekable {
		spool, err := os.CreateTemp("", "reader-regex-window-*")
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: opening selection spool: %w", err)
		}
		defer func() {
			spool.Close()
			if err := os.Remove(spool.Name()); err != nil && deps.Logger != nil {
				deps.Logger.Warn("could not remove selection spool", "error", err)
			}
		}()
		input = io.TeeReader(stream, spool)
		replay = io.MultiReader(spool, stream)
		rewind = spool
		startOffset = 0
	}

	selector := regexselect.New(deps.Logger, patterns)
	sample := batch.New(input, deps.Config.BatchSize)
	for i := 0; i < regexselect.WindowSize; i++ {
		line, ok, err := sample.NextLine(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("pipeline: reading selection window: %w", err)
		}
		if !ok {
			break
		}
		selector.Observe(line)
	}
	if err := sample.Err(); err != nil {
		return Stats{}, fmt.Errorf("pipeline: reading selection window: %w", err)
	}
	selection := selector.Result()

	now := time.Now().UTC()

	if !selection.Matched {
		state := domain.FileState{
			FileID: fileID, Status: domain.FileStatusDone,
			CurrentLine: 0, MatchedCount: 0, Priority: priority,
			CreatedAt: createdAt, UpdatedAt: now,
		}
		if err := deps.Progress.Save(ctx, state); err != nil {
			return Stats{}, fmt.Errorf("pipeline: saving no-match file state: %w", err)
		}
		if deps.Logger != nil {
			deps.Logger.Info("no regex matched file; marking done", "file_id", fileID)
		}
		return Stats{}, nil
	}

	if _, err := rewind.Seek(startOffset, io.SeekStart); err != nil {
		return Stats{}, fmt.Errorf("pipeline: rewinding selection window: %w", err)
	}

	compiled, err := regexengine.Compile(selection.Pattern.Pattern)
	if err != nil {
		return Stats{}, fmt.Errorf("pipeline: recompiling selected pattern %d: %w", selection.Pattern.ID, err)
	}

	selectedID := selection.Pattern.ID

	initial := domain.FileState{
		FileID: fileID, Status: domain.FileStatusReading,
		CurrentLine: 0, MatchedCount: 0,
		SelectedRegexID: &selectedID, Priority: priority,
		CreatedAt: createdAt, UpdatedAt: now,
	}
	if err := deps.Progress.Save(ctx, initial); err != nil {
		return Stats{}, fmt.Errorf("pipeline: saving initial file state: %w", err)
	}

	rs := &runState{
		fileID: fileID, priority: priority, selectedID: selectedID,
		compiled: compiled, dedup: uniqueurl.New(priority),
		createdAt: createdAt, modifiedAt: modifiedAt, deps: deps,
	}

	selectedPattern := selection.Pattern
	stats, err := rs.drain(ctx, batch.New(replay, deps.Config.BatchSize))
	if err != nil {
		return Stats{}, err
	}
	stats.SelectedPattern = &selectedPattern
	return stats, nil
}

// resume continues a file whose FileState shows Status = reading.
func resume(ctx context.Context, fileID string, existing domain.FileState, reader *batch.Reader, modifiedAt time.Time, deps Deps) (Stats, error) {
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

	rs := &runState{
		fileID: fileID, priority: existing.Priority, selectedID: selected.ID,
		compiled: compiled, dedup: uniqueurl.New(existing.Priority),
		createdAt: existing.CreatedAt, modifiedAt: modifiedAt, deps: deps,
	}
	if err := rs.restoreURLIndex(ctx, reader, existing.CurrentLine); err != nil {
		_ = rs.dedup.Close()
		return Stats{}, err
	}
	rs.lastLine = existing.CurrentLine
	rs.stats.MatchedCount = existing.MatchedCount

	stats, err := rs.drain(ctx, reader)
	if err != nil {
		return Stats{}, err
	}
	stats.SelectedPattern = selected
	stats.Resumed = true
	return stats, nil
}

type runState struct {
	fileID     string
	priority   int
	selectedID int64
	compiled   *regexengine.Pattern
	dedup      *uniqueurl.Dedup
	createdAt  time.Time
	modifiedAt time.Time
	deps       Deps

	stats    Stats
	lastLine int64
}

func (r *runState) drain(ctx context.Context, reader *batch.Reader) (Stats, error) {
	defer func() {
		if err := r.dedup.Close(); err != nil && r.deps.Logger != nil {
			r.deps.Logger.Warn("could not remove URL index", "error", err)
		}
	}()

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

// restoreURLIndex rebuilds membership from the committed prefix without
// publishing records or changing counters.
func (r *runState) restoreURLIndex(ctx context.Context, reader *batch.Reader, committed int64) error {
	for index := int64(0); index < committed; index++ {
		text, ok, err := reader.NextLine(ctx)
		if err != nil {
			return fmt.Errorf("pipeline: restoring URL index: %w", err)
		}
		if !ok {
			return fmt.Errorf("pipeline: resume: file %q has fewer lines (%d) than its recorded current_line (%d)", r.fileID, index, committed)
		}
		_, normalized, valid := r.normalizedCandidate(domain.RawLine{LineNumber: index + 1, Text: text})
		if valid {
			if _, _, err := r.dedup.Offer(normalized.URL, normalized.Domain, normalized.Subdomain, normalized.Suffix); err != nil {
				return fmt.Errorf("pipeline: restoring URL index: %w", err)
			}
		}
	}
	return nil
}

func (r *runState) normalizedCandidate(line domain.RawLine) (domain.ExtractedCandidate, normalize.Normalized, bool) {
	candidate, ok := extract.Candidate(r.compiled, line)
	if !ok {
		return candidate, normalize.Normalized{}, false
	}

	result := validate.Candidate(r.deps.Config, candidate)
	if !result.Valid {
		if r.deps.Logger != nil {
			r.deps.Logger.Debug("line failed validation",
				"file_id", r.fileID, "line", line.LineNumber, "reason", result.Reason)
		}
		return candidate, normalize.Normalized{}, false
	}

	normalized, err := normalize.URL(candidate.URL)
	if err != nil {
		if r.deps.Logger != nil {
			r.deps.Logger.Debug("line failed normalization",
				"file_id", r.fileID, "line", line.LineNumber, "error", err)
		}
		return candidate, normalize.Normalized{}, false
	}
	return candidate, normalized, true
}

func (r *runState) processLine(ctx context.Context, line domain.RawLine) error {
	candidate, normalized, valid := r.normalizedCandidate(line)
	if !valid {
		r.stats.InvalidCount++
		return nil
	}
	rec := record.Assemble(
		candidate.Username, candidate.Password, normalized.URL,
		normalized.Domain, normalized.Subdomain, normalized.Suffix, normalized.Port,
		r.fileModifiedAt(), r.createdAt,
	)
	if err := r.deps.Publisher.PublishRecord(ctx, rec); err != nil {
		return fmt.Errorf("publishing record for line %d: %w", line.LineNumber, err)
	}
	r.stats.MatchedCount++

	msg, isNew, err := r.dedup.Offer(normalized.URL, normalized.Domain, normalized.Subdomain, normalized.Suffix)
	if err != nil {
		return fmt.Errorf("indexing unique url for line %d: %w", line.LineNumber, err)
	}
	if isNew {
		if err := r.deps.Publisher.PublishUniqueURL(ctx, msg); err != nil {
			return fmt.Errorf("publishing unique url for line %d: %w", line.LineNumber, err)
		}
		r.stats.UniqueURLCount++
	}
	return nil
}

func (r *runState) fileModifiedAt() time.Time {
	if r.modifiedAt.IsZero() {
		return r.createdAt
	}
	return r.modifiedAt
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
