package uniqueurl

import "github.com/yourorg/reader/internal/domain"

type Dedup struct {
	priority int
	seen     map[string]struct{}
}

// New returns a Dedup scoped to one file, carrying that file's
// priority onto every message it emits.
func New(priority int) *Dedup {
	return &Dedup{priority: priority, seen: make(map[string]struct{})}
}

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
