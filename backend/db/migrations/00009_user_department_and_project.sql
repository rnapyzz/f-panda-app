-- +goose Up
ALTER TABLE app_user
  ADD COLUMN department_id BIGINT UNSIGNED NULL AFTER role,
  ADD CONSTRAINT fk_app_user_department FOREIGN KEY (department_id) REFERENCES dim_department(id);

CREATE TABLE dim_project (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(50) NOT NULL,
  name VARCHAR(200) NOT NULL,
  service_id BIGINT UNSIGNED NOT NULL,
  primary_department_id BIGINT UNSIGNED NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_project_code (code),
  FOREIGN KEY (service_id) REFERENCES dim_service(id),
  FOREIGN KEY (primary_department_id) REFERENCES dim_department(id)
) ENGINE=InnoDB;

-- dim_initiative moves one level down the hierarchy: it referenced
-- dim_service directly, but now sits under the new dim_project instead
-- (dim_business > dim_service > dim_project > dim_initiative). The FK is
-- given an explicit name so the down migration doesn't have to guess
-- MySQL's auto-generated constraint name.
ALTER TABLE dim_initiative DROP FOREIGN KEY dim_initiative_ibfk_1;
ALTER TABLE dim_initiative CHANGE COLUMN service_id project_id BIGINT UNSIGNED NOT NULL;
ALTER TABLE dim_initiative ADD CONSTRAINT fk_initiative_project FOREIGN KEY (project_id) REFERENCES dim_project(id);

-- +goose Down
ALTER TABLE dim_initiative DROP FOREIGN KEY fk_initiative_project;
ALTER TABLE dim_initiative CHANGE COLUMN project_id service_id BIGINT UNSIGNED NOT NULL;
ALTER TABLE dim_initiative ADD CONSTRAINT dim_initiative_ibfk_1 FOREIGN KEY (service_id) REFERENCES dim_service(id);

DROP TABLE dim_project;

ALTER TABLE app_user DROP FOREIGN KEY fk_app_user_department;
ALTER TABLE app_user DROP COLUMN department_id;
