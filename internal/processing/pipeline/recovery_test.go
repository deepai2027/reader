package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports/stub"
)

// errSimulatedCrash stands in for "the process died here" - a batch
// that returns this is expected to be reprocessed correctly by a later
// Run() call against the same ProgressStore, exactly like any other
// unrecovered publish failure.
var errSimulatedCrash = errors.New("simulated crash")

// crashAfterNPublisher publishes successfully for the first n
// PublishRecord calls, then fails every call after that.
type crashAfterNPublisher struct {
	mu        sync.Mutex
	n         int
	published int
	records   []domain.ValidatedRecord
	unique    []domain.UniqueURLMessage
}

func (p *crashAfterNPublisher) PublishRecord(_ context.Context, r domain.ValidatedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.published >= p.n {
		return errSimulatedCrash
	}
	p.published++
	p.records = append(p.records, r)
	return nil
}

func (p *crashAfterNPublisher) PublishUniqueURL(_ context.Context, m domain.UniqueURLMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unique = append(p.unique, m)
	return nil
}

func recoveryTestDeps(publisher interface {
	PublishRecord(context.Context, domain.ValidatedRecord) error
	PublishUniqueURL(context.Context, domain.UniqueURLMessage) error
}, progress *stub.ProgressStore) Deps {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Deps{
		Progress:  progress,
		Publisher: publisher,
		Regexes:   stub.NewRegexSource(),
		Files:     stub.NewFileSource(),
		Config: config.Config{
			BatchSize:         1, // one line per batch: each checkpoint independently observable
			MinUsernameLength: 1,
			MaxUsernameLength: 50,
			MinPasswordLength: 1,
			MaxPasswordLength: 50,
			RetryMaxAttempts:  1,
			RetryBaseDelayMS:  1,
			LogLevel:          "error",
		},
		Logger: logger,
	}
}

func TestRun_CrashMidBatchThenResume_NoReprocessingNoSkip(t *testing.T) {
	progress := stub.NewProgressStore()
	crasher := &crashAfterNPublisher{n: 2} // crash publishing the 3rd record

	deps := recoveryTestDeps(crasher, progress)
	_, err := Run(context.Background(), "file-crash", "../testdata/well_formed.txt", 7, deps)
	if err == nil {
		t.Fatal("expected the simulated crash to surface as an error")
	}
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("got error %v, want it to wrap errSimulatedCrash", err)
	}

	stateAfterCrash, ok, loadErr := progress.Load(context.Background(), "file-crash")
	if loadErr != nil || !ok {
		t.Fatalf("expected a saved FileState after the crash, ok=%v err=%v", ok, loadErr)
	}
	if stateAfterCrash.Status != domain.FileStatusReading {
		t.Fatalf("Status after crash = %q, want %q", stateAfterCrash.Status, domain.FileStatusReading)
	}
	if stateAfterCrash.CurrentLine != 2 {
		t.Fatalf("CurrentLine after crash = %d, want 2 (two lines checkpointed before the crash)", stateAfterCrash.CurrentLine)
	}

	// "Restart": same ProgressStore (state survives), a fresh working
	// publisher, same file.
	finisher := &capturingPublisher{}
	deps2 := recoveryTestDeps(finisher, progress)
	stats, err := Run(context.Background(), "file-crash", "../testdata/well_formed.txt", 7, deps2)
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	if !stats.Resumed {
		t.Error("expected Stats.Resumed = true")
	}

	// well_formed.txt has 3 lines; 2 published before the crash, so
	// exactly 1 more on resume - never a 4th, never a repeat of the
	// first 2.
	if len(finisher.records) != 1 {
		t.Fatalf("published %d records on resume, want exactly 1", len(finisher.records))
	}

	finalState, ok, err := progress.Load(context.Background(), "file-crash")
	if err != nil || !ok {
		t.Fatalf("expected a final saved FileState, ok=%v err=%v", ok, err)
	}
	if finalState.Status != domain.FileStatusDone {
		t.Errorf("final Status = %q, want %q", finalState.Status, domain.FileStatusDone)
	}
	if finalState.MatchedCount != 3 {
		t.Errorf("final MatchedCount = %d, want 3 (2 before crash + 1 after resume)", finalState.MatchedCount)
	}
	if finalState.CurrentLine != 3 {
		t.Errorf("final CurrentLine = %d, want 3", finalState.CurrentLine)
	}
}

func TestRun_ResumeReusesSelectedRegex(t *testing.T) {
	progress := stub.NewProgressStore()
	crasher := &crashAfterNPublisher{n: 1}
	deps := recoveryTestDeps(crasher, progress)
	_, _ = Run(context.Background(), "file-regex", "../testdata/well_formed.txt", 1, deps)

	stateAfterCrash, ok, _ := progress.Load(context.Background(), "file-regex")
	if !ok || stateAfterCrash.SelectedRegexID == nil {
		t.Fatal("expected SelectedRegexID to be set after the crash")
	}
	idBeforeResume := *stateAfterCrash.SelectedRegexID

	finisher := &capturingPublisher{}
	deps2 := recoveryTestDeps(finisher, progress)
	stats, err := Run(context.Background(), "file-regex", "../testdata/well_formed.txt", 1, deps2)
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	if stats.SelectedPattern == nil || stats.SelectedPattern.ID != idBeforeResume {
		t.Errorf("resume selected a different pattern: got %+v, want ID %d", stats.SelectedPattern, idBeforeResume)
	}
}

func TestRun_PriorityPreservedAcrossRestart(t *testing.T) {
	progress := stub.NewProgressStore()
	crasher := &crashAfterNPublisher{n: 1}
	deps := recoveryTestDeps(crasher, progress)
	_, _ = Run(context.Background(), "file-priority", "../testdata/well_formed.txt", 42, deps)

	finisher := &capturingPublisher{}
	deps2 := recoveryTestDeps(finisher, progress)
	_, err := Run(context.Background(), "file-priority", "../testdata/well_formed.txt", 42, deps2)
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}

	finalState, ok, _ := progress.Load(context.Background(), "file-priority")
	if !ok {
		t.Fatal("expected a final saved FileState")
	}
	if finalState.Priority != 42 {
		t.Errorf("Priority after resume = %d, want 42", finalState.Priority)
	}
}

func TestRun_AlreadyDoneFileIsIdempotent(t *testing.T) {
	progress := stub.NewProgressStore()
	pub := &capturingPublisher{}
	deps := recoveryTestDeps(pub, progress)

	_, err := Run(context.Background(), "file-done", "../testdata/well_formed.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}
	firstCount := len(pub.records)

	stats, err := Run(context.Background(), "file-done", "../testdata/well_formed.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error re-running an already-done file: %v", err)
	}
	if stats.MatchedCount != 0 {
		t.Errorf("MatchedCount on a no-op re-run = %d, want 0", stats.MatchedCount)
	}
	if len(pub.records) != firstCount {
		t.Errorf("records published after re-running a done file = %d, want unchanged at %d", len(pub.records), firstCount)
	}
}

// TestRun_UniqueURLDedupNotPreservedAcrossRestart_KnownGap documents,
// rather than hides, a known MVP limitation (plan.md Group 2): Dedup
// state is in-memory and file-scoped, so a restart can re-emit a
// unique-URL message for a URL already emitted before the crash. If
// this test starts failing because dedup state became persisted,
// that's progress - update the spec to reflect the gap closing rather
// than "fixing" this test to hide it again.
func TestRun_UniqueURLDedupNotPreservedAcrossRestart_KnownGap(t *testing.T) {
	progress := stub.NewProgressStore()
	crasher := &crashAfterNPublisher{n: 1} // crash after line 1 ("dup.example.com") fully publishes
	deps := recoveryTestDeps(crasher, progress)
	_, _ = Run(context.Background(), "file-dup-gap", "../testdata/duplicate_urls.txt", 1, deps)

	finisher := &capturingPublisher{}
	deps2 := recoveryTestDeps(finisher, progress)
	_, err := Run(context.Background(), "file-dup-gap", "../testdata/duplicate_urls.txt", 1, deps2)
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}

	// duplicate_urls.txt's first two lines share the URL
	// https://dup.example.com. Line 1 published (and deduped as "new")
	// before the crash; resume continues at line 2, whose URL is the
	// same - but the resumed run's Dedup has never seen it, so it is
	// re-emitted as "new" again.
	foundDupAgain := false
	for _, m := range finisher.unique {
		if m.URL == "https://dup.example.com" {
			foundDupAgain = true
		}
	}
	if !foundDupAgain {
		t.Fatal("expected the known dedup-not-preserved gap to reproduce; if it no longer does, the gap may have been fixed - update the spec, not this test")
	}
}
