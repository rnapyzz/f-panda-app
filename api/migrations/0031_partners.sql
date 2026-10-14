-- 取引先（docs/plan.md「2.29」）。施策の相手（顧客・発注先）のマスタと、内訳への紐づけ。
--
-- - 取引先は1つの表で、会社名を項目として持つ。区分（受注／発注）は取引先ごとに1つ
-- - 内訳（activity_lines）ごとに取引先を 0〜1件付ける。紐づけは ID で持つ（名前を変えても紐づきは変わらない）
-- - 組織変更では新しい取引先を作り、古いほうを取引停止にする。内訳に付いている取引先は削除できない

CREATE TABLE partners (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code            VARCHAR(50)     NOT NULL COMMENT '取引先コード（アプリ内の番号。TP-0001 など）',
    name            VARCHAR(100)    NOT NULL COMMENT '取引先名（部署・窓口まで含めた呼び名）',
    company_name    VARCHAR(100)    NOT NULL COMMENT '会社名',
    accounting_code VARCHAR(50)     NULL COMMENT '会計システムの得意先・仕入先のコード',
    direction       ENUM('sales', 'purchase') NOT NULL COMMENT '区分（sales: 受注＝顧客、purchase: 発注＝発注先）',
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE COMMENT '取引中（false は取引停止）',
    note            VARCHAR(500)    NULL COMMENT 'メモ',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_partners_code (code),
    UNIQUE KEY uq_partners_accounting_code (accounting_code),
    KEY idx_partners_company (company_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='取引先';

ALTER TABLE activity_lines
    ADD COLUMN partner_id BIGINT UNSIGNED NULL COMMENT '取引先（任意）' AFTER outlook,
    ADD KEY idx_activity_lines_partner (partner_id),
    ADD CONSTRAINT fk_activity_lines_partner FOREIGN KEY (partner_id) REFERENCES partners (id);
