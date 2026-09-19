-- name: ListBusinesses :many
SELECT id, code, name, is_active, created_at, updated_at
FROM dim_business
ORDER BY code;

-- name: CreateBusiness :execlastid
INSERT INTO dim_business (code, name) VALUES (?, ?);

-- name: UpdateBusiness :exec
UPDATE dim_business SET code = ?, name = ?, is_active = ? WHERE id = ?;

-- name: ListDepartments :many
SELECT id, code, name, is_active, created_at, updated_at
FROM dim_department
ORDER BY code;

-- name: CreateDepartment :execlastid
INSERT INTO dim_department (code, name) VALUES (?, ?);

-- name: UpdateDepartment :exec
UPDATE dim_department SET code = ?, name = ?, is_active = ? WHERE id = ?;

-- name: ListAccounts :many
SELECT id, code, name, account_type, is_active, created_at, updated_at
FROM dim_account
ORDER BY code;

-- name: CreateAccount :execlastid
INSERT INTO dim_account (code, name, account_type) VALUES (?, ?, ?);

-- name: UpdateAccount :exec
UPDATE dim_account SET code = ?, name = ?, account_type = ?, is_active = ? WHERE id = ?;

-- name: ListServices :many
SELECT id, code, name, business_id, is_active, created_at, updated_at
FROM dim_service
ORDER BY code;

-- name: CreateService :execlastid
INSERT INTO dim_service (code, name, business_id) VALUES (?, ?, ?);

-- name: UpdateService :exec
UPDATE dim_service SET code = ?, name = ?, business_id = ?, is_active = ? WHERE id = ?;

-- name: ListProjects :many
SELECT id, code, name, service_id, primary_department_id, is_active, created_at, updated_at
FROM dim_project
ORDER BY code;

-- name: CreateProject :execlastid
INSERT INTO dim_project (code, name, service_id, primary_department_id) VALUES (?, ?, ?, ?);

-- name: UpdateProject :exec
UPDATE dim_project SET code = ?, name = ?, service_id = ?, primary_department_id = ?, is_active = ? WHERE id = ?;

-- name: ListInitiatives :many
SELECT id, code, name, project_id, primary_department_id, is_active, created_at, updated_at
FROM dim_initiative
ORDER BY code;

-- name: CreateInitiative :execlastid
INSERT INTO dim_initiative (code, name, project_id, primary_department_id) VALUES (?, ?, ?, ?);

-- name: UpdateInitiative :exec
UPDATE dim_initiative SET code = ?, name = ?, project_id = ?, primary_department_id = ?, is_active = ? WHERE id = ?;
