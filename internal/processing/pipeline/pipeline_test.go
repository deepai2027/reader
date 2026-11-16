package pipeline

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports/stub"
)

// capturingPublisher is a test-only fake (NOT a stub-package
// placeholder - see internal/ports/stub's doc comment on why real
// logic doesn't belong there) that records every published
// record/message for assertions. It never fails, since Phase 1 does
// not yet test retry/failure behavior - that's Phase 2.
type capturingPublisher struct {
	mu      sync.Mutex
	records []domain.ValidatedRecord
	unique  []domain.UniqueURLMessage
}

func (p *capturingPublisher) PublishRecord(_ context.Context, r domain.ValidatedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, r)
	return nil
}

func (p *capturingPublisher) PublishUniqueURL(_ context.Context, m domain.UniqueURLMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unique = append(p.unique, m)
	return nil
}

func testDeps(t *testing.T, publisher *capturingPublisher) Deps {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Deps{
		Progress:  stub.NewProgressStore(),
		Publisher: publisher,
		Regexes:   stub.NewRegexSource(), // the single url:username:password test pattern
		Files:     stub.NewFileSource(),
		Config: config.Config{
			BatchSize:         2, // deliberately small, to exercise multi-batch logic on tiny fixtures
			MinUsernameLength: 2,
			MaxUsernameLength: 50,
			MinPasswordLength: 6,
			MaxPasswordLength: 50,
			RetryMaxAttempts:  1,
			RetryBaseDelayMS:  1,
			LogLevel:          "error",
		},
		Logger: logger,
	}
}

func TestProcessFile_WellFormed(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-1", "../testdata/well_formed.txt", 3, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 3 {
		t.Errorf("MatchedCount = %d, want 3", stats.MatchedCount)
	}
	if stats.InvalidCount != 0 {
		t.Errorf("InvalidCount = %d, want 0", stats.InvalidCount)
	}
	if len(pub.records) != 3 {
		t.Errorf("published %d records, want 3", len(pub.records))
	}

	state, ok, err := deps.Progress.Load(context.Background(), "file-1")
	if err != nil || !ok {
		t.Fatalf("expected a saved FileState, ok=%v err=%v", ok, err)
	}
	if state.Status != domain.FileStatusDone {
		t.Errorf("Status = %q, want %q", state.Status, domain.FileStatusDone)
	}
	if state.MatchedCount != 3 {
		t.Errorf("FileState.MatchedCount = %d, want 3", state.MatchedCount)
	}
	if state.SelectedRegexID == nil {
		t.Error("expected SelectedRegexID to be set")
	}
}

func TestProcessFile_MixedValidInvalid(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-2", "../testdata/mixed_valid_invalid.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 2 {
		t.Errorf("MatchedCount = %d, want 2", stats.MatchedCount)
	}
	if stats.InvalidCount != 2 {
		t.Errorf("InvalidCount = %d, want 2", stats.InvalidCount)
	}
}

func TestProcessFile_NoMatch(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-3", "../testdata/no_match.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 0 || stats.SelectedPattern != nil {
		t.Errorf("got %+v, want zero matches and no selected pattern", stats)
	}
	if len(pub.records) != 0 || len(pub.unique) != 0 {
		t.Error("expected nothing published when no pattern matches")
	}

	state, ok, err := deps.Progress.Load(context.Background(), "file-3")
	if err != nil || !ok {
		t.Fatalf("expected a saved FileState even on no-match, ok=%v err=%v", ok, err)
	}
	if state.Status != domain.FileStatusDone || state.MatchedCount != 0 {
		t.Errorf("got %+v, want done status with zero matches", state)
	}
	if state.SelectedRegexID != nil {
		t.Error("expected no SelectedRegexID recorded on a no-match file")
	}
}

func TestProcessFile_EmptyFile(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-4", "../testdata/empty.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 0 {
		t.Errorf("MatchedCount = %d, want 0", stats.MatchedCount)
	}
}

func TestProcessFile_DuplicateURLsDedup(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-5", "../testdata/duplicate_urls.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 3 {
		t.Errorf("MatchedCount = %d, want 3 (all three records are valid)", stats.MatchedCount)
	}
	if stats.UniqueURLCount != 2 {
		t.Errorf("UniqueURLCount = %d, want 2 (dup.example.com counted once)", stats.UniqueURLCount)
	}
	if len(pub.unique) != 2 {
		t.Errorf("published %d unique-url messages, want 2", len(pub.unique))
	}

	// Hard invariant: no credentials ever reach the unique-URL topic.
	// domain.UniqueURLMessage has no username/password field by
	// construction (internal/domain/types.go) - there is nothing to
	// assert at runtime today. If such a field is ever added to that
	// struct, this test's build will not fail on its own; a reviewer
	// adding that field must also add an explicit assertion here that
	// it's always empty (see validation.md's hard-check item on this).
}

func TestProcessFile_BlankLinesDoNotCrash(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-6", "../testdata/blank_lines.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 2 {
		t.Errorf("MatchedCount = %d, want 2 (blank lines dropped, not matched)", stats.MatchedCount)
	}
}

func TestProcessFile_NormalizationEdgeCases(t *testing.T) {
	pub := &capturingPublisher{}
	deps := testDeps(t, pub)

	stats, err := Run(context.Background(), "file-7", "../testdata/normalization_edge_cases.txt", 1, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MatchedCount != 4 {
		t.Fatalf("MatchedCount = %d, want 4", stats.MatchedCount)
	}
	if len(pub.records) != 4 {
		t.Fatalf("published %d records, want 4", len(pub.records))
	}

	byUsername := make(map[string]domain.ValidatedRecord, len(pub.records))
	for _, r := range pub.records {
		byUsername[r.Username] = r
	}

	// userB's line had an explicit default port (":80") that
	// normalization must strip.
	if r, ok := byUsername["userB"]; !ok || r.Port != "" || r.URL != "http://example.org" {
		t.Errorf("userB record = %+v, want default port stripped and URL http://example.org", r)
	}

	// userC's host is an IP address: no domain/subdomain/suffix.
	if r, ok := byUsername["userC"]; !ok || r.Domain != "" || r.Suffix != "" {
		t.Errorf("userC record = %+v, want empty Domain/Suffix for an IP host", r)
	}

	// userD's host has a multi-part public suffix.
	if r, ok := byUsername["userD"]; !ok || r.Domain != "example.co.uk" || r.Subdomain != "www" || r.Suffix != "co.uk" {
		t.Errorf("userD record = %+v, want Domain=example.co.uk Subdomain=www Suffix=co.uk", r)
	}
}
