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
