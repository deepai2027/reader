package local

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
)

type fakeSink struct {
	failTimes int
	calls     int
	err       error
}

func (s *fakeSink) PublishRecord(_ context.Context, _ domain.ValidatedRecord) error {
	s.calls++
	if s.calls <= s.failTimes {
		return s.err
	}
	return nil
}

func (s *fakeSink) PublishUniqueURL(_ context.Context, _ domain.UniqueURLMessage) error {
	return nil
}

func noSleep(time.Duration) {}

func TestRetryingPublisher_TransientRetriesThenSucceeds(t *testing.T) {
	sink := &fakeSink{failTimes: 2, err: fmt.Errorf("wrap: %w", ports.ErrTransient)}
	p := New(sink, 5, 1)
	p.sleep = noSleep

	if err := p.PublishRecord(context.Background(), domain.ValidatedRecord{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink.calls != 3 {
		t.Errorf("sink called %d times, want 3 (2 failures + 1 success)", sink.calls)
	}
}

func TestRetryingPublisher_TransientExhaustsRetries(t *testing.T) {
	sink := &fakeSink{failTimes: 10, err: fmt.Errorf("wrap: %w", ports.ErrTransient)}
	p := New(sink, 3, 1)
	p.sleep = noSleep

	err := p.PublishRecord(context.Background(), domain.ValidatedRecord{})
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if !errors.Is(err, ports.ErrTransient) {
		t.Errorf("expected the exhausted error to still wrap ErrTransient, got %v", err)
	}
	if sink.calls != 3 {
		t.Errorf("sink called %d times, want exactly maxAttempts (3)", sink.calls)
	}
}

func TestRetryingPublisher_PermanentFailsImmediately(t *testing.T) {
	sink := &fakeSink{failTimes: 10, err: fmt.Errorf("wrap: %w", ports.ErrPermanent)}
	p := New(sink, 5, 1)
	p.sleep = noSleep

	err := p.PublishRecord(context.Background(), domain.ValidatedRecord{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if sink.calls != 1 {
		t.Errorf("sink called %d times, want exactly 1 (no retry for a permanent error)", sink.calls)
	}
}

func TestRetryingPublisher_UnannotatedErrorTreatedAsPermanent(t *testing.T) {
	sink := &fakeSink{failTimes: 10, err: errors.New("mystery failure")}
	p := New(sink, 5, 1)
	p.sleep = noSleep

	err := p.PublishRecord(context.Background(), domain.ValidatedRecord{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if sink.calls != 1 {
		t.Errorf("sink called %d times, want exactly 1 (unannotated errors are not retried)", sink.calls)
	}
}

func TestRetryingPublisher_BackoffGrows(t *testing.T) {
	sink := &fakeSink{failTimes: 3, err: fmt.Errorf("wrap: %w", ports.ErrTransient)}
	p := New(sink, 5, 10)

	var delays []time.Duration
	p.sleep = func(d time.Duration) { delays = append(delays, d) }

	if err := p.PublishRecord(context.Background(), domain.ValidatedRecord{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delays) != 3 {
		t.Fatalf("got %d delays, want 3 (one between each of the 3 failed attempts and the next)", len(delays))
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Errorf("delay[%d]=%v did not grow from delay[%d]=%v", i, delays[i], i-1, delays[i-1])
		}
	}
}

func TestRetryingPublisher_ContextCancellationStopsRetries(t *testing.T) {
	sink := &fakeSink{failTimes: 10, err: fmt.Errorf("wrap: %w", ports.ErrTransient)}
	p := New(sink, 10, 1)
	p.sleep = noSleep

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.PublishRecord(ctx, domain.ValidatedRecord{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if sink.calls != 1 {
		t.Errorf("sink called %d times, want exactly 1 (cancellation checked before the first retry sleep)", sink.calls)
	}
}
