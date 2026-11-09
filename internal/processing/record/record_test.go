package record

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestHash_Deterministic(t *testing.T) {
	h1 := Hash("https://example.com", "alice", "s3cret")
	h2 := Hash("https://example.com", "alice", "s3cret")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q != %q", h1, h2)
	}
}

func TestHash_PinnedFormula(t *testing.T) {
	// This pins the exact formula (url|username|password, sha256,
	// hex-encoded) so a change to delimiter or field order fails loudly
	// here rather than only surfacing as a silent dedup/consumer
	// mismatch downstream.
	want := sha256.Sum256([]byte("https://example.com|alice|s3cret"))
	wantHex := hex.EncodeToString(want[:])

	got := Hash("https://example.com", "alice", "s3cret")
	if got != wantHex {
		t.Fatalf("Hash formula changed: got %q, want %q", got, wantHex)
	}
}

func TestHash_DifferentInputsDifferentHashes(t *testing.T) {
	h1 := Hash("https://example.com", "alice", "s3cret")
	h2 := Hash("https://example.com", "alice", "different")
	if h1 == h2 {
		t.Fatal("different passwords produced the same hash")
	}
}

func TestHash_UsesNormalizedNotRawURL(t *testing.T) {
	rawURL := "HTTP://Example.COM:80/"
	normalizedURL := "http://example.com"

	hashOfRaw := Hash(rawURL, "alice", "s3cret")
	hashOfNormalized := Hash(normalizedURL, "alice", "s3cret")

	if hashOfRaw == hashOfNormalized {
		t.Fatal("raw and normalized URLs produced the same hash by coincidence; adjust the test fixture")
	}

	// The point of this test: callers must pass the *normalized* URL.
	// If a future change accidentally hashes the raw extracted URL
	// instead, this is the test that would need to be looking at the
	// actual pipeline wiring (Group 8) to catch it - this test only
	// confirms Hash itself treats its input literally, with no
	// normalization inside Hash itself.
	want := sha256.Sum256([]byte(normalizedURL + "|alice|s3cret"))
	wantHex := hex.EncodeToString(want[:])
	if hashOfNormalized != wantHex {
		t.Fatalf("got %q, want %q", hashOfNormalized, wantHex)
	}
}

func TestAssemble_FieldsMappedCorrectly(t *testing.T) {
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	r := Assemble("alice", "s3cret", "https://example.com", "example.com", "www", "com", "8080", modified, created)

	if r.RecordHash != Hash("https://example.com", "alice", "s3cret") {
		t.Error("RecordHash does not match Hash(url, username, password)")
	}
	if r.Username != "alice" || r.Password != "s3cret" {
		t.Error("username/password not mapped correctly")
	}
	if r.URL != "https://example.com" || r.Domain != "example.com" || r.Subdomain != "www" || r.Suffix != "com" || r.Port != "8080" {
		t.Errorf("got %+v, fields not mapped correctly", r)
	}
	if !r.ModifiedDate.Equal(modified) || !r.CreatedAt.Equal(created) {
		t.Error("timestamps not mapped correctly")
	}
}
