# F-Panda

[![CI](https://github.com/rnapyzz/f-panda-app/actions/workflows/ci.yml/badge.svg)](https://github.com/rnapyzz/f-panda-app/actions/workflows/ci.yml)

活動（施策）ベースの予実管理・ローリングフォアキャスト支援アプリ。

## 構成

| ディレクトリ | 内容                                        |
| ------------ | ------------------------------------------- |
| `api/`       | Go の API サーバーとマイグレーション        |
| `web/`       | React + TailwindCSS（Vite）                 |
| `nginx/`     | リバースプロキシ設定                        |

## 開発環境

必要なもの: Docker（Docker Compose v2）

```bash
cp .env.example .env   # 任意。未作成でもデフォルト値で起動する
make up                # docker compose up --build -d
```

- アプリ: http://localhost:8080
- API: http://localhost:8080/api/health
- MySQL: `localhost:3307`（ユーザー / パスワード: `fpanda` / `fpanda`）

起動時に `migrate` コンテナが未適用のマイグレーションを自動で適用する。

### よく使うコマンド

| コマンド              | 内容                                   |
| --------------------- | -------------------------------------- |
| `make up`             | 起動（ビルドを含む）                    |
| `make down`           | 停止                                   |
| `make logs`           | ログを表示                             |
| `make migrate`        | マイグレーションを適用                  |
| `make migrate-status` | マイグレーションの適用状況を表示        |
| `make reset-db`       | DB のデータを削除して作り直す           |
| `make test`           | Go のテスト、フロントの lint とビルド   |

### マイグレーション

`api/migrations/` に `<4桁の連番>_<説明>.sql` の形式で追加する。適用済みのファイルは変更せず、変更は新しいファイルで行う。

## CI

GitHub Actions（`.github/workflows/ci.yml`）で、PR と main への push ごとに以下を実行する。

| ジョブ       | 内容                                                                 |
| ------------ | -------------------------------------------------------------------- |
| `api`        | gofmt・`go mod tidy` の差分確認・`go vet`・`go test -race`           |
| `migrations` | MySQL 8.4 に対してマイグレーションを適用し、再実行で変更がないこと   |
| `web`        | `npm run lint`・`npm run build`（型チェックを含む）                  |
| `compose`    | `docker compose up` で全体を起動し、`/api/health` とトップページを確認 |
