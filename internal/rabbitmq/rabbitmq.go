// Package rabbitmq provides the real, RabbitMQ-backed
// ports.MessagePublisher adapter, replacing
// internal/ports/stub.MessagePublisher and the MVP Group 3 local
// retrying adapter's sink. See specs/2026-11-10-mvp/plan.md Group 8.
// Built on rabbitmq/amqp091-go, per specs/tech-stack.md.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports"
)

// Topics names the two queues this publisher writes to, by routing
// key. Exact naming should be confirmed against whatever the
// downstream consumers (out of this project's scope) actually expect -
// these are this implementation's best reading of README section 9,
// not a value confirmed against a real deployment.
type Topics struct {
	FullRecord string
	UniqueURL  string
}

// Channel is the minimal subset of *amqp.Channel this adapter needs,
// so it can be tested against a fake without a real broker.
type Channel interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
}

// Publisher is the RabbitMQ-backed ports.MessagePublisher adapter.
//
// NewPublisher expects an already-open, already-configured channel:
// queue declaration (including the x-max-priority argument matching
// the Agent's priority range, per specs/tech-stack.md) is a one-time
// startup/provisioning concern handled by the caller, not per-message
// here.
type Publisher struct {
	channel Channel
	topics  Topics
}

// NewPublisher wraps channel as a ports.MessagePublisher, routing
// records and unique-URL messages to topics.FullRecord and
// topics.UniqueURL respectively.
func NewPublisher(channel Channel, topics Topics) *Publisher {
	return &Publisher{channel: channel, topics: topics}
}

func (p *Publisher) PublishRecord(ctx context.Context, r domain.ValidatedRecord) error {
	return p.publish(ctx, p.topics.FullRecord, 0, r)
}

func (p *Publisher) PublishUniqueURL(ctx context.Context, m domain.UniqueURLMessage) error {
	if m.Priority < 0 || m.Priority > 255 {
		return fmt.Errorf("rabbitmq: priority %d outside AMQP range: %w", m.Priority, ports.ErrPermanent)
	}
	return p.publish(ctx, p.topics.UniqueURL, uint8(m.Priority), m)
}

func (p *Publisher) publish(ctx context.Context, routingKey string, priority uint8, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		// A marshal failure on our own well-typed structs indicates a
		// programming error, not a transient broker problem.
		return fmt.Errorf("rabbitmq: marshaling message: %w: %w", ports.ErrPermanent, err)
	}

	err = p.channel.PublishWithContext(ctx, "", routingKey, true, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Priority:     priority,
	})
	if err != nil {
		// Any broker/connection-level error is treated as transient -
		// the local retry wrapper (internal/delivery/local, Group 3)
		// retries per config.RetryMaxAttempts before surfacing a hard
		// failure.
		return fmt.Errorf("rabbitmq: publishing to %q: %w: %w", routingKey, ports.ErrTransient, err)
	}
	return nil
}

// ConfirmingChannel owns the connection used by a synchronous publisher. A
// retry after a transport failure establishes a fresh connection.
type ConfirmingChannel struct {
	url     string
	conn    *amqp.Connection
	channel *amqp.Channel
	returns <-chan amqp.Return
}

func NewConfirmingChannel(url string) *ConfirmingChannel {
	return &ConfirmingChannel{url: url}
}

func (c *ConfirmingChannel) Close() {
	if c.conn != nil {
		_ = c.conn.CloseDeadline(time.Now().Add(time.Second))
		c.conn, c.channel = nil, nil
	}
}

func (c *ConfirmingChannel) connect() error {
	if c.channel != nil && !c.channel.IsClosed() && !c.conn.IsClosed() {
		return nil
	}
	c.Close()
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return err
	}
	c.conn = conn
	channel, err := conn.Channel()
	if err != nil {
		c.Close()
		return err
	}
	if err := channel.Confirm(false); err != nil {
		c.Close()
		return err
	}
	c.channel = channel
	c.returns = channel.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}

func (c *ConfirmingChannel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.connect(); err != nil {
		return err
	}
	confirmation, err := c.channel.PublishWithDeferredConfirm(exchange, key, mandatory, immediate, msg)
	if err != nil {
		c.Close()
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	acked, err := confirmation.WaitContext(waitCtx)
	if err != nil || !acked {
		c.Close()
		if err != nil {
			return err
		}
		return fmt.Errorf("broker did not confirm delivery")
	}
	// RabbitMQ sends basic.return before the confirmation for an unroutable
	// mandatory message. This channel has only one outstanding publish.
	select {
	case returned, open := <-c.returns:
		if open {
			return fmt.Errorf("broker returned message: %s", returned.ReplyText)
		}
	default:
	}
	return nil
}
