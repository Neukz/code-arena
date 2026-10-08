package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"

	"github.com/Neukz/code-arena/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Handler processes a submission job delivered from the jobs queue.
// A non-nil error signals the job failed and should be retried or dead-lettered.
type Handler func(ctx context.Context, job models.SubmissionJob) error

// Consumer consumes SubmissionJob messages from the jobs queue with manual ack
// and a configurable prefetch count. Failed messages are retried up to
// MaxRetries using the x-retry-count header, then routed to jobs.dlq.
type Consumer struct {
	channel    *amqp.Channel
	handler    Handler
	log        *slog.Logger
	prefetch   int
	maxRetries int
	queue      string
}

// NewConsumer creates a consumer from an existing AMQP connection. It configures
// QoS/prefetch, declares the required topology, and prepares a manual-ack consumer.
func NewConsumer(conn *amqp.Connection, handler Handler, log *slog.Logger, prefetch, maxRetries int) (*Consumer, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}

	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("set qos: %w", err)
	}

	if err := DeclareTopology(ch); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("declare topology: %w", err)
	}

	return &Consumer{
		channel:    ch,
		handler:    handler,
		log:        log,
		prefetch:   prefetch,
		maxRetries: maxRetries,
		queue:      QueueName,
	}, nil
}

// Start begins consuming messages and blocks until ctx is cancelled or the
// delivery channel is closed. Each message is processed by the configured Handler.
func (c *Consumer) Start(ctx context.Context) error {
	msgs, err := c.channel.Consume(
		c.queue,
		"",    // consumer tag
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("start consume: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				return fmt.Errorf("delivery channel closed")
			}
			c.handle(msg)
		}
	}
}

func (c *Consumer) handle(msg amqp.Delivery) {
	var job models.SubmissionJob
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		c.log.Error("failed to unmarshal job", slog.String("error", err.Error()))
		_ = msg.Nack(false, false)
		return
	}

	if err := c.handler(context.Background(), job); err != nil {
		c.handleError(msg, err)
		return
	}

	if err := msg.Ack(false); err != nil {
		c.log.Error("failed to ack message", slog.String("error", err.Error()))
	}
}

func (c *Consumer) handleError(msg amqp.Delivery, handlerErr error) {
	retries := retryCount(msg.Headers)
	c.log.Error("job handler failed",
		slog.String("error", handlerErr.Error()),
		slog.Int("retry_count", retries),
	)

	if retries >= c.maxRetries {
		c.log.Error("max retries exceeded, sending message to dlq")
		_ = msg.Nack(false, false)
		return
	}

	headers := amqp.Table{}
	maps.Copy(headers, msg.Headers)
	headers[retryHeader] = retries + 1

	if err := c.channel.PublishWithContext(
		context.Background(),
		ExchangeName,
		RoutingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  msg.ContentType,
			DeliveryMode: msg.DeliveryMode,
			Headers:      headers,
			Body:         msg.Body,
		},
	); err != nil {
		c.log.Error("failed to republish for retry", slog.String("error", err.Error()))
		_ = msg.Nack(false, true)
		return
	}

	if err := msg.Ack(false); err != nil {
		c.log.Error("failed to ack retry message", slog.String("error", err.Error()))
	}
}

const retryHeader = "x-retry-count"

func retryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	v, ok := headers[retryHeader]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// Close closes the underlying AMQP channel.
func (c *Consumer) Close() error {
	return c.channel.Close()
}
