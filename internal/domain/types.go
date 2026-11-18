package domain

import "time"

type FileStatus string

const (
	FileStatusReading FileStatus = "reading"
	FileStatusDone FileStatus = "done"
)

type FileState struct {
	FileID          string
	Status          FileStatus
	CurrentLine     int64
	MatchedCount    int64
	SelectedRegexID *int64
	Priority        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RegexPattern struct {
	ID       int64
	Name     string
	Pattern  string
	IsActive bool
}

type RawLine struct {
	LineNumber int64
	Text       string
}

type ExtractedCandidate struct {
	LineNumber int64
	URL        string
	Username   string
	Password   string
}


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

type UniqueURLMessage struct {
	URL       string // normalized
	Priority  int    // inherited from the source file
	Domain    string
	Subdomain string
	Suffix    string
}
