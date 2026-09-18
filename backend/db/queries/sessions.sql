-- name: CreateSession :exec
INSERT INTO user_session (token_hash, user_id, expires_at, ip_address, user_agent)
VALUES (?, ?, ?, ?, ?);

-- name: GetSessionByTokenHash :one
SELECT token_hash, user_id, created_at, expires_at, last_seen_at, ip_address, user_agent
FROM user_session
WHERE token_hash = ? AND expires_at > NOW();

-- name: TouchSession :exec
UPDATE user_session
SET last_seen_at = NOW()
WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM user_session
WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM user_session
WHERE expires_at <= NOW();
