package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports/stub"
)

type streamingSource string

func (s streamingSource) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(s))), nil
}

func TestRun_StreamingSelectionReplaysReadAhead(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	const line = "https://example.test:user:secret\n"
	const lines = 5033
	publisher := &countingPublisher{}
	deps := recoveryTestDeps(publisher, stub.NewProgressStore())
	deps.Files = streamingSource(strings.Repeat(line, lines))
	deps.Config.BatchSize = 128
	stats, err := Run(t.Context(), "stream", "", 8, deps)
	if err != nil || stats.MatchedCount != lines || publisher.records != lines || publisher.urls != 1 {
		t.Fatalf("stats=%+v publisher=%+v err=%v", stats, publisher, err)
	}
}

func TestRun_ResumeAfterBatchSizeChange(t *testing.T) {
	progress := stub.NewProgressStore()
	crasher := &crashAfterNPublisher{n: 2}
	deps := recoveryTestDeps(crasher, progress)
	deps.Files = streamingSource("https://one.test:user:secret\nhttps://two.test:user:secret\nhttps://three.test:user:secret\n")
	if _, err := Run(t.Context(), "resize", "", 9, deps); !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("expected simulated crash, got %v", err)
	}

	publisher := &capturingPublisher{}
	deps.Publisher = publisher
	deps.Config.BatchSize = 1000
	stats, err := Run(t.Context(), "resize", "", 2, deps)
	if err != nil || !stats.Resumed || stats.MatchedCount != 3 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if len(publisher.unique) != 1 || publisher.unique[0].URL != "https://three.test" || publisher.unique[0].Priority != 9 {
		t.Fatalf("unexpected resumed URLs: %+v", publisher.unique)
	}
}

func TestRun_OversizeLineLeavesLastCheckpoint(t *testing.T) {
	const valid = "https://example.test:user:secret\n"
	const scannerLimit = 1 << 20
	publisher := &countingPublisher{}
	progress := stub.NewProgressStore()
	deps := recoveryTestDeps(publisher, progress)
	deps.Config.BatchSize = 500
	line := strings.Repeat(" ", scannerLimit-1) + "\n"
	deps.Files = streamingSource(strings.Repeat(valid, 5000) + strings.Repeat(line, 12) +
		strings.Repeat(" ", scannerLimit) + "\n" + valid)
	if _, err := Run(t.Context(), "line-limit", "", 9, deps); err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("want scanner limit error, got %v", err)
	}
	state, found, err := progress.Load(t.Context(), "line-limit")
	if err != nil || !found || state.Status != domain.FileStatusReading || state.CurrentLine != 5009 ||
		state.MatchedCount != 5000 || publisher.records != 5000 || publisher.urls != 1 {
		t.Fatalf("unexpected checkpoint: %+v, found=%t publisher=%+v err=%v", state, found, publisher, err)
	}
}

type endErrorSource string

func (s endErrorSource) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(endErrorReader{strings.NewReader(string(s))}), nil
}

type endErrorReader struct{ *strings.Reader }

func (r endErrorReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if r.Len() == 0 {
		return n, errSimulatedCrash
	}
	return n, err
}

func TestRun_SelectionDoesNotDiscardReadAheadError(t *testing.T) {
	progress := stub.NewProgressStore()
	publisher := &countingPublisher{}
	deps := recoveryTestDeps(publisher, progress)
	deps.Files = endErrorSource(strings.Repeat("https://example.test:user:secret\n", 5000))
	if _, err := Run(t.Context(), "read-error", "", 9, deps); !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("expected read-ahead error, got %v", err)
	}
	if _, found, err := progress.Load(t.Context(), "read-error"); err != nil || found || publisher.records != 0 {
		t.Fatalf("unexpected work after a sample read error: found=%t publisher=%+v err=%v", found, publisher, err)
	}
}
