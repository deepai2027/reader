// Package batch streams a file's lines in fixed-size batches, without
// ever holding more than one batch's worth of lines in memory. It is
// the only place in the pipeline that touches raw file I/O - see
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 1.
package batch

import (
	"bufio"
	"context"
	"io"

	"github.com/yourorg/reader/internal/domain"
)

// maxLineBytes bounds the size of a single line this reader accepts.
// A line longer than this is a data-quality problem, not something
// solved by growing buffers without limit.
const maxLineBytes = 1 << 20 // 1 MiB

// scanBufStart is bufio.Scanner's starting buffer size; it grows up to
// maxLineBytes only as needed for a long line, not by default.
const scanBufStart = 64 * 1024 // 64 KiB

// Reader streams domain.RawLine values out of an io.Reader in batches
// of up to batchSize lines. Line numbers are 1-based and monotonically
// increasing across calls, so later stages (and Phase 2's
// checkpointing) can rely on them as a stable position marker.
type Reader struct {
	scanner    *bufio.Scanner
	batchSize  int
	lineNumber int64
	done       bool
}

// New wraps r (typically the stream from ports.FileSource.Open) in a
// batching line reader. batchSize must be a positive integer;
// internal/config already validates BATCH_SIZE on load, so Reader
// does not re-validate it here.
func New(r io.Reader, batchSize int) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, scanBufStart), maxLineBytes)
	return &Reader{scanner: scanner, batchSize: batchSize}
}

// NextBatch returns the next batch of up to batchSize lines.
//
// It returns (batch, false, nil) for a batch read successfully, even
// if that batch turns out to be the last one with content (done still
// becomes true, but not until the call after it finds nothing left).
// Once the underlying reader is exhausted, it returns (nil, true, nil)
// on every subsequent call. A non-nil error (e.g. a line exceeding
// maxLineBytes) is returned alongside done=true and is terminal - do
// not call NextBatch again after an error.
func (r *Reader) NextBatch(ctx context.Context) ([]domain.RawLine, bool, error) {
	if r.done {
		return nil, true, nil
	}

	lines := make([]domain.RawLine, 0, r.batchSize)
	for len(lines) < r.batchSize {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}

		if !r.scanner.Scan() {
			r.done = true
			if err := r.scanner.Err(); err != nil {
				return nil, true, err
			}
			break
		}
		r.lineNumber++
		lines = append(lines, domain.RawLine{
			LineNumber: r.lineNumber,
			Text:       r.scanner.Text(),
		})
	}

	if len(lines) == 0 {
		return nil, true, nil
	}
	return lines, false, nil
}
