package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Neukz/code-arena/internal/db"
	"github.com/Neukz/code-arena/internal/models"
	"github.com/Neukz/code-arena/internal/queue"
	"github.com/jackc/pgx/v5/pgtype"
)

// Recover returns a queue.Handler that recovers from panics in the wrapped handler.
// On panic, it attempts to mark the submission as failed and returns an error so
// the message is negatively acknowledged (and eventually dead-lettered).
func Recover(handler queue.Handler, queries Repository, log *slog.Logger) queue.Handler {
	return func(ctx context.Context, job models.SubmissionJob) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered while processing job",
					slog.String("submission_id", job.SubmissionID),
					slog.Any("recover", r),
				)
				err = fmt.Errorf("panic: %v", r)

				if id, parseErr := parseUUID(job.SubmissionID); parseErr == nil {
					if markErr := queries.MarkSubmissionFailed(ctx, db.MarkSubmissionFailedParams{
						ID:           id,
						ErrorMessage: pgtype.Text{String: err.Error(), Valid: true},
					}); markErr != nil {
						log.Error("failed to mark submission failed after panic",
							slog.String("submission_id", job.SubmissionID),
							slog.String("error", markErr.Error()),
						)
					}
				}
			}
		}()
		return handler(ctx, job)
	}
}
