-- name: GetUserByEmail :one
SELECT id, email, name, role, password_hash, is_active, created_at, updated_at
FROM app_user
WHERE email = ? AND is_active = TRUE;

-- name: GetUserByID :one
SELECT id, email, name, role, password_hash, is_active, created_at, updated_at
FROM app_user
WHERE id = ? AND is_active = TRUE;

-- name: CreateUser :execlastid
INSERT INTO app_user (email, name, role, password_hash)
VALUES (?, ?, ?, ?);
