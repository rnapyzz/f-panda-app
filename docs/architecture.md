# アーキテクチャ

## 技術スタック

| レイヤー       | 技術                                                                 |
| -------------- | -------------------------------------------------------------------- |
| リバースプロキシ | nginx                                                                |
| フロントエンド | React.js + TypeScript + TailwindCSS（Vite でビルド）                  |
| バックエンド   | Go（標準パッケージ中心）                                              |
| データベース   | MySQL 8.4                                                            |
| 実行環境       | Docker / Docker Compose                                              |

基本方針: **できるだけ標準パッケージ・標準ライブラリで実装する。** 外部ライブラリの追加は必要最小限にとどめ、追加する場合は理由を残す。

## コンテナ構成

```
                ┌──────────────────────────────┐
  browser ───▶  │ nginx                        │
                │  /      → React の静的ファイル  │
                │  /api/* → api:8080           │
                └───────────────┬──────────────┘
                                │
                        ┌───────▼───────┐        ┌──────────┐
                        │ api (Go)      │ ─────▶ │ db       │
                        │ net/http      │        │ MySQL 8  │
                        └───────────────┘        └──────────┘
```

| サービス | 役割                                                                       |
| -------- | -------------------------------------------------------------------------- |
| `nginx`  | React のビルド成果物を配信し、`/api` を `api` コンテナに転送する            |
| `web`    | React アプリ（開発時は Vite dev server）                            |
| `api`    | Go の API サーバー                                                          |
| `db`     | MySQL 8。データはボリュームに永続化する                                     |

## バックエンド（Go）

- HTTP サーバー・ルーティング: `net/http` と `http.ServeMux`（Go 1.22 以降のメソッド・パスパターン）
- DB アクセス: `database/sql` + `github.com/go-sql-driver/mysql`（唯一の必須外部依存）
- JSON: `encoding/json`
- ログ: `log/slog`
- テスト: `testing`
- マイグレーション: `api/migrations/<連番>_<説明>.sql` を `embed` で埋め込み、自前のランナー（`api/internal/migrate`）で適用する。適用済みバージョンは `schema_migrations` に記録する。compose の `migrate` サービスが起動時に適用する
- 計算式の評価: ドライバー式（四則演算・括弧・数値・ドライバー code 参照）は自前の簡易パーサで評価する

## フロントエンド（React）

- React + TypeScript + TailwindCSS v4、ビルドは Vite
- API 呼び出しは `fetch`
- ライブラリの追加は必要最小限（ルーティング・グラフ描画などは必要になった時点で検討する）

## ディレクトリ構成（案）

```
.
├── compose.yaml
├── nginx/
│   └── default.conf
├── api/                  # Go
│   ├── cmd/server/       # エントリーポイント
│   ├── internal/         # ドメイン・ハンドラー・リポジトリ
│   └── migrations/       # SQL マイグレーション
├── web/                  # React + TailwindCSS
│   └── src/
└── docs/
    ├── plan.md           # 要件・ドメイン設計
    └── architecture.md   # 本ドキュメント
```
