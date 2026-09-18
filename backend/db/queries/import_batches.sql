-- name: CreateImportBatch :execlastid
INSERT INTO import_batch (uploaded_by, original_filename, file_size_bytes, scenario_version_id, status)
VALUES (?, ?, ?, ?, 'processing');

-- name: CompleteImportBatch :exec
UPDATE import_batch
SET status = ?, row_count = ?, error_count = ?, error_detail = ?, completed_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListImportBatchesByScenarioVersion :many
SELECT id, uploaded_by, original_filename, file_size_bytes, scenario_version_id,
       status, row_count, error_count, error_detail, created_at, completed_at
FROM import_batch
WHERE scenario_version_id = ?
ORDER BY created_at DESC;
