-- name: UnsetCurrentScenarioVersions :exec
UPDATE scenario_version
SET is_current = FALSE
WHERE scenario_type = ? AND fiscal_year = ? AND is_current = TRUE;

-- name: CreateScenarioVersion :execlastid
INSERT INTO scenario_version (scenario_type, fiscal_year, as_of_period_id, version_label, is_current, created_by)
VALUES (?, ?, ?, ?, TRUE, ?);

-- name: ListScenarioVersions :many
SELECT id, scenario_type, fiscal_year, as_of_period_id, version_label, status, is_current, created_by, created_at, submitted_at, locked_at
FROM scenario_version
WHERE scenario_type = ? AND fiscal_year = ?
ORDER BY created_at DESC;

-- name: GetCurrentScenarioVersion :one
SELECT id, scenario_type, fiscal_year, as_of_period_id, version_label, status, is_current, created_by, created_at, submitted_at, locked_at
FROM scenario_version
WHERE scenario_type = ? AND fiscal_year = ? AND is_current = TRUE
LIMIT 1;

-- name: GetScenarioVersionByID :one
SELECT id, scenario_type, fiscal_year, as_of_period_id, version_label, status, is_current, created_by, created_at, submitted_at, locked_at
FROM scenario_version
WHERE id = ?;

-- name: SubmitScenarioVersion :execrows
UPDATE scenario_version
SET status = 'submitted', submitted_at = NOW()
WHERE id = ? AND status = 'draft';

-- name: LockScenarioVersion :execrows
UPDATE scenario_version
SET status = 'locked', locked_at = NOW()
WHERE id = ? AND status = 'submitted';
