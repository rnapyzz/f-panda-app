# F-Panda

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
