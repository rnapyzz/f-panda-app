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
- 権限: `auth.RequireAuth`（ログイン必須）と `auth.RequireRole(...)`（ロール制限）のミドルウェアで制御する。施策のようにデータごとに権限が変わるものはハンドラー内で判定する
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
- エラー: 入力エラーは 422（`details` にフィールドごとのメッセージ）、存在しない場合は 404、参照中などの矛盾は 409。ファイル取込の行ごとのエラーは `rows`（`line`・`message`）で返す

## 施策 API

| リソース       | API                                                                                     |
| -------------- | --------------------------------------------------------------------------------------- |
| 施策           | `GET/POST /api/activities`、`GET/PUT/DELETE /api/activities/{id}`                        |
| 外部コード     | `POST /api/activities/{id}/external-codes`、`DELETE /api/activities/{id}/external-codes/{eid}` |
| マイルストーン | `POST /api/activities/{id}/milestones`、`PUT/DELETE /api/activities/{id}/milestones/{mid}` |
| ドライバー定義 | `POST /api/activities/{id}/drivers`、`PUT/DELETE /api/activities/{id}/drivers/{did}`     |
| 計算式         | `PUT/DELETE /api/activities/{id}/formulas/{subject_id}`（科目ごとに1つ、PUT で登録・更新） |

- 一覧は `function_id` / `owner_user_id` / `activity_type` / `status` / `q`（コード・名称の部分一致）で絞り込める
- 詳細（`GET /api/activities/{id}`）はマイルストーン・ドライバー・計算式を含む。各施策に `can_edit`（ログインユーザーが編集できるか）を付ける
- 権限は施策ごとに判定する（docs/plan.md「4. ロール」）
- 値の形式
  - 施策コード: 半角英数字・`-`・`_`、50文字以内、全体で一意。作成時に空欄なら `ACT-0001` 形式で自動採番
  - 外部コード: 空白・カンマ・引用符を除く100文字以内、全体で一意。施策コードと同じ値は不可
  - ステータス: `planned` / `in_progress` / `completed` / `on_hold` / `cancelled`
  - 確度: 0〜1、小数点以下4桁まで。日付は `YYYY-MM-DD`。プロジェクト型は開始日・終了日が必須
  - ドライバー code: 英小文字で始まる英小文字・数字・`_`、施策内で一意。`probability` は予約語
  - マイルストーンのステータス: `not_started` / `in_progress` / `completed` / `delayed`
- 計算式は `internal/formula` で解析・評価する（`math/big.Rat` による誤差のない計算、四捨五入は `RoundHalfUp`）。登録時に構文と、未定義のドライバーを使っていないかを検証する
- 計算式で使われているドライバーは、code の変更・削除ができない。値が登録済みのドライバーも削除できない
- 金額・ドライバー値・シナリオ条件がある施策は削除できない（409）。削除できる場合は、マイルストーン・ドライバー・計算式も合わせて削除し、それぞれ監査ログに残す

## シナリオ・数値入力 API

| API                                                           | 内容                                         | 権限           |
| ------------------------------------------------------------- | -------------------------------------------- | -------------- |
| `GET /api/scenarios`（`fiscal_year` / `scenario_kind` で絞り込み） | 一覧                                         | 全員           |
| `POST /api/scenarios`                                         | 作成（`base_scenario_id` 指定で複製）          | FP&A           |
| `GET/PUT /api/scenarios/{id}`                                 | 取得・名称変更                               | 全員 / FP&A    |
| `POST /api/scenarios/{id}/lock`、`/unlock`                    | ロック・ロック解除（解除は理由必須）         | FP&A           |
| `GET /api/scenarios/{id}/activities/{aid}`                    | 施策の月別のドライバー値・金額・想定条件     | 全員           |
| `PUT .../activities/{aid}/driver-values`                      | ドライバー値の一括登録・更新・削除（理由必須） | 施策の編集権限 |
| `PUT .../activities/{aid}/amounts`                            | 金額の直接入力（理由必須）                   | 施策の編集権限 |
| `PUT .../activities/{aid}/condition`                          | 想定条件の登録（空文字で削除）               | 施策の編集権限 |

- 月は `YYYY-MM`。値を `null` にすると削除。1リクエスト1,000件まで
- ドライバー値は小数点以下6桁まで、金額は円単位の整数（マイナス可）
- 「仮の値」（`is_provisional: true`）には理由（`provisional_reason`）が必須
- 更新 API のレスポンスは、更新後の `GET .../activities/{aid}` と同じ形
- ロック済みシナリオ・実績シナリオへの入力は 409。入力の可否はレスポンスの `editable` で分かる
- 金額の再計算は `internal/calc` が行い、変更した金額は同じ変更セットの監査ログに残す

## 実績取込 API

`POST /api/scenarios/{id}/actuals/import`（FP&A のみ）

- `multipart/form-data` で `file`（CSV）と `reason`（変更理由）を送る。形式は docs/plan.md「6.1 実績 CSV の形式」
- `?dry_run=true` を付けると、検証と集計だけを行い保存しない（理由は不要）
- レスポンス: 取り込んだ月、データ行数、合算後の件数、追加・更新・削除・変更なしの件数、月別の収益・費用の合計（会計システムとの突合用）
- CSV にエラーがあれば 422（`code: invalid_csv`）で、`error.rows` に行番号とメッセージを返す（最大100件）。1件もエラーがなければ保存する
- 取込先は、ロックされていない実績シナリオ（`scenario_kind = actual`）のみ。それ以外は 409

## 予実比較 API

`GET /api/reports/comparison`（ログインユーザー全員）

- `scenario_ids`: 比較するシナリオ（カンマ区切り、最大4つ）。先頭が差異の基準
- `landing_actual_id` / `landing_forecast_id` / `landing_through`（YYYY-MM）: 着地見込を系列に加える。`landing_through` までの月は実績シナリオ、それ以降の月は見込シナリオの金額を使う
- `function_id`: 指定するとその機能の施策ごと、指定しなければ機能ごとに集計する
- レスポンス: 系列（`series`）と、機能（または施策）× 科目 × 月の金額（`rows[].values` に系列ごとの金額を文字列で）
- すべての系列は同じ年度のシナリオであること
- セグメント・組織の階層での集計、収益・費用・利益の計算（利益 = 収益 − 費用）、差異の計算は画面側（`web/src/lib/aggregate.ts`、BigInt で計算）で行う

## リスク API

`GET /api/reports/risk?scenario_id=&optimistic_id=&pessimistic_id=`（ログインユーザー全員）

- 基準シナリオ（必須）と、楽観・悲観シナリオ（任意、基準と同じ年度）を指定する
- 施策ごとに、確度・前提条件、基準／楽観／悲観の年間の収益・費用、基準の仮の値（件数・金額・理由）、注意が必要なマイルストーン、シナリオごとの想定条件を返す
- 注意が必要なマイルストーン: 完了していないもののうち、期日超過（`overdue`）・状態が遅延（`delayed`）・30日以内に期日（`upcoming`）。日付は日本時間
- 確度帯の集計、振れ幅（楽観と悲観の差）の計算、並べ替えは画面側で行う

## 変更履歴 API

| API                         | 内容                                                                                   |
| --------------------------- | -------------------------------------------------------------------------------------- |
| `GET /api/change-sets`      | 変更セットの一覧（新しい順）。`scenario_id` / `activity_id` / `user_id` / `reason`（with・without）/ `from`・`to`（YYYY-MM-DD）で絞り込み、`before_id` と `limit` で続きを取得 |
| `GET /api/change-sets/{id}` | 変更セットと監査ログ（変更前後の値と、対象を人が読める形にした `label`）                |

- 参照はログインユーザー全員
- 一覧の各行は、テーブル別の件数と、関係する施策（最大5件）を含む
- `activity_id` の絞り込みは、施策自体・施策に紐づくレコード（`activity_id` を持つもの）・ドライバー値（ドライバー経由）の変更を対象にする
- 変更のなかった操作（監査ログが0件の変更セット）は一覧に出さない

## フロントエンド（React）

- React + TypeScript + TailwindCSS v4、ビルドは Vite。依存ライブラリは React と Tailwind のみ
- 構成
  - `src/api/`: API クライアント（`client.ts`）と型（`types.ts`）。エラーは `ApiError`（`code`・`details`・`rows`）として投げる
  - `src/lib/`: ルーター（History API を使った最小実装）、認証（`AuthProvider`）、`useApi`（取得と再取得）、書式
  - `src/components/`: 共通 UI（ボタン・入力・表・ダイアログなど）、レイアウト、確認ダイアログ、変更理由ダイアログ
  - `src/pages/`: 画面
- 画面遷移は `lib/router.tsx` の `Link` / `navigate` を使う。ルートは `App.tsx` に定義する
- 変更理由: `useReason().withReason(op)` で操作を実行すると、API が「変更理由が必要」（422 `details.reason`）を返したときに理由の入力ダイアログを出して再実行する
- 権限による表示の切り替え（編集ボタンを出すかなど）は画面で行うが、最終的な判定は API が行う
- テスト: ロジックは `node:test`（`src/**/*.test.ts`、Node の型ストリップで .ts をそのまま実行）、主要な操作の流れは Playwright の E2E（`e2e/`）。E2E は `scripts/e2e.sh` が専用の compose スタックを起動して実行する

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
