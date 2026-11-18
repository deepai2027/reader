package local

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
)

// RetryingPublisher wraps an underlying ports.MessagePublisher
type RetryingPublisher struct {
	sink        ports.MessagePublisher
	maxAttempts int
	baseDelay   time.Duration
	sleep       func(time.Duration) // seam for testing
}

// New returns a RetryingPublisher. maxAttempts and baseDelayMS
// normally come from internal/config (RetryMaxAttempts,
// RetryBaseDelayMS).
func New(sink ports.MessagePublisher, maxAttempts, baseDelayMS int) *RetryingPublisher {
	return &RetryingPublisher{
		sink:        sink,
		maxAttempts: maxAttempts,
		baseDelay:   time.Duration(baseDelayMS) * time.Millisecond,
		sleep:       time.Sleep,
	}
}

func (p *RetryingPublisher) PublishRecord(ctx context.Context, r domain.ValidatedRecord) error {
	return p.retry(ctx, func() error { return p.sink.PublishRecord(ctx, r) })
}

func (p *RetryingPublisher) PublishUniqueURL(ctx context.Context, m domain.UniqueURLMessage) error {
	return p.retry(ctx, func() error { return p.sink.PublishUniqueURL(ctx, m) })
}

func (p *RetryingPublisher) retry(ctx context.Context, attempt func() error) error {
	var lastErr error
	for i := 0; i < p.maxAttempts; i++ {
		err := attempt()
		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, ports.ErrPermanent) {
			return err // never retried
		}
		if !errors.Is(err, ports.ErrTransient) {
			return err // unannotated error: treated as permanent
		}
		if i == p.maxAttempts-1 {
			break // exhausted; fall through to the wrapped error below
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		delay := p.baseDelay * time.Duration(1<<uint(i)) // exponential backoff
		p.sleep(delay)
	}
	return fmt.Errorf("delivery: exhausted %d attempts: %w", p.maxAttempts, lastErr)
}
