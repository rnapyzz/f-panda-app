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

### 最初のユーザーを作成する

```bash
make create-user EMAIL=admin@example.com NAME=管理者 ROLE=fpa_admin
```

パスワード（12文字以上）の入力を求められる。`ROLE` を省略すると `fpa_admin` になる。

### 画面

| URL                                   | 画面                                                         |
| ------------------------------------- | ------------------------------------------------------------ |
| `/activities`                         | 施策一覧                                                     |
| `/activities/{id}`                    | 施策の詳細（マイルストーン・ドライバー・計算式）             |
| `/scenarios`                          | シナリオ一覧・作成（複製）                                   |
| `/scenarios/{id}`                     | シナリオの詳細（ロック・実績 CSV 取込・施策の一覧）          |
| `/scenarios/{id}/activities/{aid}`    | 数値の入力（月別のドライバー値・金額・想定条件）             |
| `/reports`                            | 予実比較（シナリオ・着地見込の比較、階層でのドリルダウン）   |
| `/history`                            | 変更履歴（誰が・いつ・なぜ・何を変えたか）                   |
| `/masters/...`                        | マスタ管理（組織・セグメント・機能・勘定科目・ユーザー）     |

### よく使うコマンド

| コマンド              | 内容                                   |
| --------------------- | -------------------------------------- |
| `make up`             | 起動（ビルドを含む）                    |
| `make down`           | 停止                                   |
| `make logs`           | ログを表示                             |
| `make migrate`        | マイグレーションを適用                  |
| `make migrate-status` | マイグレーションの適用状況を表示        |
| `make reset-db`       | DB のデータを削除して作り直す           |
| `make create-user`    | ユーザーを作成                          |
| `make test`           | Go のテスト、フロントの lint とビルド   |
| `make test-api`       | Go のテスト（DB を使うテストを含む）    |

### マイグレーション

`api/migrations/` に `<4桁の連番>_<説明>.sql` の形式で追加する。適用済みのファイルは変更せず、変更は新しいファイルで行う。

## CI

GitHub Actions（`.github/workflows/ci.yml`）で、PR と main への push ごとに以下を実行する。

| ジョブ       | 内容                                                                 |
| ------------ | -------------------------------------------------------------------- |
| `api`        | gofmt・`go mod tidy` の差分確認・`go vet`・`go test -race`           |
| `integration` | MySQL 8.4 に対してマイグレーションを適用・再実行で変更がないこと、DB を使う統合テスト |
| `web`        | `npm run lint`・`npm run build`（型チェックを含む）                  |
| `compose`    | `docker compose up` で全体を起動し、`/api/health` とトップページを確認 |

### テスト

DB を使うテストは、環境変数 `TEST_DB_HOST` が設定されているときだけ実行される（未設定ならスキップ）。テストごとに一時的なデータベースを作成・削除するため、DB を作成できるユーザー（root）で接続する。`make test-api` は compose の db に接続して実行する。
