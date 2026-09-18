-- name: ListFactAmountsByScenarioVersion :many
SELECT business_id, department_id, account_id, period_id, amount
FROM fact_amount
WHERE scenario_version_id = ?;

-- name: UpsertFactAmountFromSubmission :exec
INSERT INTO fact_amount (scenario_version_id, business_id, department_id, account_id, period_id, amount, source_type, submission_id, created_by)
VALUES (?, ?, ?, ?, ?, ?, 'sheet_binding', ?, ?)
ON DUPLICATE KEY UPDATE
  amount = VALUES(amount),
  source_type = VALUES(source_type),
  submission_id = VALUES(submission_id),
  import_batch_id = NULL,
  created_by = VALUES(created_by),
  updated_at = CURRENT_TIMESTAMP;

-- name: UpsertFactAmountFromImport :exec
INSERT INTO fact_amount (scenario_version_id, business_id, department_id, account_id, period_id, amount, source_type, import_batch_id, created_by)
VALUES (?, ?, ?, ?, ?, ?, 'csv_import', ?, ?)
ON DUPLICATE KEY UPDATE
  amount = VALUES(amount),
  source_type = VALUES(source_type),
  import_batch_id = VALUES(import_batch_id),
  submission_id = NULL,
  created_by = VALUES(created_by),
  updated_at = CURRENT_TIMESTAMP;
