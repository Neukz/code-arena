-- name: Ping :one
SELECT 1;

-- name: CreateSubmission :one
INSERT INTO submissions (problem_id, language_id, source_code)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSubmissionByID :one
SELECT * FROM submissions
WHERE id = $1;

-- name: MarkSubmissionRunning :exec
UPDATE submissions
SET status = 'running', updated_at = now()
WHERE id = $1 AND status = 'pending';

-- name: MarkSubmissionCompleted :exec
UPDATE submissions
SET status = 'completed', overall_verdict = $2, completed_at = now(), updated_at = now()
WHERE id = $1 AND status != 'completed';

-- name: MarkSubmissionFailed :exec
UPDATE submissions
SET status = 'failed', error_message = $2, completed_at = now(), updated_at = now()
WHERE id = $1 AND status != 'completed';
