// Package stub provides minimal, no-op/in-memory implementations of
// Reader's core ports (see the parent ports package). These exist only
// to let the Phase 0 scaffolding compile, link, and run end-to-end
// against something — they are NOT the real Phase 1/2 local adapters
// and contain no meaningful persistence, retry, or delivery logic.
//
// Phase 1 and Phase 2 replace these with real local-adapter logic
// (checkpointed persistence, retry/backoff, real regex storage). The
// Final Phase replaces them again with real infrastructure adapters
// (PostgreSQL, RabbitMQ, MinIO). If you are implementing Phase 1/2 and
// are tempted to add real logic here, don't — create the real local
// adapter alongside this package instead, and leave this one as the
// Phase 0 placeholder it's documented to be.
package stub

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
)

// Compile-time interface satisfaction checks. If a stub ever drifts
// out of sync with its port interface, this fails the build instead of
// failing silently at wiring time.
var (
	_ ports.ProgressStore    = (*ProgressStore)(nil)
	_ ports.MessagePublisher = (*MessagePublisher)(nil)
	_ ports.RegexSource      = (*RegexSource)(nil)
	_ ports.FileSource       = (*FileSource)(nil)
)

// ProgressStore is an in-memory, non-persistent ports.ProgressStore.
// State is lost on process restart — it exists only to satisfy the
// interface during Phase 0. Phase 2 replaces this with a real local
// adapter that actually survives a restart.
type ProgressStore struct {
	mu    sync.Mutex
	files map[string]domain.FileState
}

// NewProgressStore returns an empty in-memory ProgressStore.
func NewProgressStore() *ProgressStore {
	return &ProgressStore{files: make(map[string]domain.FileState)}
}

func (s *ProgressStore) Save(_ context.Context, state domain.FileState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[state.FileID] = state
	return nil
}

func (s *ProgressStore) Load(_ context.Context, fileID string) (domain.FileState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.files[fileID]
	return state, ok, nil
}

func (s *ProgressStore) FindReading(_ context.Context) (domain.FileState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range s.files {
		if state.Status == domain.FileStatusReading {
			return state, true, nil
		}
	}
	return domain.FileState{}, false, nil
}

// MessagePublisher logs what would have been published and always
// succeeds. It implements no retry/backoff/acknowledgement logic —
// Phase 2 builds that into the real local adapter.
type MessagePublisher struct {
	Logger *slog.Logger
}

// NewMessagePublisher returns a MessagePublisher that logs via logger.
func NewMessagePublisher(logger *slog.Logger) *MessagePublisher {
	return &MessagePublisher{Logger: logger}
}

func (p *MessagePublisher) PublishRecord(_ context.Context, record domain.ValidatedRecord) error {
	p.Logger.Debug("stub publish: record", "record_hash", record.RecordHash, "domain", record.Domain)
	return nil
}

func (p *MessagePublisher) PublishUniqueURL(_ context.Context, msg domain.UniqueURLMessage) error {
	p.Logger.Debug("stub publish: unique url", "url", msg.URL, "priority", msg.Priority)
	return nil
}

// RegexSource returns a single hardcoded pattern for local
// smoke-testing. Phase 1 replaces this with a real local adapter (a
// static in-process list, per specs/roadmap.md); the Final Phase
// replaces it again with a PostgreSQL-backed adapter.
type RegexSource struct{}

// NewRegexSource returns a RegexSource with one hardcoded test pattern.
func NewRegexSource() *RegexSource {
	return &RegexSource{}
}

func (RegexSource) ActivePatterns(_ context.Context) ([]domain.RegexPattern, error) {
	return []domain.RegexPattern{
		{
			ID:       1,
			Name:     "stub-test-pattern",
			Pattern:  `^(?P<url>\S+):(?P<username>\S+):(?P<password>\S+)$`,
			IsActive: true,
		},
	}, nil
}

// FileSource opens files from the local filesystem by path. Note this
// is already structurally close to what Phase 1's local-filesystem
// FileSource looks like per specs/roadmap.md — it's the Final Phase's
// MinIO-backed adapter that changes behavior, not this shape.
type FileSource struct{}

// NewFileSource returns a FileSource that reads local files.
func NewFileSource() *FileSource {
	return &FileSource{}
}

func (FileSource) Open(_ context.Context, ref string) (io.ReadCloser, error) {
	return os.Open(ref)
}
