package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/queue"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var projectRoot string

func init() {
	_, b, _, _ := runtime.Caller(0)
	projectRoot = filepath.Join(filepath.Dir(b), "..", "..")

	if os.Getenv("DOCKER_HOST") == "" {
		for _, sock := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
			if _, err := os.Stat(sock); err == nil {
				_ = os.Setenv("DOCKER_HOST", "unix://"+sock)
				break
			}
		}
	}
}

func closeWithErrorCheck(t *testing.T, closer interface{ Close() error }) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func startRabbitMQ(t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "rabbitmq:4.3-management-alpine",
		ExposedPorts: []string{"5672/tcp"},
		WaitingFor:   wait.ForListeningPort("5672/tcp"),
		Env: map[string]string{
			"RABBITMQ_DEFAULT_USER": "codearena",
			"RABBITMQ_DEFAULT_PASS": "codearena",
		},
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start rabbitmq container: %v", err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to get container host: %v", err)
	}

	port, err := ctr.MappedPort(ctx, "5672")
	if err != nil {
		_ = ctr.Terminate(ctx)
		t.Fatalf("failed to get mapped port: %v", err)
	}

	url := fmt.Sprintf("amqp://codearena:codearena@%s:%s/", host, port.Port())
	return ctr, url
}

func TestPublisherRoundTrip(t *testing.T) {
	ctr, url := startRabbitMQ(t)
	defer func() { _ = ctr.Terminate(context.Background()) }()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("failed to dial rabbitmq: %v", err)
	}
	defer closeWithErrorCheck(t, conn)

	pub, err := queue.NewPublisher(conn)
	if err != nil {
		t.Fatalf("failed to create publisher: %v", err)
	}
	defer closeWithErrorCheck(t, pub)

	job := models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a12",
		LanguageID:   "python",
		SourceCode:   "print('hello')",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, job); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer closeWithErrorCheck(t, ch)

	msgs, err := ch.Consume(queue.QueueName, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case msg := <-msgs:
		var received models.SubmissionJob
		if err := json.Unmarshal(msg.Body, &received); err != nil {
			t.Fatalf("unmarshal message body: %v", err)
		}
		if received != job {
			t.Errorf("received job does not match: got %+v, want %+v", received, job)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublisherDLQRouting(t *testing.T) {
	ctr, url := startRabbitMQ(t)
	defer func() { _ = ctr.Terminate(context.Background()) }()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("failed to dial rabbitmq: %v", err)
	}
	defer closeWithErrorCheck(t, conn)

	pub, err := queue.NewPublisher(conn)
	if err != nil {
		t.Fatalf("failed to create publisher: %v", err)
	}
	defer closeWithErrorCheck(t, pub)

	job := models.SubmissionJob{
		SubmissionID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a21",
		ProblemID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a22",
		LanguageID:   "go",
		SourceCode:   "package main\nfunc main() {}",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, job); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer closeWithErrorCheck(t, ch)

	msgs, err := ch.Consume(queue.QueueName, "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case msg := <-msgs:
		if err := msg.Nack(false, false); err != nil {
			t.Fatalf("nack message: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message to nack")
	}

	dlqCh, err := conn.Channel()
	if err != nil {
		t.Fatalf("open dlq channel: %v", err)
	}
	defer closeWithErrorCheck(t, dlqCh)

	dlqMsgs, err := dlqCh.Consume(queue.DLQQueueName, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume dlq: %v", err)
	}

	select {
	case msg := <-dlqMsgs:
		var received models.SubmissionJob
		if err := json.Unmarshal(msg.Body, &received); err != nil {
			t.Fatalf("unmarshal dlq message body: %v", err)
		}
		if received.SubmissionID != job.SubmissionID {
			t.Errorf("dlq message submission_id mismatch: got %s, want %s", received.SubmissionID, job.SubmissionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dlq message")
	}
}
