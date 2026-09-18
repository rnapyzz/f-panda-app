-- +goose Up
CREATE TABLE import_batch (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  uploaded_by BIGINT UNSIGNED NOT NULL,
  original_filename VARCHAR(255) NOT NULL,
  file_size_bytes INT NOT NULL,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  status ENUM('processing','completed','failed') NOT NULL DEFAULT 'processing',
  row_count INT NOT NULL DEFAULT 0,
  error_count INT NOT NULL DEFAULT 0,
  error_detail JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  FOREIGN KEY (uploaded_by) REFERENCES app_user(id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id)
) ENGINE=InnoDB;

CREATE INDEX idx_import_batch_scenario ON import_batch (scenario_version_id);

-- fact_amount.import_batch_id predates this table (added in the Phase 1
-- migration as a plain nullable column); wire up the FK now.
ALTER TABLE fact_amount ADD CONSTRAINT fk_fact_amount_import_batch FOREIGN KEY (import_batch_id) REFERENCES import_batch(id);

-- +goose Down
ALTER TABLE fact_amount DROP FOREIGN KEY fk_fact_amount_import_batch;
DROP TABLE import_batch;
