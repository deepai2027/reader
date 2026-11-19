package uniqueurl

import (
	"errors"
	"strings"

	"github.com/yourorg/reader/internal/domain"
)

const (
	maxMemoryURLs     = 8192
	maxMemoryURLBytes = 4 << 20
)

type Dedup struct {
	priority  int
	seen      map[string]struct{}
	seenBytes int
	disk      *diskSet
}

// New returns a Dedup scoped to one run, carrying the file's priority
// onto every message it emits. The caller must close it.
func New(priority int) *Dedup {
	return &Dedup{priority: priority, seen: make(map[string]struct{})}
}

func (d *Dedup) Offer(normalizedURL, domainPart, subdomain, suffix string) (domain.UniqueURLMessage, bool, error) {
	added, err := d.add(normalizedURL)
	if err != nil || !added {
		return domain.UniqueURLMessage{}, false, err
	}
	return domain.UniqueURLMessage{
		URL:       normalizedURL,
		Priority:  d.priority,
		Domain:    domainPart,
		Subdomain: subdomain,
		Suffix:    suffix,
	}, true, nil
}

func (d *Dedup) add(url string) (bool, error) {
	if d.disk != nil {
		return d.disk.add(url)
	}
	if _, exists := d.seen[url]; exists {
		return false, nil
	}
	if len(d.seen) < maxMemoryURLs && d.seenBytes+len(url) <= maxMemoryURLBytes {
		d.seen[strings.Clone(url)] = struct{}{}
		d.seenBytes += len(url)
		return true, nil
	}

	disk, err := newDiskSet()
	if err != nil {
		return false, err
	}
	for key := range d.seen {
		if _, err := disk.add(key); err != nil {
			return false, errors.Join(err, disk.close())
		}
	}
	d.disk = disk
	d.seen = nil
	return d.disk.add(url)
}

func (d *Dedup) Close() error {
	if d.disk == nil {
		return nil
	}
	err := d.disk.close()
	d.disk = nil
	return err
}
