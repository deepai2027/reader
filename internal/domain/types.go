package domain

import "time"

type FileStatus string

const (
	FileStatusReading FileStatus = "reading"
	FileStatusDone    FileStatus = "done"
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
	RecordHash   string    `json:"record_hash"`
	Username     string    `json:"username"`
	Password     string    `json:"password"`
	URL          string    `json:"url"`
	Domain       string    `json:"domain"`
	Subdomain    string    `json:"subdomain"`
	Suffix       string    `json:"suffix"`
	Port         string    `json:"port"`
	ModifiedDate time.Time `json:"modified_date"`
	CreatedAt    time.Time `json:"created_at"`
}

type UniqueURLMessage struct {
	URL       string `json:"url"`
	Priority  int    `json:"priority"`
	Domain    string `json:"domain"`
	Subdomain string `json:"subdomain"`
	Suffix    string `json:"suffix"`
}
