package extract

import (
	"testing"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

const testPattern = `^(?P<url>\S+):(?P<username>\S+):(?P<password>\S+)$`

func mustCompile(t *testing.T, pattern string) *regexengine.Pattern {
	t.Helper()
	p, err := regexengine.Compile(pattern)
	if err != nil {
		t.Fatalf("compile %q: %v", pattern, err)
	}
	return p
}

func TestCandidate_MatchingLineExtractsAllFields(t *testing.T) {
	p := mustCompile(t, testPattern)
	line := domain.RawLine{LineNumber: 7, Text: "https://example.com:alice:s3cret"}

	c, ok := Candidate(p, line)
	if !ok {
		t.Fatal("expected a match")
	}
	if c.LineNumber != 7 {
		t.Errorf("LineNumber = %d, want 7", c.LineNumber)
	}
	if c.URL != "https://example.com" || c.Username != "alice" || c.Password != "s3cret" {
		t.Errorf("got %+v", c)
	}
}

func TestCandidate_NonMatchingLineIsDropped(t *testing.T) {
	p := mustCompile(t, testPattern)
	line := domain.RawLine{LineNumber: 3, Text: "this line has spaces and no colons"}

	_, ok := Candidate(p, line)
	if ok {
		t.Fatal("expected no match for a non-conforming line")
	}
}

func TestCandidate_PatternMissingRequiredGroupNeverMatches(t *testing.T) {
	// A pattern with no named groups at all can still match the line
	// structurally, but must never produce a candidate - the required
	// group names are absent.
	p := mustCompile(t, `^\S+:\S+:\S+$`)
	line := domain.RawLine{LineNumber: 1, Text: "a:b:c"}

	_, ok := Candidate(p, line)
	if ok {
		t.Fatal("expected no candidate when required named groups are absent")
	}
}
