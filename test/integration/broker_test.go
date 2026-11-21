//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yourorg/reader/internal/delivery/local"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
	"github.com/yourorg/reader/internal/rabbitmq"
)

type publishAttempt struct {
	rabbitmq.Channel
	before func() error
}

func (p publishAttempt) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	if err := p.before(); err != nil {
		return err
	}
	return p.Channel.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg)
}

func checkBrokerDelivery(t *testing.T, ctx context.Context, url string, observer *amqp.Channel) {
	const queue = "test.delivery"
	stream := rabbitmq.NewConfirmingChannel(url)
	defer stream.Close()
	topics := rabbitmq.Topics{FullRecord: queue}
	record := domain.ValidatedRecord{RecordHash: "test-retry"}
	sink := rabbitmq.NewPublisher(stream, topics)
	if err := local.New(sink, 2, 1).PublishRecord(ctx, record); !errors.Is(err, ports.ErrTransient) {
		t.Fatalf("missing queue should exhaust retries: %v", err)
	}
	attempts := 0
	recovering := rabbitmq.NewPublisher(publishAttempt{Channel: stream, before: func() error {
		attempts++
		if attempts == 2 {
			// Simulate recovery after a real broker rejection and force a
			// reconnect between attempts.
			stream.Close()
			_, err := observer.QueueDeclare(queue, true, false, false, false, nil)
			return err
		}
		return nil
	}}, topics)
	if err := local.New(recovering, 3, 1).PublishRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("accepted delivery after %d attempts, want 2", attempts)
	}
	if _, found, err := observer.Get(queue, true); err != nil || !found {
		t.Fatalf("confirmed message absent: %v", err)
	}
	checkTransportRecovery(t, ctx, url, observer, queue)
}

func checkTransportRecovery(t *testing.T, ctx context.Context, brokerURL string, observer *amqp.Channel, queue string) {
	t.Helper()
	broker, err := url.Parse(brokerURL)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	upstream := net.JoinHostPort(broker.Hostname(), broker.Port())
	connections := make(chan net.Conn, 4)
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", upstream, time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			connections <- client
			go forward(client, server)
			go forward(server, client)
		}
	}()
	broker.Host = listener.Addr().String()
	stream := rabbitmq.NewConfirmingChannel(broker.String())
	defer stream.Close()
	publisher := local.New(rabbitmq.NewPublisher(stream, rabbitmq.Topics{FullRecord: queue}), 3, 20)
	record := domain.ValidatedRecord{RecordHash: "test-connection"}
	if err := publisher.PublishRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	first := <-connections
	// Terminate a live transport without asking the AMQP client to close it.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connections:
	case <-ctx.Done():
		t.Fatal("publisher did not open a replacement transport")
	}
	for i := 0; i < 2; i++ {
		if _, found, err := observer.Get(queue, true); err != nil || !found {
			t.Fatalf("confirmed message %d absent after reconnect: %v", i+1, err)
		}
	}
}

func forward(dst, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}
