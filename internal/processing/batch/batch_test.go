package batch

import (
	"context"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func collect(t *testing.T, r *Reader) [][]string {
	t.Helper()
	var batches [][]string
	for {
		lines, done, err := r.NextBatch(context.Background())
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		if len(lines) > 0 {
			texts := make([]string, len(lines))
			for i, l := range lines {
				texts[i] = l.Text
			}
			batches = append(batches, texts)
		}
		if done {
			break
		}
	}
	return batches
}

func TestEmptyFile(t *testing.T) {
	r := New(strings.NewReader(""), 10)
	batches := collect(t, r)
	if len(batches) != 0 {
		t.Fatalf("got %d batches, want 0", len(batches))
	}
}

func TestSingleLineNoTrailingNewline(t *testing.T) {
	r := New(strings.NewReader("only line, no newline"), 10)
	batches := collect(t, r)
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("got %v, want one batch with one line", batches)
	}
	if batches[0][0] != "only line, no newline" {
		t.Fatalf("got %q", batches[0][0])
	}
}

func TestBlankLinesPreserved(t *testing.T) {
	r := New(strings.NewReader("a\n\nb\n"), 10)
	batches := collect(t, r)
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1", len(batches))
	}
	want := []string{"a", "", "b"}
	got := batches[0]
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBatchBoundaryExactlyOnLastLine(t *testing.T) {
	r := New(strings.NewReader("one\ntwo\n"), 2)
	batches := collect(t, r)
	if len(batches) != 1 || len(batches[0]) != 2 {
		t.Fatalf("got %v, want a single batch of 2", batches)
	}
}

func TestLineNumbersMonotonicAcrossBatches(t *testing.T) {
	r := New(strings.NewReader("a\nb\nc\nd\ne\n"), 2)
	var gotLineNumbers []int64
	for {
		lines, done, err := r.NextBatch(context.Background())
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		for _, l := range lines {
			gotLineNumbers = append(gotLineNumbers, l.LineNumber)
		}
		if done {
			break
		}
	}
	for i, n := range gotLineNumbers {
		if n != int64(i+1) {
			t.Fatalf("line numbers = %v, want 1..%d in order", gotLineNumbers, len(gotLineNumbers))
		}
	}
}

// repeatingLineReader generates n copies of line, streaming them on
// demand rather than materializing the whole logical file in memory -
// this is what lets TestMemoryBoundedOverLargeInput actually test
// boundedness rather than just measure a pre-built giant byte slice.
type repeatingLineReader struct {
	remaining int
	line      []byte
	buf       []byte
	pos       int
}

func newRepeatingLineReader(n int, line string) *repeatingLineReader {
	return &repeatingLineReader{remaining: n, line: []byte(line + "\n")}
}

func (r *repeatingLineReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.buf) {
		if r.remaining <= 0 {
			return 0, io.EOF
		}
		r.buf = r.line
		r.pos = 0
		r.remaining--
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}

// TestMemoryBoundedOverLargeInput is a sanity check, not a proof: it
// streams a logically large input (well beyond what we'd want
// resident in memory at once) and asserts heap growth stays small
// relative to the logical size, catching the obvious regression of
// someone replacing the streaming scan with a full read/buffer.
func TestMemoryBoundedOverLargeInput(t *testing.T) {
	const numLines = 2_000_000
	const line = "0123456789012345678901234567890123456789" // 40 bytes
	logicalBytes := int64(numLines) * int64(len(line)+1)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	r := New(newRepeatingLineReader(numLines, line), 1000)
	var total int64
	for {
		lines, done, err := r.NextBatch(context.Background())
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		total += int64(len(lines))
		if done {
			break
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if total != numLines {
		t.Fatalf("processed %d lines, want %d", total, numLines)
	}

	const maxExpectedGrowth = 20 * 1024 * 1024 // 20 MiB
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if growth > maxExpectedGrowth {
		t.Errorf("heap grew by %d bytes (%.1f MiB) processing %s of logical input; want under %d bytes",
			growth, float64(growth)/(1024*1024), formatBytes(logicalBytes), maxExpectedGrowth)
	}
}

func formatBytes(n int64) string {
	return strconv.FormatInt(n/(1024*1024), 10) + " MiB"
}
