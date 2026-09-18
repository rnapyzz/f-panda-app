-- +goose Up
CREATE TABLE scenario_version (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  scenario_type ENUM('budget','forecast','actual') NOT NULL,
  fiscal_year SMALLINT NOT NULL,
  as_of_period_id BIGINT UNSIGNED NULL,
  version_label VARCHAR(100) NOT NULL,
  status ENUM('draft','submitted','locked') NOT NULL DEFAULT 'draft',
  is_current BOOLEAN NOT NULL DEFAULT FALSE,
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  submitted_at TIMESTAMP NULL,
  locked_at TIMESTAMP NULL,
  FOREIGN KEY (as_of_period_id) REFERENCES dim_period(id),
  FOREIGN KEY (created_by) REFERENCES app_user(id),
  INDEX idx_scenario_lookup (scenario_type, fiscal_year, is_current)
) ENGINE=InnoDB;

CREATE TABLE fact_amount (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  account_id BIGINT UNSIGNED NOT NULL,
  period_id BIGINT UNSIGNED NOT NULL,
  amount DECIMAL(18,2) NOT NULL,
  source_type ENUM('manual_entry','sheet_binding','csv_import') NOT NULL,
  submission_id BIGINT UNSIGNED NULL,
  import_batch_id BIGINT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_fact_dims (scenario_version_id, business_id, department_id, account_id, period_id),
  INDEX idx_fact_report (period_id, scenario_version_id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id),
  FOREIGN KEY (business_id) REFERENCES dim_business(id),
  FOREIGN KEY (department_id) REFERENCES dim_department(id),
  FOREIGN KEY (account_id) REFERENCES dim_account(id),
  FOREIGN KEY (period_id) REFERENCES dim_period(id),
  FOREIGN KEY (created_by) REFERENCES app_user(id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE fact_amount;
DROP TABLE scenario_version;
