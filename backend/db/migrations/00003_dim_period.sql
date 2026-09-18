-- +goose Up
CREATE TABLE dim_period (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  fiscal_year SMALLINT NOT NULL,
  fiscal_month TINYINT NOT NULL,
  calendar_year SMALLINT NOT NULL,
  calendar_month TINYINT NOT NULL,
  start_date DATE NOT NULL,
  end_date DATE NOT NULL,
  label VARCHAR(20) NOT NULL,
  UNIQUE KEY uq_period_fy_fm (fiscal_year, fiscal_month)
) ENGINE=InnoDB;
-- Rows are populated by `cmd/seedperiods` (idempotent upsert), not by a
-- migration, so the rolling window can be extended in later years without a
-- new migration each time.

-- +goose Down
DROP TABLE dim_period;
