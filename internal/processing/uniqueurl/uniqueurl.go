// Package uniqueurl deduplicates normalized URLs within a single
// file's processing run and produces the Unique URL topic's message
// shape. See specs/2026-11-09-core-file-processing-pipeline/plan.md
// Group 7 and specs/mission.md's data-safety principle.
package uniqueurl

import "github.com/yourorg/reader/internal/domain"

// Dedup tracks normalized URLs seen during a single file's processing
// run and emits a domain.UniqueURLMessage only for the first
// occurrence of each one. A Dedup must not be reused across different
// files - construct a new one per file, scoped to that file's run.
type Dedup struct {
	priority int
	seen     map[string]struct{}
}

// New returns a Dedup scoped to one file, carrying that file's
// priority onto every message it emits.
func New(priority int) *Dedup {
	return &Dedup{priority: priority, seen: make(map[string]struct{})}
}

// Offer records normalizedURL and returns (message, true) the first
// time a given normalizedURL is seen in this Dedup's lifetime, or
// (zero value, false) on every subsequent occurrence.
//
// domain.UniqueURLMessage intentionally has no username/password
// field. This function must never be changed to populate such a
// field, even if one is ever added to the struct - per
// specs/mission.md, credentials must never reach the Unique URL
// topic.
func (d *Dedup) Offer(normalizedURL, domainPart, subdomain, suffix string) (domain.UniqueURLMessage, bool) {
	if _, exists := d.seen[normalizedURL]; exists {
		return domain.UniqueURLMessage{}, false
	}
	d.seen[normalizedURL] = struct{}{}
	return domain.UniqueURLMessage{
		URL:       normalizedURL,
		Priority:  d.priority,
		Domain:    domainPart,
		Subdomain: subdomain,
		Suffix:    suffix,
	}, true
}
