package validate

import (
	"testing"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
)

func testConfig() config.Config {
	return config.Config{
		MinUsernameLength: 2,
		MaxUsernameLength: 10,
		MinPasswordLength: 4,
		MaxPasswordLength: 20,
	}
}

func TestCandidate_Valid(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "https://example.com", Username: "alice", Password: "password123"}
	result := Candidate(testConfig(), c)
	if !result.Valid {
		t.Fatalf("got invalid: %q, want valid", result.Reason)
	}
}

func TestCandidate_EmptyURL(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "", Username: "alice", Password: "password123"}
	result := Candidate(testConfig(), c)
	if result.Valid {
		t.Fatal("expected empty URL to be invalid")
	}
}

func TestCandidate_UsernameTooShort(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "https://example.com", Username: "a", Password: "password123"}
	result := Candidate(testConfig(), c)
	if result.Valid {
		t.Fatal("expected too-short username to be invalid")
	}
}

func TestCandidate_UsernameTooLong(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "https://example.com", Username: "waytoolongusername", Password: "password123"}
	result := Candidate(testConfig(), c)
	if result.Valid {
		t.Fatal("expected too-long username to be invalid")
	}
}

func TestCandidate_PasswordTooShort(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "https://example.com", Username: "alice", Password: "abc"}
	result := Candidate(testConfig(), c)
	if result.Valid {
		t.Fatal("expected too-short password to be invalid")
	}
}

func TestCandidate_PasswordTooLong(t *testing.T) {
	c := domain.ExtractedCandidate{URL: "https://example.com", Username: "alice", Password: "this-password-is-way-too-long-to-pass"}
	result := Candidate(testConfig(), c)
	if result.Valid {
		t.Fatal("expected too-long password to be invalid")
	}
}

func TestCandidate_BoundaryValuesPass(t *testing.T) {
	cfg := testConfig()
	c := domain.ExtractedCandidate{
		URL:      "https://example.com",
		Username: "ab",            // exactly MinUsernameLength
		Password: "abcd",          // exactly MinPasswordLength
	}
	if result := Candidate(cfg, c); !result.Valid {
		t.Fatalf("expected minimum-length boundary values to pass, got invalid: %q", result.Reason)
	}

	c.Username = "abcdefghij" // exactly MaxUsernameLength (10)
	c.Password = "abcdefghijklmnopqrst" // exactly MaxPasswordLength (20)
	if result := Candidate(cfg, c); !result.Valid {
		t.Fatalf("expected maximum-length boundary values to pass, got invalid: %q", result.Reason)
	}
}
