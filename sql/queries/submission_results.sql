-- name: UpsertSubmissionResult :exec
INSERT INTO submission_results
    (submission_id, test_case_id, verdict, stdout, stderr, execution_time_ms, memory_used_mb)
VALUES
    ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (submission_id, test_case_id)
DO UPDATE SET
    verdict = EXCLUDED.verdict,
    stdout = EXCLUDED.stdout,
    stderr = EXCLUDED.stderr,
    execution_time_ms = EXCLUDED.execution_time_ms,
    memory_used_mb = EXCLUDED.memory_used_mb;

-- name: GetSubmissionResultsBySubmissionID :many
SELECT
    r.id,
    r.submission_id,
    r.test_case_id,
    r.verdict,
    r.stdout,
    r.stderr,
    r.execution_time_ms,
    r.memory_used_mb,
    r.created_at,
    t.name,
    t.is_hidden
FROM submission_results r
JOIN test_cases t ON r.test_case_id = t.id
WHERE r.submission_id = $1
ORDER BY t.display_order, t.created_at;
