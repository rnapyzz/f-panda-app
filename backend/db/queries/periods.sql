-- name: ListPeriods :many
SELECT id, fiscal_year, fiscal_month, calendar_year, calendar_month, start_date, end_date, label
FROM dim_period
ORDER BY fiscal_year, fiscal_month;

-- name: UpsertPeriod :exec
INSERT INTO dim_period (fiscal_year, fiscal_month, calendar_year, calendar_month, start_date, end_date, label)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  calendar_year = VALUES(calendar_year),
  calendar_month = VALUES(calendar_month),
  start_date = VALUES(start_date),
  end_date = VALUES(end_date),
  label = VALUES(label);
