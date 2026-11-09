package regexselect

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yourorg/reader/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func rawLines(texts ...string) []domain.RawLine {
	lines := make([]domain.RawLine, len(texts))
	for i, t := range texts {
		lines[i] = domain.RawLine{LineNumber: int64(i + 1), Text: t}
	}
	return lines
}

func TestSelect_SinglePatternMatches(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 1, Pattern: `^a.*$`, IsActive: true},
	}
	lines := rawLines("abc", "xyz")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched || result.Pattern.ID != 1 {
		t.Fatalf("got %+v, want pattern 1 matched", result)
	}
}

func TestSelect_MostMatchesWins(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 1, Pattern: `^a.*$`, IsActive: true}, // matches 1 line
		{ID: 2, Pattern: `^.*$`, IsActive: true},  // matches all 3 lines
	}
	lines := rawLines("abc", "xyz", "def")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched || result.Pattern.ID != 2 {
		t.Fatalf("got %+v, want pattern 2 (most matches) selected", result)
	}
}

func TestSelect_TieBreaksOnLowestID(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 5, Pattern: `^a.*$`, IsActive: true}, // matches "abc" -> 1
		{ID: 2, Pattern: `^.bc$`, IsActive: true}, // matches "abc" -> 1
	}
	lines := rawLines("abc")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched || result.Pattern.ID != 2 {
		t.Fatalf("got %+v, want lowest-ID pattern 2 to win the tie", result)
	}
}

func TestSelect_NoPatternsMatch(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 1, Pattern: `^zzz$`, IsActive: true},
	}
	lines := rawLines("abc", "def")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Matched {
		t.Fatalf("got %+v, want Matched=false", result)
	}
}

func TestSelect_InactivePatternsIgnored(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 1, Pattern: `^.*$`, IsActive: false},
	}
	lines := rawLines("abc")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Matched {
		t.Fatalf("got %+v, want inactive pattern to be ignored", result)
	}
}

func TestSelect_MalformedPatternSkippedNotFatal(t *testing.T) {
	patterns := []domain.RegexPattern{
		{ID: 1, Pattern: `(unclosed`, IsActive: true},
		{ID: 2, Pattern: `^abc$`, IsActive: true},
	}
	lines := rawLines("abc")
	result, err := Select(context.Background(), testLogger(), patterns, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched || result.Pattern.ID != 2 {
		t.Fatalf("got %+v, want the malformed pattern skipped and pattern 2 selected", result)
	}
}
