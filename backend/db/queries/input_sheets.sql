-- name: CreateInputSheet :execlastid
INSERT INTO input_sheet (owner_user_id, name, sheet_snapshot)
VALUES (?, ?, ?);

-- name: UpdateInputSheetSnapshot :exec
UPDATE input_sheet
SET sheet_snapshot = ?
WHERE id = ? AND owner_user_id = ?;

-- name: GetInputSheet :one
SELECT id, owner_user_id, name, sheet_snapshot, created_at, updated_at
FROM input_sheet
WHERE id = ?;

-- name: ListInputSheetsByOwner :many
SELECT id, owner_user_id, name, created_at, updated_at
FROM input_sheet
WHERE owner_user_id = ?
ORDER BY updated_at DESC;
