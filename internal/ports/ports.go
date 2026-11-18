package ports

import (
	"context"
	"errors"
	"io"

	"github.com/yourorg/reader/internal/domain"
)

// ErrTransient marks a publish failure that is safe to retry (e.g. a
// temporary connection problem).
var ErrTransient = errors.New("transient publish failure")

// ErrPermanent marks a publish failure that will not succeed on retry
// (e.g. a malformed message).
var ErrPermanent = errors.New("permanent publish failure")

// ProgressStore persists and recovers file processing state.
type ProgressStore interface {
	Save(ctx context.Context, state domain.FileState) error

	Load(ctx context.Context, fileID string) (domain.FileState, bool, error)

	FindReading(ctx context.Context) (domain.FileState, bool, error)
}

// MessagePublisher delivers processed records and unique URLs to their
// respective destinations.
type MessagePublisher interface {
	// PublishRecord delivers one ValidatedRecord.
	PublishRecord(ctx context.Context, record domain.ValidatedRecord) error

	// PublishUniqueURL delivers one UniqueURLMessage, under the same
	// transient/permanent failure contract as PublishRecord.
	PublishUniqueURL(ctx context.Context, msg domain.UniqueURLMessage) error
}

// RegexSource supplies the active Regex patterns available for file
// identification.
type RegexSource interface {
	// ActivePatterns returns all currently active Regex patterns.
	// Disabled patterns must never be included (README §1.1).
	ActivePatterns(ctx context.Context) ([]domain.RegexPattern, error)
}

// FileSource supplies the byte stream Reader processes for a given file
// reference.
type FileSource interface {
	Open(ctx context.Context, ref string) (io.ReadCloser, error)
}
