package batch

import (
	"bufio"
	"context"
	"io"

	"github.com/yourorg/reader/internal/domain"
)

// maxLineBytes bounds the size of a single line this reader accepts.
const maxLineBytes = 1 << 20 // 1 MiB

// scanBufStart is bufio.Scanner's starting buffer size; it grows up to
// maxLineBytes only as needed for a long line, not by default.
const scanBufStart = 64 * 1024 // 64 KiB

// Stop collecting at this byte budget, with at most one line of overshoot.
const batchByteBudget = 8 << 20

type Reader struct {
	scanner    *bufio.Scanner
	batchSize  int
	lineNumber int64
	done       bool
}

func New(r io.Reader, batchSize int) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, scanBufStart), maxLineBytes)
	return &Reader{scanner: scanner, batchSize: batchSize}
}

// NextBatch returns up to batchSize lines, subject to a separate byte budget.
func (r *Reader) NextBatch(ctx context.Context) ([]domain.RawLine, bool, error) {
	if r.done {
		return nil, true, nil
	}

	lines := make([]domain.RawLine, 0, r.batchSize)
	bytes := 0
	for len(lines) < r.batchSize && bytes < batchByteBudget {
		text, ok, err := r.NextLine(ctx)
		if err != nil {
			return nil, r.done, err
		}
		if !ok {
			break
		}
		bytes += len(text)
		lines = append(lines, domain.RawLine{
			LineNumber: r.lineNumber,
			Text:       text,
		})
	}

	if len(lines) == 0 {
		return nil, true, nil
	}
	return lines, false, nil
}

func (r *Reader) NextLine(ctx context.Context) (string, bool, error) {
	ok, err := r.scan(ctx)
	if !ok {
		return "", false, err
	}
	return r.scanner.Text(), true, nil
}

// Err reports an error encountered while reading ahead, even if buffered
// tokens were available afterward.
func (r *Reader) Err() error {
	return r.scanner.Err()
}

// Skip advances exactly count lines without materializing discarded lines.
func (r *Reader) Skip(ctx context.Context, count int64) (int64, error) {
	var skipped int64
	for skipped < count {
		ok, err := r.scan(ctx)
		if !ok {
			return skipped, err
		}
		skipped++
	}
	return skipped, nil
}

func (r *Reader) scan(ctx context.Context) (bool, error) {
	if r.done {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !r.scanner.Scan() {
		r.done = true
		return false, r.scanner.Err()
	}
	r.lineNumber++
	return true, nil
}
