-- name: CreateSubmission :execlastid
INSERT INTO submission (
  input_sheet_id, binding_id, scenario_version_id, submitted_by,
  status, validation_status, validation_detail
)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListSubmissionsBySheet :many
SELECT id, input_sheet_id, binding_id, scenario_version_id, submitted_by,
       submitted_at, status, validation_status, validation_detail
FROM submission
WHERE input_sheet_id = ?
ORDER BY submitted_at DESC;

-- name: CreateSubmissionScope :exec
INSERT INTO submission_scope (submission_id, business_id, department_id)
VALUES (?, ?, ?);

-- name: ListSubmissionScopesByScenarioVersion :many
-- Ordered submitted_at DESC so callers can dedupe by (business_id,
-- department_id) and keep the first row seen — that's the latest
-- non-superseded submission covering that scope.
SELECT ss.business_id, ss.department_id, s.id AS submission_id,
       s.validation_status, s.submitted_at
FROM submission_scope ss
JOIN submission s ON s.id = ss.submission_id
WHERE s.scenario_version_id = ? AND s.status <> 'superseded'
ORDER BY s.submitted_at DESC;

-- name: ListSubmissionsForReview :many
SELECT s.id, s.input_sheet_id, sh.name AS sheet_name, sh.owner_user_id, owner.name AS owner_name,
       s.binding_id, ib.name AS binding_name,
       s.scenario_version_id, sv.scenario_type, sv.fiscal_year, sv.version_label,
       s.submitted_by, submitter.name AS submitted_by_name,
       s.submitted_at, s.status, s.validation_status, s.validation_detail
FROM submission s
JOIN input_sheet sh ON sh.id = s.input_sheet_id
JOIN app_user owner ON owner.id = sh.owner_user_id
JOIN input_binding ib ON ib.id = s.binding_id
JOIN scenario_version sv ON sv.id = s.scenario_version_id
JOIN app_user submitter ON submitter.id = s.submitted_by
ORDER BY s.submitted_at DESC
LIMIT 500;
