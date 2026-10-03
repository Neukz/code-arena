package queue

import (
	"context"

	"github.com/Neukz/code-arena/internal/models"
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
