# f-panda-app

管理会計・予実管理・KPI管理・FP&A・要員計画を支援するWebアプリケーション（MVP開発中）。

背景・コンセプト・データモデル・フェーズ計画は [docs/plan.md](docs/plan.md) を参照してください。

## 技術スタック

- バックエンド: Go（標準ライブラリ中心、`database/sql` + [sqlc](https://sqlc.dev/)）
- フロントエンド: React + Vite（TypeScript, SPA）
- DB: MySQL（将来のTiDB移行を想定）
- 開発環境: Docker / Docker Compose

## 必要なもの

- Docker / Docker Compose のみ（Go・Node・MySQLをホストに直接インストールする必要はありません）

## 開発環境の起動

```bash
make dev
```

これで以下が起動します。

| サービス | URL | 内容 |
|---|---|---|
| frontend | http://localhost:5183 | Vite開発サーバー（ホットリロード）。`/api`はbackendへプロキシされる |
| backend | http://localhost:8081 | Go API（`air`によるホットリロード）。`/healthz`でヘルスチェック（コンテナ間通信は`backend:8080`） |
| mysql | localhost:3307 | ユーザー`fpanda` / パスワード`fpanda` / DB`fpanda`（コンテナ間通信は`mysql:3306`） |

ホストの3306/5173/8080番ポートが別プロセスで使用中の環境が多いため、あえて3307/5183/8081にずらしています。競合する場合は`docker-compose.yml`のports設定を調整してください。

停止する場合:

```bash
make down
```

### スキーマのマイグレーション

`mysql`コンテナが起動している状態で実行してください。

```bash
make migrate
```

直前のマイグレーションを取り消す場合:

```bash
make migrate-down
```

### ログインユーザーの作成

初回はログイン可能なユーザーが存在しないため、事務局管理者（`office_admin`）を1人作成してください。

```bash
make seed EMAIL=admin@example.com NAME="管理者" PASSWORD=changeme
```

作成後、http://localhost:5183 からログインできます。

## 個別コマンド

sqlc生成コードの再生成（`backend/db/queries/*.sql`や`backend/db/migrations/*.sql`を変更したとき）:

```bash
make sqlc-generate
```

Lint:

```bash
make lint
```

テスト:

```bash
make test
```

## 本番ビルド

```bash
make build
```

`frontend`をビルドし、その静的アセットを`backend/web/dist`にコピーしたうえで、Goバイナリに埋め込んでビルドします（`bin/server`）。本番のコンテナイメージは`backend/Dockerfile`の`prod`ステージから作成します。

```bash
docker build --target prod -t f-panda-app-backend ./backend
```

デプロイ物はこのコンテナイメージ＋MySQL（RDS等）のみで完結します。

## ディレクトリ構成

```
f-panda-app/
  backend/    Go API（cmd/server, cmd/seed, internal/, db/migrations, db/queries）
  frontend/   React + Vite SPA
  docs/       実装計画（plan.md）
  docker-compose.yml
  Makefile
```

詳細なディレクトリ構成・アーキテクチャ判断・フェーズごとの検証計画は [docs/plan.md](docs/plan.md) を参照してください。
