-- +goose Up
CREATE TABLE input_sheet (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(200) NOT NULL,
  sheet_snapshot LONGTEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (owner_user_id) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE INDEX idx_input_sheet_owner ON input_sheet (owner_user_id);

CREATE TABLE input_binding (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  input_sheet_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(200) NOT NULL,
  range_sheet_name VARCHAR(100) NOT NULL,
  start_row INT NOT NULL,
  end_row INT NOT NULL,
  start_col INT NOT NULL,
  end_col INT NOT NULL,
  header_rows TINYINT NOT NULL DEFAULT 1,
  header_cols TINYINT NOT NULL DEFAULT 1,
  row_axis_dimension ENUM('account','period','business','department','none') NOT NULL,
  col_axis_dimension ENUM('account','period','business','department','none') NOT NULL,
  fixed_dimensions JSON NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (input_sheet_id) REFERENCES input_sheet(id),
  FOREIGN KEY (created_by) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE INDEX idx_input_binding_sheet ON input_binding (input_sheet_id);

CREATE TABLE input_binding_axis_label (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  binding_id BIGINT UNSIGNED NOT NULL,
  axis ENUM('row','col') NOT NULL,
  axis_index INT NOT NULL,
  raw_label_text VARCHAR(255) NOT NULL,
  resolved_dimension_type ENUM('account','period','business','department') NOT NULL,
  resolved_dimension_id BIGINT UNSIGNED NOT NULL,
  UNIQUE KEY uq_axis_label (binding_id, axis, axis_index),
  FOREIGN KEY (binding_id) REFERENCES input_binding(id)
) ENGINE=InnoDB;

CREATE TABLE submission (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  input_sheet_id BIGINT UNSIGNED NOT NULL,
  binding_id BIGINT UNSIGNED NOT NULL,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  submitted_by BIGINT UNSIGNED NOT NULL,
  submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  status ENUM('draft','submitted','superseded') NOT NULL DEFAULT 'submitted',
  validation_status ENUM('ok','warning','error') NOT NULL DEFAULT 'ok',
  validation_detail JSON NULL,
  FOREIGN KEY (input_sheet_id) REFERENCES input_sheet(id),
  FOREIGN KEY (binding_id) REFERENCES input_binding(id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id),
  FOREIGN KEY (submitted_by) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE INDEX idx_submission_binding ON submission (binding_id);

-- fact_amount.submission_id predates the submission table (added in the
-- Phase 1 migration as a plain nullable column); wire up the FK now that
-- the referenced table exists.
ALTER TABLE fact_amount ADD CONSTRAINT fk_fact_amount_submission FOREIGN KEY (submission_id) REFERENCES submission(id);

-- +goose Down
ALTER TABLE fact_amount DROP FOREIGN KEY fk_fact_amount_submission;
DROP TABLE submission;
DROP TABLE input_binding_axis_label;
DROP TABLE input_binding;
DROP TABLE input_sheet;
