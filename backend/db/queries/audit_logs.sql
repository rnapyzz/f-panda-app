-- name: CreateAuditLog :exec
INSERT INTO audit_log (user_id, action, entity_type, entity_id, detail, ip_address)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAuditLogs :many
SELECT a.id, a.user_id, u.name AS user_name, u.email AS user_email,
       a.action, a.entity_type, a.entity_id, a.detail, a.ip_address, a.created_at
FROM audit_log a
LEFT JOIN app_user u ON u.id = a.user_id
WHERE (sqlc.narg('user_id') IS NULL OR a.user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('action') IS NULL OR a.action = sqlc.narg('action'))
  AND (sqlc.narg('entity_type') IS NULL OR a.entity_type = sqlc.narg('entity_type'))
  AND (sqlc.narg('from_date') IS NULL OR a.created_at >= sqlc.narg('from_date'))
  AND (sqlc.narg('to_date') IS NULL OR a.created_at <= sqlc.narg('to_date'))
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?;
