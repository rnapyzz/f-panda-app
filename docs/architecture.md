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

## 認証

- メールアドレス＋パスワードでログインし、セッション Cookie（`fpanda_session`）で認証する
- パスワード: 標準ライブラリの PBKDF2-HMAC-SHA256（600,000回）でハッシュ化。12〜128文字
- セッション: ランダムな32バイトのトークンを Cookie で渡し、DB（`sessions`）には SHA-256 のみ保存する。有効期間は `SESSION_TTL`（デフォルト12時間）。期限切れは API サーバーが1時間ごとに削除する
- Cookie: `HttpOnly`・`SameSite=Lax`。HTTPS 環境では `COOKIE_SECURE=true` で `Secure` を付ける
- CSRF: Go 1.25 の `http.CrossOriginProtection` で、別オリジンからの更新系リクエストを拒否する
- 権限: `auth.RequireAuth`（ログイン必須）と `auth.RequireRole(...)`（ロール制限）のミドルウェアで制御する
- 無効化（`users.is_active = false`）したユーザーはログインできず、既存のセッションも使えなくなる
- 最初の FP&A 管理者は `createuser` コマンドで作成する（`make create-user`）

| API                      | 内容                     |
| ------------------------ | ------------------------ |
| `POST /api/auth/login`   | ログイン                 |
| `POST /api/auth/logout`  | ログアウト               |
| `GET /api/auth/me`       | ログイン中のユーザー     |

## マスタ管理 API

組織・セグメント・機能・勘定科目・ユーザーの CRUD。参照はログインユーザー全員、更新は FP&A（`fpa_admin`）のみ。

| リソース                       | API                                                                 |
| ------------------------------ | ------------------------------------------------------------------- |
| 組織 / セグメント              | `GET/POST /api/{organizations,segments}`、`GET/PUT/DELETE /api/{organizations,segments}/{id}` |
| 機能                           | `GET/POST /api/functions`、`GET/PUT/DELETE /api/functions/{id}`     |
| 勘定科目                       | `GET/POST /api/subjects`、`GET/PUT/DELETE /api/subjects/{id}`       |
| ユーザー                       | `GET/POST /api/users`、`GET/PUT /api/users/{id}`、`PUT /api/users/{id}/password` |

- 一覧は `{"items": [...]}` で全件を返す（マスタは件数が少ないためページングしない）
- 更新は `PUT`（全項目を送る）。更新系のボディには任意の `reason`（変更理由）を含められる。`DELETE` も JSON ボディで `reason` を送れる
- すべての更新は1トランザクションで変更セット（`change_sets`）と監査ログ（`audit_logs`）に記録する。ユーザーのパスワードハッシュは記録しない
- 階層（組織・セグメント）
  - `level` はサーバーが親から計算する。親を変更（移動）すると配下の `level` も更新する
  - 自分自身や配下のノードを親にはできない
  - 機能が所属しているノードの下には子を作れない。機能は子を持たない末端ノードにのみ所属できる
  - 子ノードや機能が残っているノードは削除できない（409）
- 勘定科目: コードは一意。親科目は同じ区分（収益/費用）のみ。子科目がある科目は削除・区分変更できない
- ユーザー: 削除はせず `is_active` で無効化する（無効化するとセッションも削除）。自分自身のロール変更・無効化はできない。パスワード再設定で対象ユーザーのセッションを削除する
- エラー: 入力エラーは 422（`details` にフィールドごとのメッセージ）、存在しない場合は 404、参照中などの矛盾は 409

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
