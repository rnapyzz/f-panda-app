# CLAUDE.md

## プロジェクト概要

活動（施策）ベースの予実管理・ローリングフォアキャストを支援する FP&A 向けアプリ。

- 要件・ドメイン設計: [docs/plan.md](docs/plan.md)
- アーキテクチャ・技術スタック: [docs/architecture.md](docs/architecture.md)

## 技術スタック

nginx + React.js + TailwindCSS + Go + MySQL 8。すべて Docker コンテナで動かす。

## よく使うコマンド

- 起動: `make up`（http://localhost:8080）／停止: `make down`
- マイグレーション: `make migrate`、`make migrate-status`、DB 作り直し: `make reset-db`
- テスト: `make test`（Go のテスト、フロントの lint とビルド）
- マイグレーションは `api/migrations/` に `<4桁の連番>_<説明>.sql` で追加し、適用済みファイルは変更しない

## 開発フロー

作業ごとに main からブランチを切り、コミット → プッシュ → PR 作成まで行う。

## 開発方針

- **標準パッケージ・標準ライブラリを優先する。** Go は `net/http`・`database/sql`・`encoding/json`・`log/slog` を使う。外部依存は MySQL ドライバー（`go-sql-driver/mysql`）以外は原則追加しない。追加が必要なときは理由を示して確認をとる。
- 金額・ドライバー値は `DECIMAL` で扱い、浮動小数点で計算しない。
- 年月（`target_month`）は月初日の `DATE` で保持する。
- 値の変更は変更セット（`change_sets`）単位で行い、変更理由を必須にする。変更前後の値は `audit_logs` に残す。
- ロック済みシナリオと実績シナリオは画面から編集できないようにする。
- ドキュメント・UI 文言は日本語で書く。
