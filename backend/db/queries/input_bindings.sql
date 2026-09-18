-- name: CreateInputBinding :execlastid
INSERT INTO input_binding (
  input_sheet_id, name, range_sheet_name,
  start_row, end_row, start_col, end_col,
  header_rows, header_cols,
  row_axis_dimension, col_axis_dimension,
  fixed_dimensions, created_by
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateInputBindingAxisLabel :exec
INSERT INTO input_binding_axis_label (
  binding_id, axis, axis_index, raw_label_text,
  resolved_dimension_type, resolved_dimension_id
)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetInputBinding :one
SELECT id, input_sheet_id, name, range_sheet_name,
       start_row, end_row, start_col, end_col,
       header_rows, header_cols,
       row_axis_dimension, col_axis_dimension,
       fixed_dimensions, is_active, created_by, created_at, updated_at
FROM input_binding
WHERE id = ?;

-- name: ListInputBindingsBySheet :many
SELECT id, input_sheet_id, name, range_sheet_name,
       start_row, end_row, start_col, end_col,
       header_rows, header_cols,
       row_axis_dimension, col_axis_dimension,
       fixed_dimensions, is_active, created_by, created_at, updated_at
FROM input_binding
WHERE input_sheet_id = ? AND is_active = TRUE
ORDER BY created_at DESC;

-- name: ListAxisLabelsByBinding :many
SELECT id, binding_id, axis, axis_index, raw_label_text,
       resolved_dimension_type, resolved_dimension_id
FROM input_binding_axis_label
WHERE binding_id = ?
ORDER BY axis, axis_index;
