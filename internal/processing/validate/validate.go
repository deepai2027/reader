package validate

import (
	"fmt"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
)

type Result struct {
	Valid  bool
	Reason string
}


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
