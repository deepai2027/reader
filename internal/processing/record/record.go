// Package record computes the record hash and assembles the final
// domain.ValidatedRecord shape. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 6 and
// README section 8.
package record

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/yourorg/reader/internal/domain"
)

// Hash computes record_hash = sha256(url|username|password) using the
// already-normalized url. The delimiter and field order are a
// persistence/dedup contract downstream consumers rely on - do not
// change them without updating this doc comment, the test in
// record_test.go that pins the exact formula, and the spec.
func Hash(normalizedURL, username, password string) string {
	sum := sha256.Sum256([]byte(normalizedURL + "|" + username + "|" + password))
	return hex.EncodeToString(sum[:])
}

// Assemble builds the final domain.ValidatedRecord for a validated,
// normalized candidate.
//
// modifiedDate is a placeholder until the Final Phase's MinIO-backed
// FileSource can supply real object metadata - callers currently pass
// the pipeline run's own clock time. This is a known, documented gap
// (see plan.md Group 6), not an oversight.
func Assemble(username, password, normalizedURL, domainPart, subdomain, suffix, port string, modifiedDate, createdAt time.Time) domain.ValidatedRecord {
	return domain.ValidatedRecord{
		RecordHash:   Hash(normalizedURL, username, password),
		Username:     username,
		Password:     password,
		URL:          normalizedURL,
		Domain:       domainPart,
		Subdomain:    subdomain,
		Suffix:       suffix,
		Port:         port,
		ModifiedDate: modifiedDate,
		CreatedAt:    createdAt,
	}
}
