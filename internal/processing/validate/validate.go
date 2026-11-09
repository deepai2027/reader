// Package validate checks an extracted candidate's structural
// validity before it is normalized, hashed, or published. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 4 and
// README section 7.2.
package validate

import (
	"fmt"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
)

// Result describes whether a candidate passed validation, and if not,
// why. The reason matters for diagnosing why a file's matched_count
// came out lower than expected (README section 7.3) - callers should
// log it at debug level rather than discard it.
type Result struct {
	Valid  bool
	Reason string
}

// Candidate checks c against cfg's configured length bounds. The URL
// is required to be non-empty; username and password must each fall
// within their configured [min, max] length range, inclusive on both
// ends.
func Candidate(cfg config.Config, c domain.ExtractedCandidate) Result {
	if c.URL == "" {
		return Result{Valid: false, Reason: "empty url"}
	}
	if n := len(c.Username); n < cfg.MinUsernameLength || n > cfg.MaxUsernameLength {
		return Result{Valid: false, Reason: fmt.Sprintf(
			"username length %d outside [%d,%d]", n, cfg.MinUsernameLength, cfg.MaxUsernameLength)}
	}
	if n := len(c.Password); n < cfg.MinPasswordLength || n > cfg.MaxPasswordLength {
		return Result{Valid: false, Reason: fmt.Sprintf(
			"password length %d outside [%d,%d]", n, cfg.MinPasswordLength, cfg.MaxPasswordLength)}
	}
	return Result{Valid: true}
}
