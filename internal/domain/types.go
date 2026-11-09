// Package domain contains Reader's core data types — the shapes that
// flow through the file-processing pipeline. These types are
// infrastructure-agnostic: they know nothing about PostgreSQL, MinIO,
// RabbitMQ, or the Agent API. Phase 1+ logic operates on these types;
// the Final Phase infrastructure adapters translate to and from them.
package domain

import "time"

// FileStatus represents where a file is in its processing lifecycle.
type FileStatus string

const (
	// FileStatusReading indicates the file is currently being
	// processed, either for the first time or resumed after a restart.
	FileStatusReading FileStatus = "reading"
	// FileStatusDone indicates processing has completed, either
	// successfully or because no Regex matched the file's first 5,000
	// lines (README §4.2).
	FileStatusDone FileStatus = "done"
)

// FileState captures everything needed to process a file and to resume
// processing it after a restart. It mirrors the eventual
// `file_management` table (README §1.2) but has no persistence
// awareness of its own — that's ports.ProgressStore's job.
type FileState struct {
	FileID          string
	Status          FileStatus
	CurrentLine     int64
	MatchedCount    int64
	SelectedRegexID *int64 // nil until a Regex has been selected for this file
	Priority        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RegexPattern is a predefined pattern used to identify and extract
// information from a file (README §1.1). Pattern is kept as a plain
// string rather than a compiled *regexp.Regexp so this type stays
// independent of whichever regex engine Phase 1 ultimately uses
// (stdlib `regexp` vs. `regexp2` — see specs/tech-stack.md, "Regex
// Engine — Important Open Risk").
type RegexPattern struct {
	ID       int64
	Name     string
	Pattern  string
	IsActive bool
}

// RawLine is one line read from a file during batch processing, paired
// with its 1-based line number so progress can be checkpointed
// precisely (README §5).
type RawLine struct {
	LineNumber int64
	Text       string
}

// ExtractedCandidate is the unvalidated result of applying a selected
// RegexPattern to a RawLine. It may fail validation (README §7.2–7.3)
// and never become a ValidatedRecord.
type ExtractedCandidate struct {
	LineNumber int64
	URL        string
	Username   string
	Password   string
}

// ValidatedRecord is a fully extracted, validated, normalized record,
// ready to be published to the Full Record RabbitMQ topic (README
// §8, §9.1). Every field here is final — no further transformation
// happens downstream.
type ValidatedRecord struct {
	RecordHash   string
	Username     string
	Password     string
	URL          string // normalized
	Domain       string
	Subdomain    string // empty if none
	Suffix       string
	Port         string // empty if none
	ModifiedDate time.Time // file's last-modified time, from MinIO metadata
	CreatedAt    time.Time
}

// UniqueURLMessage is a deduplicated URL ready to be published to the
// Unique URL RabbitMQ topic (README §9.2). It deliberately excludes
// Username and Password — per specs/mission.md's data-safety
// principle, credentials must never reach this topic.
type UniqueURLMessage struct {
	URL       string // normalized
	Priority  int    // inherited from the source file (README §9.2)
	Domain    string
	Subdomain string
	Suffix    string
}
