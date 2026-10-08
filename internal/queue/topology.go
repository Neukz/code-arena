package queue

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeName  = "jobs"
	QueueName     = "jobs"
	DLQQueueName  = "jobs.dlq"
	RoutingKey    = "jobs"
	DLQRoutingKey = "jobs.dlq"
)

// DeclareTopology creates the jobs exchange, the jobs queue, and the jobs.dlq
// dead-letter queue if they do not already exist. It is safe to call multiple
// times because all declarations are idempotent.
func DeclareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(
		ExchangeName,
		"direct",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); err != nil {
		return fmt.Errorf("declare exchange %q: %w", ExchangeName, err)
	}

	if _, err := ch.QueueDeclare(
		DLQQueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	); err != nil {
		return fmt.Errorf("declare dlq %q: %w", DLQQueueName, err)
	}

	args := amqp.Table{
		"x-dead-letter-exchange":    ExchangeName,
		"x-dead-letter-routing-key": DLQRoutingKey,
	}
	if _, err := ch.QueueDeclare(
		QueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		args,
	); err != nil {
		return fmt.Errorf("declare queue %q: %w", QueueName, err)
	}

	if err := ch.QueueBind(
		QueueName,
		RoutingKey,
		ExchangeName,
		false, // noWait
		nil,
	); err != nil {
		return fmt.Errorf("bind queue %q to exchange %q: %w", QueueName, ExchangeName, err)
	}

	if err := ch.QueueBind(
		DLQQueueName,
		DLQRoutingKey,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind dlq %q to exchange %q: %w", DLQQueueName, ExchangeName, err)
	}

	return nil
}
