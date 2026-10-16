-- シナリオの説明（docs/plan.md「2.31」）。計画の筋書きを、シナリオごとに FP&A が文章で書く。
--
-- - シナリオごとに1行。空の欄は NULL ではなく空文字で持つ
-- - 複製元のあるシナリオの作成では、数値と一緒に複製する（新年度の期初計画は空で始める）

CREATE TABLE scenario_narratives (
    scenario_id BIGINT UNSIGNED NOT NULL,
    outline     TEXT            NOT NULL COMMENT '計画の骨子',
    growth      TEXT            NOT NULL COMMENT '成長の柱',
    premises    TEXT            NOT NULL COMMENT '主な前提',
    risks       TEXT            NOT NULL COMMENT '主なリスクと機会',
    updated_by  BIGINT UNSIGNED NULL COMMENT '最後に書いた人',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (scenario_id),
    KEY idx_scenario_narratives_updated_by (updated_by),
    CONSTRAINT fk_scenario_narratives_scenario   FOREIGN KEY (scenario_id) REFERENCES scenarios (id),
    CONSTRAINT fk_scenario_narratives_updated_by FOREIGN KEY (updated_by)  REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='シナリオの説明';
