-- name: ListLanguages :many
SELECT * FROM languages
ORDER BY display_name;

-- name: GetLanguageByID :one
SELECT * FROM languages
WHERE id = $1;
