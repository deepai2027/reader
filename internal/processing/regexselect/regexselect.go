package regexselect

import (
	"context"
	"log/slog"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

const WindowSize = 5000

type Result struct {
	Pattern domain.RegexPattern
	Matched bool
}

type candidate struct {
	pattern  domain.RegexPattern
	compiled *regexengine.Pattern
	count    int
}

// Selector accumulates match counts without retaining the sampled lines.
type Selector struct {
	candidates []candidate
}

func New(logger *slog.Logger, patterns []domain.RegexPattern) *Selector {
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
	return &Selector{candidates: candidates}
}

func (s *Selector) Observe(line string) {
	for i := range s.candidates {
		c := &s.candidates[i]
		if c.compiled.MatchString(line) {
			c.count++
		}
	}
}

func (s *Selector) Result() Result {
	var best *candidate
	for i := range s.candidates {
		c := &s.candidates[i]
		if c.count > 0 && (best == nil || c.count > best.count ||
			(c.count == best.count && c.pattern.ID < best.pattern.ID)) {
			best = c
		}
	}
	if best == nil {
		return Result{}
	}
	return Result{Pattern: best.pattern, Matched: true}
}

func Select(ctx context.Context, logger *slog.Logger, patterns []domain.RegexPattern, lines []domain.RawLine) (Result, error) {
	selector := New(logger, patterns)
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		selector.Observe(line.Text)
	}
	return selector.Result(), nil
}
