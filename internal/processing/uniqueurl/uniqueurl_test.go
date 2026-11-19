package uniqueurl

import (
	"fmt"
	"os"
	"testing"

	"github.com/yourorg/reader/internal/domain"
)

func offer(t *testing.T, d *Dedup, url, domainPart, subdomain, suffix string) (domain.UniqueURLMessage, bool) {
	t.Helper()
	msg, added, err := d.Offer(url, domainPart, subdomain, suffix)
	if err != nil {
		t.Fatal(err)
	}
	return msg, added
}

func TestOffer_SameURLTwiceEmitsOnce(t *testing.T) {
	d := New(5)

	_, firstOK := offer(t, d, "https://example.com", "example.com", "", "com")
	if !firstOK {
		t.Fatal("first occurrence should be emitted")
	}

	_, secondOK := offer(t, d, "https://example.com", "example.com", "", "com")
	if secondOK {
		t.Fatal("second occurrence of the same URL should not be emitted")
	}
}

func TestOffer_DifferentURLsEmitBoth(t *testing.T) {
	d := New(5)

	msg1, ok1 := offer(t, d, "https://a.example.com", "example.com", "a", "com")
	msg2, ok2 := offer(t, d, "https://b.example.com", "example.com", "b", "com")

	if !ok1 || !ok2 {
		t.Fatal("both distinct URLs should be emitted")
	}
	if msg1.URL == msg2.URL {
		t.Fatal("expected distinct URLs in the emitted messages")
	}
}

func TestOffer_PriorityAttachedToEveryMessage(t *testing.T) {
	d := New(9)
	msg, ok := offer(t, d, "https://example.com", "example.com", "", "com")
	if !ok {
		t.Fatal("expected first occurrence to be emitted")
	}
	if msg.Priority != 9 {
		t.Errorf("Priority = %d, want 9", msg.Priority)
	}
}

func TestOffer_DedupScopedToOneInstance(t *testing.T) {
	// Two separate Dedups (as Phase 8's orchestration constructs per
	// file) must not share state - this guards against a future
	// refactor accidentally introducing shared/global dedup state.
	d1 := New(1)
	d2 := New(1)

	_, ok1 := offer(t, d1, "https://example.com", "example.com", "", "com")
	_, ok2 := offer(t, d2, "https://example.com", "example.com", "", "com")

	if !ok1 || !ok2 {
		t.Fatal("the same URL should be emitted as new in each separate Dedup instance")
	}
}

func TestOffer_ExactMembershipAfterSpill(t *testing.T) {
	d := New(11)
	for i := 0; i <= maxMemoryURLs+2048; i++ {
		url := fmt.Sprintf("https://example.test/%08d", i)
		if msg, added := offer(t, d, url, "example.test", "", "test"); !added || msg.URL != url || msg.Priority != 11 {
			t.Fatalf("unexpected result for %s: %+v, added=%t", url, msg, added)
		}
	}
	if d.disk == nil || d.disk.flushed == 0 {
		t.Fatal("expected flushed disk entries")
	}
	path := d.disk.file.Name()
	t.Cleanup(func() { _ = d.Close() })
	for i := 0; i <= maxMemoryURLs+2048; i++ {
		url := fmt.Sprintf("https://example.test/%08d", i)
		if _, added := offer(t, d, url, "", "", ""); added {
			t.Fatalf("re-emitted %s", url)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary index remains: %v", err)
	}
}

func TestOffer_SpillCreationFailure(t *testing.T) {
	missing := t.TempDir() + "/missing"
	t.Setenv("TMPDIR", missing)
	t.Setenv("TEMP", missing)
	t.Setenv("TMP", missing)
	d := New(0)
	for i := 0; i < maxMemoryURLs; i++ {
		offer(t, d, fmt.Sprint(i), "", "", "")
	}
	if _, added, err := d.Offer("requires-spill", "", "", ""); err == nil || added {
		t.Fatalf("added=%t err=%v, want a spill error", added, err)
	}
}
