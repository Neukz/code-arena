package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Neukz/code-arena/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

// JobPublisher is the interface the API gateway uses to enqueue submission jobs.
type JobPublisher interface {
	Publish(ctx context.Context, job models.SubmissionJob) error
}

// StubPublisher is a no-op publisher used before the real RabbitMQ publisher is wired.
// It optionally returns a configured error to test failure paths.
type StubPublisher struct {
	Err error
}

// Publish satisfies the JobPublisher interface.
func (p *StubPublisher) Publish(ctx context.Context, job models.SubmissionJob) error {
	return p.Err
}

// Publisher enqueues SubmissionJob messages to RabbitMQ with publisher confirms.
type Publisher struct {
	channel    *amqp.Channel
	exchange   string
	routingKey string
}

// NewPublisher creates a publisher from an existing AMQP connection. It opens a
// dedicated channel, enables publisher confirms, and declares the required topology.
func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}

	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}

	if err := DeclareTopology(ch); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("declare topology: %w", err)
	}

	return &Publisher{
		channel:    ch,
		exchange:   ExchangeName,
		routingKey: RoutingKey,
	}, nil
}

// Publish marshals the job to JSON, publishes it to the jobs exchange, and waits
// for a broker confirm or context cancellation. It returns an error if the publish
// fails or the broker negatively acknowledges the message.
func (p *Publisher) Publish(ctx context.Context, job models.SubmissionJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	conf, err := p.channel.PublishWithDeferredConfirm(
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	ack, err := conf.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ack {
		return fmt.Errorf("publish nacked")
	}
	return nil
}

// Close closes the underlying AMQP channel.
func (p *Publisher) Close() error {
	return p.channel.Close()
}
