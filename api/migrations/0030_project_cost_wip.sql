-- プロジェクトの原価と仕掛（docs/plan.md「2.28」）。
--
-- - 科目に「原価の科目」（仕掛品に積む費用科目）と「仕掛品振替の科目」（全体で1つ）を持たせる
-- - 施策に計上の仕方（発生した月に計上／検収月に一括）を持たせる。既存の施策は今と同じ「発生した月に計上」
-- - 検収月に一括の施策の計上月は、シナリオ × 施策ごとに持つ
-- - 振替の金額は budget_facts に source = 'wip' で保存する（アプリが計算する）

ALTER TABLE subjects
    ADD COLUMN is_project_cost BOOLEAN NOT NULL DEFAULT FALSE COMMENT '原価の科目（検収月に一括の施策で仕掛品に積む）' AFTER is_restricted,
    ADD COLUMN role ENUM('normal', 'wip_transfer') NOT NULL DEFAULT 'normal' COMMENT '科目の役割（wip_transfer は仕掛品振替の科目）' AFTER is_project_cost,
    ADD COLUMN wip_transfer_key TINYINT AS (IF(role = 'wip_transfer', 1, NULL)) STORED COMMENT '仕掛品振替の科目を1つにするためのキー' AFTER role,
    ADD UNIQUE KEY uq_subjects_wip_transfer (wip_transfer_key);

ALTER TABLE activities
    ADD COLUMN recognition ENUM('as_incurred', 'on_delivery') NOT NULL DEFAULT 'as_incurred' COMMENT '計上の仕方（on_delivery は検収月に一括）' AFTER assumptions;

CREATE TABLE activity_recognitions (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    scenario_id       BIGINT UNSIGNED NOT NULL,
    activity_id       BIGINT UNSIGNED NOT NULL,
    recognition_month DATE            NOT NULL COMMENT '計上月（検収月。月初日）',
    created_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_activity_recognitions (scenario_id, activity_id),
    KEY idx_activity_recognitions_activity (activity_id),
    CONSTRAINT fk_activity_recognitions_scenario FOREIGN KEY (scenario_id) REFERENCES scenarios (id),
    CONSTRAINT fk_activity_recognitions_activity FOREIGN KEY (activity_id) REFERENCES activities (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='検収月に一括の施策の計上月（シナリオごと）';

ALTER TABLE budget_facts
    MODIFY COLUMN source ENUM('manual', 'formula', 'import', 'wip') NOT NULL;
