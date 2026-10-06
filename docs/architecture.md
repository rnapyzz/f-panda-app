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
- 通知: Slack は Incoming Webhook に `net/http` で送る（SDK は使わない）。定期の送信は API のゴルーチンで行う（外部のスケジューラーは使わない）
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

組織・セグメント・ユニット・勘定科目・会計科目・割当ルール・ユーザー・確度の段階の CRUD。参照はログインユーザー全員、更新は FP&A（`fpa_admin`）のみ。

| リソース                       | API                                                                 |
| ------------------------------ | ------------------------------------------------------------------- |
| 組織 / セグメント              | `GET/POST /api/{organizations,segments}`、`GET/PUT/DELETE /api/{organizations,segments}/{id}` |
| ユニット                       | `GET/POST /api/units`、`GET/PUT/DELETE /api/units/{id}`。種別 `unit_type`（service / cost_center / corporate、省略時 service） |
| 勘定科目                       | `GET/POST /api/subjects`、`GET/PUT/DELETE /api/subjects/{id}`       |
| 会計科目                       | `GET/POST /api/gl-accounts`、`PUT/DELETE /api/gl-accounts/{id}`（コード・名前・対応する科目 `subject_id`・対象外 `is_excluded`・明細を FP&A 以外に見せない `hide_details`。明細から参照されている会計科目は削除できない） |
| 割当ルール                     | `GET/POST /api/allocation-rules`、`PUT/DELETE /api/allocation-rules/{id}`（会計科目 `gl_account_id`・部門 `department_code`（空は全部門）・施策 `activity_id`。会計科目 × 部門で一意。変更は次の取込・再割当から反映） |
| ユーザー                       | `GET/POST /api/users`、`GET/PUT /api/users/{id}`、`PUT /api/users/{id}/password`。Slack のメンバー ID（`slack_user_id`、英大文字・数字、空で未登録）を持つ |
| 確度の段階                     | `GET/POST /api/confidence-levels`、`PUT/DELETE /api/confidence-levels/{id}`（名前・標準の確率 0〜1・判定基準・表示順。コードは作成後に変更できない。施策・内訳から参照されている段階は削除できない） |

- 一覧は `{"items": [...]}` で全件を返す（マスタは件数が少ないためページングしない）
- 組織・セグメント・ユニット・勘定科目はコードを持つ（一意）。組織・セグメント・ユニットは、作成時にコードが空なら `ORG-0001` / `SEG-0001` / `UNIT-0001` 形式で自動採番し、更新時に空なら変更しない
- 更新は `PUT`（全項目を送る）。更新系のボディには任意の `reason`（変更理由）を含められる。`DELETE` も JSON ボディで `reason` を送れる
- すべての更新は1トランザクションで変更セット（`change_sets`）と監査ログ（`audit_logs`）に記録する。ユーザーのパスワードハッシュは記録しない
- 階層（組織・セグメント）
  - `level` はサーバーが親から計算する。親を変更（移動）すると配下の `level` も更新する
  - 自分自身や配下のノードを親にはできない
  - ユニットが所属しているノードの下には子を作れない。ユニットは子を持たない末端ノードにのみ所属できる
  - 子ノードやユニットが残っているノードは削除できない（409）
- 勘定科目: コードは一意。親科目は同じ区分（収益/費用）のみ。子科目がある科目は削除・区分変更できない
- ユーザー: 削除はせず `is_active` で無効化する（無効化するとセッションも削除）。自分自身のロール変更・無効化はできない。パスワード再設定で対象ユーザーのセッションを削除する
- エラー: 入力エラーは 422（`details` にフィールドごとのメッセージ）、存在しない場合は 404、参照中などの矛盾は 409。ファイル取込の行ごとのエラーは `rows`（`line`・`message`）で返す

## 施策 API

| リソース       | API                                                                                     |
| -------------- | --------------------------------------------------------------------------------------- |
| 施策           | `GET/POST /api/activities`、`GET/PUT/DELETE /api/activities/{id}`                        |
| 外部コード     | `POST /api/activities/{id}/external-codes`、`DELETE /api/activities/{id}/external-codes/{eid}` |
| 実績の明細     | `GET /api/activities/{id}/actual-entries?month=&subject_id=`（会計科目・部門・箱の ID・摘要・金額・割当の根拠。`hide_details` の会計科目は、FP&A 以外には会計科目 × 月の合計だけを返す） |
| マイルストーン | `POST /api/activities/{id}/milestones`、`PUT/DELETE /api/activities/{id}/milestones/{mid}` |
| ドライバー定義 | `POST /api/activities/{id}/drivers`、`PUT/DELETE /api/activities/{id}/drivers/{did}`     |
| 重点施策       | `PUT /api/activities/{id}/priority`（`is_priority`。施策の作成・削除の権限が必要。変更履歴に残す） |
| ウォッチ       | `PUT/DELETE /api/activities/{id}/watch`（ログインユーザー本人の印。施策の一覧・詳細に `is_watched` を返す） |
| 金額の内訳     | `POST /api/activities/{id}/lines`、`PUT/DELETE /api/activities/{id}/lines/{lid}`（施策 × 科目に複数。計算式で反映するか・確度の段階（`confidence_level`、空なら施策の段階）・見通しの種類（`outlook`: base / addon / downside）を内訳ごとに設定。計算式・反映の有無・段階・見通しの種類の変更は理由必須） |

- 一覧は `unit_id` / `owner_user_id` / `activity_type` / `status` / `q`（コード・名称の部分一致）で絞り込める
- 詳細（`GET /api/activities/{id}`）はマイルストーン・ドライバー・内訳を含む。各施策に `can_edit`（ログインユーザーが編集できるか）を付ける
- 権限は施策ごとに判定する（docs/plan.md「4. ロール」）
- 値の形式
  - 施策コード: 半角英数字・`-`・`_`、50文字以内、全体で一意。作成時に空欄なら `ACT-0001` 形式で自動採番
  - 外部コード: 空白・カンマ・引用符を除く100文字以内、全体で一意。施策コードと同じ値は不可
  - ステータス: `planned` / `in_progress` / `completed` / `on_hold` / `cancelled`
  - 確度: 確度の段階のコード（`confidence_level`、例: `C`）。日付は `YYYY-MM-DD`。プロジェクト型は開始日・終了日が必須
  - ドライバー code: 英小文字で始まる英小文字・数字・`_`、施策内で一意
  - マイルストーンのステータス: `not_started` / `in_progress` / `completed` / `delayed`
- 計算式は `internal/formula` で解析・評価する（`math/big.Rat` による誤差のない計算、四捨五入は `RoundHalfUp`）。登録時に構文と、未定義のドライバーを使っていないかを検証する
- 内訳の計算式（反映しない式を含む）で使われているドライバーは、code の変更・削除ができない。値が登録済みのドライバーも削除できない
- 金額・ドライバー値・シナリオ条件がある施策は削除できない（409）。削除できる場合は、マイルストーン・ドライバー・内訳も合わせて削除し、それぞれ監査ログに残す

## シナリオ・数値入力 API

| API                                                           | 内容                                         | 権限           |
| ------------------------------------------------------------- | -------------------------------------------- | -------------- |
| `GET /api/scenarios`（`fiscal_year` で絞り込み）              | 一覧（エイリアス・決算確定月・作成中を含む） | 全員           |
| `GET /api/scenarios/active`                                   | 作成中のシナリオ（未設定なら `null`）        | 全員           |
| `POST /api/scenarios`                                         | 作成（`base_scenario_id` 指定で複製。`plan_role`・`actual_through`・`previous_scenario_id`（前回見込、省略時は複製元）も指定できる） | FP&A           |
| `GET/PUT /api/scenarios/{id}`                                 | 取得・名称、エイリアス（`plan_role`）、前回見込（`previous_scenario_id`）、決算確定月（`actual_through`、変更は理由必須）、現場の更新の締切日（`update_deadline`、`YYYY-MM-DD`）の変更 | 全員 / FP&A    |
| `POST /api/scenarios/{id}/activate`                           | 作成中に指定（前の作成中は外れる）           | FP&A           |
| `POST /api/scenarios/{id}/lock`、`/unlock`                    | ロック（決算確定月以前の実績を `scenario_actuals` に保存し、作成中なら外す）・ロック解除（理由必須。保存した実績を外す） | FP&A           |
| `GET /api/scenarios/{id}/activities/{aid}`                    | 施策の月別のドライバー値・金額・想定条件     | 全員           |
| `PUT .../activities/{aid}/driver-values`                      | ドライバー値の一括登録・更新・削除（理由必須） | 施策の編集権限 |
| `PUT .../activities/{aid}/amounts`                            | 金額の直接入力（理由必須）                   | 施策の編集権限 |
| `PUT .../activities/{aid}/condition`                          | 想定条件の登録（空文字で削除）               | 施策の編集権限 |
| `GET/PUT .../activities/{aid}/note`                           | 差異の説明・要因の分類（docs/plan.md「2.10」）  | 参照は全員、更新は数値の入力と同じ |
| `POST/DELETE .../activities/{aid}/complete`                   | 更新を完了にする・完了を取り消す             | 数値の入力と同じ |
| `GET /api/scenarios/{id}/activity-status?scope=mine\|units\|all` | ホーム用。施策ごとの状態・差異の説明・要因の分類、重点施策・ウォッチ、今回・基準・期初計画・修正計画・前回見込の年間の収益・費用、新しく実績になった月の前回見込との差 | 全員（範囲はロールで決まる） |
| `GET /api/scenarios/{id}/milestones?activity_ids=` | ホームのマイルストーン（docs/plan.md「2.11」）。完了していないマイルストーンと、期日超過・遅延・期日が近い・後ろ倒し（前回見込の作成以降の回数と日数） | 全員 |

- 月は `YYYY-MM`。値を `null` にすると削除。1リクエスト1,000件まで
- ドライバー値は小数点以下6桁まで、金額は円単位の整数（マイナス可）
- 「仮の値」（`is_provisional: true`）には理由（`provisional_reason`）が必須
- 更新 API のレスポンスは、更新後の `GET .../activities/{aid}` と同じ形
- 金額は、決算確定月以前の月は実績（ロック済みなら `scenario_actuals`、それ以外は `actual_facts`）、それより後の月は計画値（`budget_facts`）。月ごとに実績かどうか（`actual_months`）を返す
- 入力できるのは、ロックされていないシナリオの決算確定月より後の月で、作成中のシナリオは施策の編集権限、それ以外は FP&A のみ。それ以外は 409。入力の可否はレスポンスの `editable` で分かる
- エイリアスを付けると、同じ年度で同じエイリアスを持つシナリオからは外れる
- 金額の再計算は `internal/calc` が行い、変更した金額は同じ変更セットの監査ログに残す

## 実績取込 API

`POST /api/actuals/import`（FP&A のみ）

- `multipart/form-data` で `file`（CSV）と `reason`（変更理由）を送る。形式は docs/plan.md「6.1 実績 CSV の形式（会計の明細）」
- 各行を docs/plan.md「2.12」の順番（施策コード・外部コード → 割当ルール → 未割当）で施策に割り当て、明細（`actual_entries`）を対象月ごとに置き換え、合計（`actual_facts`）を差分で更新する。シナリオは指定しない
- `?dry_run=true` を付けると、検証と集計だけを行い保存しない（理由は不要）
- レスポンス: 取り込んだ月、データ行数、対象外の行数、割当の根拠ごと（`activity_code` / `external_code` / `rule` / `unallocated`）の件数と金額、合計の追加・更新・削除・変更なしの件数、月別の収益・費用の合計（会計システムとの突合用。未割当を含む）
- CSV にエラーがあれば 422（`code: invalid_csv`）で、`error.rows` に行番号とメッセージを返す（最大100件）。未登録の会計科目は、コードごとに1件（最初の行番号と行数）にまとめて返す。1件もエラーがなければ保存する
- `GET /api/actuals/months?fiscal_year=`: 実績を取り込み済みの月（決算確定月の既定値に使う）

| API | 内容 | 権限 |
| --- | ---- | ---- |
| `GET /api/actuals/unallocated?fiscal_year=` | 未割当の一覧。箱の ID がある行は箱の ID ごと、ない行は会計科目 × 部門ごとにまとめ、月・件数・金額・摘要の例を返す | FP&A |
| `POST /api/actuals/unallocated/assign` | 未割当のまとまり（`box_code`、または `gl_account_id` と `department_code`）を施策（`activity_id`）に割り当てる。箱の ID は外部コードとして、会計科目 × 部門は割当ルールとして登録し、同じまとまりの未割当の行（すべての月）を割り当てる。`all_departments: true` なら部門を問わないルールにする（部門のない行のまとまりも全部門のルールになる）。ルールで割り当てるときは、割当の順番に合わせ、その会計科目（・部門）の未割当の行を箱の ID の有無を問わず割り当てる。登録済みの箱の ID・ルールは 409（再割当で当て直す）。`reason` 必須 | FP&A |
| `POST /api/actuals/reallocate` | 指定した月（`months`）の明細に、今の外部コード・割当ルールを当て直す。`reason` 必須。`dry_run: true` で施策ごとの増減だけを返す | FP&A |

- 明細の行は変更セットに紐づけて保存し、行ごとの監査ログは残さない。監査ログには合計（`actual_facts`）の変更と、外部コード・割当ルールの追加を残す
- 未割当は `actual_facts.activity_id` が NULL の行として持つ。ロック時の `scenario_actuals` にも含める

## 通知 API

締切と通知の仕様は docs/plan.md「2.13」。

| API | 内容 | 権限 |
| --- | ---- | ---- |
| `GET /api/notifications?limit=` | 自分宛てのお知らせ（新しい順、既定 30 件）と未読の数（`unread`） | 全員 |
| `POST /api/notifications/{id}/read`、`POST /api/notifications/read-all` | 既読にする | 全員（自分宛てのみ） |
| `GET/PUT /api/notification-settings` | 通知の設定（種類ごとの有効・無効 `enabled_kinds`、締切の前の日数 `reminder_days`、送信時刻 `send_time`）。Slack が設定されているか（`slack_configured`）も返す | 参照は全員、更新は FP&A |
| `POST /api/notification-settings/test` | Slack にテスト送信する | FP&A |
| `GET /api/notification-runs?limit=` | 送信の記録（日時・種類・シナリオ・宛先の数・Slack の結果） | FP&A |

- 通知の組み立てと送信は `internal/notify`。宛先の決定（未完了の施策・担当者・マネージャー）はホームの更新の状態（`activity-status`）と同じ判定を使う
- 定期の送信: API の起動時にゴルーチンを1つ起動し、1分ごとに送信時刻を過ぎたかを確かめる。送信の記録（`notification_runs`）の一意制約で、同じ通知を同じ日に二重に送らない（複数台・再起動でも安全）
- 即時の通知: 締切の設定（「更新の開始」）と決算確定月の変更（「実績の反映」）は、保存のトランザクションが終わった後に送る。送信の失敗は保存を失敗させない
- Slack: 環境変数 `SLACK_WEBHOOK_URL`（Incoming Webhook）に `net/http` で JSON を POST する。未設定なら送らない。メンションは `<@メンバーID>`。タイムアウトは 10 秒。失敗は `notification_runs` に記録し、同じ日のうちに最大3回まで送り直す
- 時刻は日本時間（`Asia/Tokyo`）で判定する

## 予実比較 API

`GET /api/reports/comparison`（ログインユーザー全員）

- `scenario_ids`: 比較するシナリオ（カンマ区切り、最大4つ）。先頭が差異の基準
- 各シナリオの金額は、決算確定月以前は実績、それより後は計画値（着地見込の指定は廃止）
- `include_actual=true`: 実績データ（取込済みの月）を系列に加える
- `unit_id`: 指定するとそのユニットの施策ごと、指定しなければユニットごとに集計する。ユニットを指定しないときは、未割当の実績を `unit_id: null` の行として返す（全社の合計を会計と一致させるため）
- `measure`: `full`（満額、既定）/ `weighted`（加重見込）/ `optimistic`（楽観）/ `pessimistic`（悲観）。確度の段階と見通しの種類による算出は docs/plan.md「2.8」。実績の月はどれも実績の金額
- レスポンス: 系列（`series`）と、ユニット（または施策）× 科目 × 月の金額（`rows[].values` に系列ごとの金額を文字列で）
- すべての系列は同じ年度のシナリオであること
- セグメント・組織の階層での集計、収益・費用・利益の計算（利益 = 収益 − 費用）、差異の計算は画面側（`web/src/lib/aggregate.ts`、BigInt で計算）で行う

## CSV インポート・エクスポート API

| API | 内容 | 権限 |
| --- | ---- | ---- |
| `GET /api/{organizations,segments,units,subjects,gl-accounts,allocation-rules,users,activities}/export` | CSV（BOM 付き UTF-8）をダウンロード | 全員 |
| `POST /api/{organizations,segments,units,subjects,gl-accounts,allocation-rules,users,activities}/import` | CSV を取込（`multipart/form-data` の `file` と `reason`、`?dry_run=true` で確認のみ） | FP&A |

- 形式と取込のルールは docs/plan.md「6.2」
- 共通処理（アップロードの読込、ヘッダーと行の検証、行エラー、結果、CSV の書き出し）は `internal/csvio`
- 結果: `{"dry_run", "rows", "inserted", "updated", "unchanged"}`。エラーは 422（`code: invalid_csv`、`error.rows` に行番号とメッセージ）

## リスク API

`GET /api/reports/risk?scenario_id=&compare_id=&period=`（ログインユーザー全員）。画面の仕様は docs/plan.md「2.9 リスク画面」。

- `scenario_id`: 基準シナリオ（必須）。`compare_id`: 比較シナリオ（任意、同じ年度）。`period`: `year`（通期、既定）/ `remaining`（決算確定月より後の月のみ）
- 施策ごとに次を返す
  - 確度の段階・前提条件・担当者・ユニット
  - 楽観・基準（加重見込）・悲観・満額の、期間の収益・費用（docs/plan.md「2.8」）
  - 段階別・見通しの種類別の売上（実績の月は「実績」として別に返す）
  - 内訳ごとの段階・見通しの種類・金額
  - 比較シナリオの加重見込の収益・費用
  - 警告: マイルストーンの遅れ（`overdue` / `delayed`、注意として `upcoming`）、後ろ倒し（回数・日数）、下方修正（幅）、連続の下方修正、当たり具合（差の率）。しきい値は docs/plan.md「2.8」の固定値
  - 想定条件（基準・比較）
- マイルストーンの日付は日本時間
- セグメント・組織での集計、並べ替えは画面側で行う

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
