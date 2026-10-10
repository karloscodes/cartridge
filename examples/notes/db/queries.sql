-- Every query of the app is here. sqlc checks each one against the
-- migrations and writes its Go function: run `mise run generate` after a
-- change. Scope each query on notes by the user.

-- name: UserByEmail :one
SELECT id, email, password_hash FROM users WHERE email = ?;

-- name: CountUsersByID :one
SELECT COUNT(*) FROM users WHERE id = ?;

-- name: CreateUser :exec
INSERT INTO users (email, password_hash) VALUES (?, ?)
ON CONFLICT (email) DO NOTHING;

-- name: NotesOfUser :many
SELECT id, user_id, body, created_at FROM notes
WHERE user_id = ?
ORDER BY created_at DESC, id DESC;

-- name: CountNotes :one
SELECT COUNT(*) FROM notes;

-- name: CreateNote :exec
INSERT INTO notes (user_id, body) VALUES (?, ?);
