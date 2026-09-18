-- +goose Up
CREATE TABLE user_assignment (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE KEY uq_assignment (user_id, business_id, department_id),
  FOREIGN KEY (user_id) REFERENCES app_user(id),
  FOREIGN KEY (business_id) REFERENCES dim_business(id),
  FOREIGN KEY (department_id) REFERENCES dim_department(id)
) ENGINE=InnoDB;

CREATE INDEX idx_user_assignment_business_department ON user_assignment (business_id, department_id);

CREATE TABLE submission_scope (
  submission_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (submission_id, business_id, department_id),
  FOREIGN KEY (submission_id) REFERENCES submission(id),
  FOREIGN KEY (business_id) REFERENCES dim_business(id),
  FOREIGN KEY (department_id) REFERENCES dim_department(id)
) ENGINE=InnoDB;

-- (No new index needed for "all submissions for this scenario_version_id":
-- InnoDB already auto-generated one to back submission's existing FK on
-- scenario_version_id, confirmed via SHOW CREATE TABLE submission.)

-- user_id and entity_id deliberately have no FK: entity_id is a polymorphic
-- reference whose target table depends on entity_type, and audit_log rows
-- must remain intact and readable even if the referenced user is later
-- deleted (referential integrity for user_id is enforced in the app layer
-- instead, consistent with the TiDB-portability stance elsewhere in this
-- schema).
CREATE TABLE audit_log (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NULL,
  action VARCHAR(100) NOT NULL,
  entity_type VARCHAR(100) NOT NULL,
  entity_id BIGINT UNSIGNED NULL,
  detail JSON NULL,
  ip_address VARCHAR(45) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE INDEX idx_audit_entity ON audit_log (entity_type, entity_id);
CREATE INDEX idx_audit_user_created ON audit_log (user_id, created_at);
CREATE INDEX idx_audit_created ON audit_log (created_at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE submission_scope;
DROP TABLE user_assignment;
