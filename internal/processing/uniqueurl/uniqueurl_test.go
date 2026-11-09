package uniqueurl

import "testing"

func TestOffer_SameURLTwiceEmitsOnce(t *testing.T) {
	d := New(5)

	_, firstOK := d.Offer("https://example.com", "example.com", "", "com")
	if !firstOK {
		t.Fatal("first occurrence should be emitted")
	}

	_, secondOK := d.Offer("https://example.com", "example.com", "", "com")
	if secondOK {
		t.Fatal("second occurrence of the same URL should not be emitted")
	}
}

func TestOffer_DifferentURLsEmitBoth(t *testing.T) {
	d := New(5)

	msg1, ok1 := d.Offer("https://a.example.com", "example.com", "a", "com")
	msg2, ok2 := d.Offer("https://b.example.com", "example.com", "b", "com")

	if !ok1 || !ok2 {
		t.Fatal("both distinct URLs should be emitted")
	}
	if msg1.URL == msg2.URL {
		t.Fatal("expected distinct URLs in the emitted messages")
	}
}

func TestOffer_PriorityAttachedToEveryMessage(t *testing.T) {
	d := New(9)
	msg, ok := d.Offer("https://example.com", "example.com", "", "com")
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

	_, ok1 := d1.Offer("https://example.com", "example.com", "", "com")
	_, ok2 := d2.Offer("https://example.com", "example.com", "", "com")

	if !ok1 || !ok2 {
		t.Fatal("the same URL should be emitted as new in each separate Dedup instance")
	}
}
