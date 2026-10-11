-- 作成中のシナリオを年度ごとに1つにする（docs/plan.md「2.25」「14」）。
-- 12〜3月は、当期の見込と翌期の計画が同時に作成中になる。今の作成中の版はそのまま。

ALTER TABLE scenarios
    DROP INDEX uq_scenarios_active,
    DROP COLUMN active_key;

ALTER TABLE scenarios
    ADD COLUMN active_fiscal_year INT AS (IF(is_active, fiscal_year, NULL)) STORED COMMENT '作成中なら年度（年度ごとに1つの一意制約）',
    ADD UNIQUE KEY uq_scenarios_active_per_year (active_fiscal_year);
