package extract

import (
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

// Candidate extracts URL/username/password from line using pattern's
// named capture groups ("url", "username", "password").
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
