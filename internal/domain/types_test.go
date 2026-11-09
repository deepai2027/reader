package domain

import "testing"

// TestFileStatusValues guards against an accidental rename of the
// FileStatus constants. Their string values are a persistence
// contract (they will become literal values stored in PostgreSQL's
// `status` column in the Final Phase and are already specified
// verbatim in the README), so a silent rename here would be a breaking
// change disguised as a refactor.
func TestFileStatusValues(t *testing.T) {
	cases := map[FileStatus]string{
		FileStatusReading: "reading",
		FileStatusDone:    "done",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("FileStatus %v = %q, want %q", status, string(status), want)
		}
	}
}
