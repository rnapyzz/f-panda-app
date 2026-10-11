-- 説明へのコメントの編集（docs/plan.md「2.24」）。書いた人は自分のコメントを編集できる。編集した日時を残し、画面に「編集済み」と出す。
-- 編集前の本文は残さない（コメントは値ではないので、変更履歴にも残さない）。

ALTER TABLE activity_scenario_comments
    ADD COLUMN edited_at DATETIME NULL COMMENT '最後に編集した日時（NULL は編集していない）' AFTER created_at;
