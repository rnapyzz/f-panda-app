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
