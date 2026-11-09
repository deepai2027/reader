// Package extract pulls URL/username/password out of a single line
// using an already-selected, already-compiled regex pattern. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 3.
package extract

import (
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

// Candidate extracts URL/username/password from line using pattern's
// named capture groups ("url", "username", "password"). It returns
// (candidate, true) on a full match, or (zero value, false) if the
// line does not match the pattern at all, or the pattern is missing
// one of the three required named groups. Extraction never panics on
// a non-matching line - that's an expected, countable outcome, not an
// error (README section 5.3: invalid lines are dropped and counted).
func Candidate(pattern *regexengine.Pattern, line domain.RawLine) (domain.ExtractedCandidate, bool) {
	url, username, password, ok := pattern.Extract(line.Text)
	if !ok {
		return domain.ExtractedCandidate{}, false
	}
	return domain.ExtractedCandidate{
		LineNumber: line.LineNumber,
		URL:        url,
		Username:   username,
		Password:   password,
	}, true
}
