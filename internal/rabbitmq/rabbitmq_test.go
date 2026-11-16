package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
)

type fakeChannel struct {
	published []amqp.Publishing
	keys      []string
	err       error
}

func (c *fakeChannel) PublishWithContext(_ context.Context, _, key string, _, _ bool, msg amqp.Publishing) error {
	if c.err != nil {
		return c.err
	}
	c.published = append(c.published, msg)
	c.keys = append(c.keys, key)
	return nil
}

func TestPublishRecord_RoutesToFullRecordTopic(t *testing.T) {
	ch := &fakeChannel{}
	p := NewPublisher(ch, Topics{FullRecord: "full-record", UniqueURL: "unique-url"})

	if err := p.PublishRecord(context.Background(), domain.ValidatedRecord{RecordHash: "abc"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ch.keys) != 1 || ch.keys[0] != "full-record" {
		t.Fatalf("got keys %v, want [\"full-record\"]", ch.keys)
	}

	var decoded domain.ValidatedRecord
	if err := json.Unmarshal(ch.published[0].Body, &decoded); err != nil {
		t.Fatalf("failed to decode published body: %v", err)
	}
	if decoded.RecordHash != "abc" {
		t.Errorf("RecordHash = %q, want %q", decoded.RecordHash, "abc")
	}
}

func TestPublishUniqueURL_RoutesToUniqueURLTopic(t *testing.T) {
	ch := &fakeChannel{}
	p := NewPublisher(ch, Topics{FullRecord: "full-record", UniqueURL: "unique-url"})

	if err := p.PublishUniqueURL(context.Background(), domain.UniqueURLMessage{URL: "https://example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ch.keys) != 1 || ch.keys[0] != "unique-url" {
		t.Fatalf("got keys %v, want [\"unique-url\"]", ch.keys)
	}
}

func TestPublish_ChannelErrorIsTransient(t *testing.T) {
	ch := &fakeChannel{err: errors.New("connection reset")}
	p := NewPublisher(ch, Topics{FullRecord: "full-record", UniqueURL: "unique-url"})

	err := p.PublishRecord(context.Background(), domain.ValidatedRecord{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ports.ErrTransient) {
		t.Errorf("expected the error to wrap ports.ErrTransient, got %v", err)
	}
}

func TestPublish_UniqueURLMessageNeverCarriesCredentials(t *testing.T) {
	// domain.UniqueURLMessage has no username/password field by
	// construction, so marshaling it can never leak one - this test
	// pins that by decoding the published JSON and confirming no such
	// keys exist, rather than relying on the struct definition alone.
	ch := &fakeChannel{}
	p := NewPublisher(ch, Topics{FullRecord: "full-record", UniqueURL: "unique-url"})

	if err := p.PublishUniqueURL(context.Background(), domain.UniqueURLMessage{URL: "https://example.com", Priority: 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(ch.published[0].Body, &raw); err != nil {
		t.Fatalf("failed to decode published body: %v", err)
	}
	for _, forbidden := range []string{"username", "password", "Username", "Password"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("published unique-url message unexpectedly contains key %q", forbidden)
		}
	}
}
