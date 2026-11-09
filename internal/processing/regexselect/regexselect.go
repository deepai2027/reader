// Package regexselect chooses which predefined Regex pattern applies
// to a file, by testing every active pattern against the file's first
// WindowSize lines (README section 4). See
// specs/2026-11-09-core-file-processing-pipeline/requirements.md for
// the tie-break rule this package implements.
package regexselect

import (
	"context"
	"log/slog"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

// WindowSize is the number of lines inspected when selecting a Regex
// pattern for a file (README section 4). It is a package-level
// constant, not a config value, because changing it has
// format/semantics implications beyond ordinary tuning.
const WindowSize = 5000

// Result describes the outcome of regex selection against a window of
// lines. If Matched is false, Pattern is the zero value and the
// caller should treat the file as having no applicable pattern
// (README section 4.2) rather than inspecting Pattern.
type Result struct {
	Pattern domain.RegexPattern
	Matched bool
}

// Select applies the tie-break rule from requirements.md: the active
// pattern matching the most lines in the window wins; on an exact,
// non-zero tie, the pattern with the lowest ID wins. Patterns that
// fail to compile are logged and skipped - they are excluded from
// both the match-count comparison and the tie-break, never treated as
// a fatal error for the file.
func Select(_ context.Context, logger *slog.Logger, patterns []domain.RegexPattern, lines []domain.RawLine) (Result, error) {
	type candidate struct {
		pattern  domain.RegexPattern
		compiled *regexengine.Pattern
		count    int
	}

	candidates := make([]candidate, 0, len(patterns))
	for _, p := range patterns {
		if !p.IsActive {
			continue
		}
		compiled, err := regexengine.Compile(p.Pattern)
		if err != nil {
			if logger != nil {
				logger.Warn("regex pattern failed to compile; skipping",
					"pattern_id", p.ID, "pattern_name", p.Name, "error", err)
			}
			continue
		}
		candidates = append(candidates, candidate{pattern: p, compiled: compiled})
	}

	for i := range candidates {
		for _, line := range lines {
			if candidates[i].compiled.MatchString(line.Text) {
				candidates[i].count++
			}
		}
	}

	var best *candidate
	for i := range candidates {
		c := &candidates[i]
		if c.count == 0 {
			continue
		}
		switch {
		case best == nil:
			best = c
		case c.count > best.count:
			best = c
		case c.count == best.count && c.pattern.ID < best.pattern.ID:
			best = c
		}
	}

	if best == nil {
		return Result{Matched: false}, nil
	}
	return Result{Pattern: best.pattern, Matched: true}, nil
}
