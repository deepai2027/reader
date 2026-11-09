// Package ports defines the interfaces Reader's core pipeline depends
// on instead of talking to infrastructure directly. Per the core-first
// strategy in specs/roadmap.md, Phase 1/2 implement local,
// dependency-free adapters for these interfaces (see the stub package
// in this module for the Phase 0 placeholders); the Final Phase
// replaces them with real PostgreSQL-, RabbitMQ-, MinIO-, and
// Agent-backed adapters. Core pipeline code is written against these
// interfaces only and should never need to change when an adapter is
// swapped out from under it.
package ports

import (
	"context"
	"errors"
	"io"

	"github.com/yourorg/reader/internal/domain"
)

// ErrTransient marks a publish failure that is safe to retry (e.g. a
// temporary connection problem). Adapters should wrap transient causes
// with it (fmt.Errorf("%w: ...", ports.ErrTransient)) so callers can
// test for it with errors.Is. Phase 2's retry/backoff logic depends on
// this distinction.
var ErrTransient = errors.New("transient publish failure")

// ErrPermanent marks a publish failure that will not succeed on retry
// (e.g. a malformed message). Adapters should wrap permanent causes
// with it the same way as ErrTransient.
var ErrPermanent = errors.New("permanent publish failure")

// ProgressStore persists and recovers file processing state. The real
// adapter is PostgreSQL-backed (Final Phase / Step A); Phase 1/2 use a
// local in-memory or JSON-file-backed implementation built on top of
// the Phase 0 stub in this module.
type ProgressStore interface {
	// Save persists the given FileState, creating or updating the
	// record for state.FileID.
	Save(ctx context.Context, state domain.FileState) error

	// Load retrieves the current FileState for fileID. It returns
	// (zero value, false, nil) if no record exists for that file.
	Load(ctx context.Context, fileID string) (domain.FileState, bool, error)

	// FindReading returns the file currently in domain.FileStatusReading,
	// if any. Reader must check this on startup before polling the
	// Agent for a new file (README §6, §14). It returns (zero value,
	// false, nil) if no file is currently being processed.
	FindReading(ctx context.Context) (domain.FileState, bool, error)
}

// MessagePublisher delivers processed records and unique URLs to their
// respective destinations. The real adapter publishes to RabbitMQ
// (Final Phase / Step D); Phase 1/2 use a local adapter that implements
// real retry/backoff/acknowledgement semantics against an in-memory
// sink, so reliability behavior is proven before a real broker is
// involved (specs/roadmap.md, Phase 2).
//
// Each method publishes a single independent message — Reader never
// combines multiple records into one message (README §5.3, §9.1).
type MessagePublisher interface {
	// PublishRecord delivers one ValidatedRecord. Implementations must
	// distinguish transient failures (safe to retry) from permanent
	// failures (will not succeed on retry) via the returned error,
	// using ErrTransient/ErrPermanent — Phase 2's retry logic depends
	// on being able to tell these apart.
	PublishRecord(ctx context.Context, record domain.ValidatedRecord) error

	// PublishUniqueURL delivers one UniqueURLMessage, under the same
	// transient/permanent failure contract as PublishRecord.
	PublishUniqueURL(ctx context.Context, msg domain.UniqueURLMessage) error
}

// RegexSource supplies the active Regex patterns available for file
// identification. The real adapter reads from PostgreSQL's `regex`
// table (Final Phase / Step A); Phase 1/2 use a static, in-process list
// built on top of the Phase 0 stub in this module.
type RegexSource interface {
	// ActivePatterns returns all currently active Regex patterns.
	// Disabled patterns must never be included (README §1.1).
	ActivePatterns(ctx context.Context) ([]domain.RegexPattern, error)
}

// FileSource supplies the byte stream Reader processes for a given file
// reference. The real adapter streams a download from MinIO (Final
// Phase / Step B); Phase 1/2 use a local-filesystem adapter built on
// top of the Phase 0 stub in this module, so the pipeline can be built
// and tested against ordinary local files.
type FileSource interface {
	// Open returns a stream for the file identified by ref (a MinIO
	// object key in the real adapter, a local path in the Phase 0–2
	// stub/local adapters). Callers are responsible for closing the
	// returned ReadCloser.
	Open(ctx context.Context, ref string) (io.ReadCloser, error)
}
