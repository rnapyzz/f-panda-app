-- name: GetUserByEmail :one
SELECT id, email, name, role, department_id, password_hash, is_active, created_at, updated_at
FROM app_user
WHERE email = ? AND is_active = TRUE;

-- name: GetUserByID :one
SELECT id, email, name, role, department_id, password_hash, is_active, created_at, updated_at
FROM app_user
WHERE id = ? AND is_active = TRUE;

-- name: CreateUser :execlastid
INSERT INTO app_user (email, name, role, department_id, password_hash)
VALUES (?, ?, ?, ?, ?);

-- name: ListUsers :many
SELECT id, email, name, role, department_id, is_active, created_at, updated_at
FROM app_user
ORDER BY name;

-- name: UpdateUser :exec
UPDATE app_user SET name = ?, role = ?, department_id = ?, is_active = ? WHERE id = ?;

-- name: UpdateUserPassword :exec
UPDATE app_user SET password_hash = ? WHERE id = ?;
