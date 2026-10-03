-- name: ListProblems :many
SELECT id, slug, title, description, difficulty, created_at, updated_at
FROM problems
WHERE (sqlc.narg('difficulty')::problem_difficulty IS NULL OR difficulty = sqlc.narg('difficulty'))
ORDER BY created_at DESC;

-- name: GetProblemBySlug :one
SELECT id, slug, title, description, difficulty, created_at, updated_at
FROM problems
WHERE slug = $1;

-- name: GetProblemByID :one
SELECT id, slug, title, description, difficulty, created_at, updated_at
FROM problems
WHERE id = $1;
