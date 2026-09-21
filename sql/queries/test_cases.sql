-- name: GetSampleTestCasesByProblemID :many
SELECT id, problem_id, name, input, expected_output, is_hidden, display_order, created_at
FROM test_cases
WHERE problem_id = $1 AND is_hidden = false
ORDER BY display_order, created_at;

-- name: GetTestCasesByProblemID :many
SELECT id, problem_id, name, input, expected_output, is_hidden, display_order, created_at
FROM test_cases
WHERE problem_id = $1
ORDER BY display_order, created_at;
