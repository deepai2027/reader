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

// NextBatch returns the next batch of up to batchSize lines.
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
