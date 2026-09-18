-- name: ListActiveUserAssignments :many
SELECT ua.id, ua.user_id, u.name AS user_name, u.email AS user_email,
       ua.business_id, b.code AS business_code, b.name AS business_name,
       ua.department_id, d.code AS department_code, d.name AS department_name
FROM user_assignment ua
JOIN app_user u ON u.id = ua.user_id
JOIN dim_business b ON b.id = ua.business_id
JOIN dim_department d ON d.id = ua.department_id
WHERE ua.is_active = TRUE
ORDER BY b.code, d.code, u.name;

-- name: CreateUserAssignment :execlastid
-- ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id) lets a previously
-- deactivated assignment be reactivated by the same (user, business,
-- department) triple: a plain "SET is_active = TRUE" without the
-- LAST_INSERT_ID(id) trick would make execlastid report 0 instead of the
-- existing row's id.
INSERT INTO user_assignment (user_id, business_id, department_id)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), is_active = TRUE;

-- name: DeactivateUserAssignment :exec
UPDATE user_assignment SET is_active = FALSE WHERE id = ?;
