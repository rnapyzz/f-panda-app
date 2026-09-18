# FP&A管理会計Webアプリ — MVP実装計画

## Context

管理会計・予実管理・KPI管理・FP&A領域（Loglass/Finovoに近い）のWebアプリを新規開発する。リポジトリ（[f-panda-app](https://github.com/rnapyzz/f-panda-app)）はコミット0件の空リポジトリで、完全にゼロからの構築となる。

**解決したい課題**：通常の予算作成・見込作成・予実分析フローは「①現場担当者が作成→②事務局が集約→③事務局が集計・資料化」という流れだが、(a) 現場に自由入力させると事務局の集約が破綻する、(b) 事務局が指定フォーマットを強制すると現場が独自フォーマット→転記という二重作業と転記ミスを生む、という2つの失敗パターンがある。

**採用する解決策**：事務局は固定の正規化データスキーマ（ディメンションモデル）だけを所有し、現場担当者はアプリ内スプレッドシートで自分の使いやすいレイアウトを自由に作る。特定のセル範囲を正規化スキーマの軸（事業・部門・勘定科目・期間）に「バインディング」する仕組みで、両者の間を橋渡しする。この入力層の柔軟性こそが本プロダクトの差別化ポイントであり、同時に最もリスクの高い開発対象でもある。

このプランは、ユーザーとの対話で確定した以下の要件・技術選定を前提に、Planサブエージェントの設計をレビュー・統合したもの。

## 確定済みの要件（前提として扱う）

**業務スコープ**
- 対象：予算編成＋見込（ローリングフォーキャスト）＋予実差異分析。実績はCSV/Excelの手動取り込み（会計システムAPI連携はしない）
- 承認フローなし：現場担当者→事務局の直接提出。ただし提出状況トラッキングは必要
- ディメンション：事業×部門×勘定科目×期間×シナリオ（予算/見込/実績）。事業と部門はマトリクス構造（1部門が複数事業にまたがる）で、MVPでは配賦ロジックなし、現場がそれぞれ直接入力
- プロジェクト/拠点軸はMVP対象外だが拡張可能な設計にする

**入力層（差別化機能）**
- Univer.js（Apache 2.0、アクティブメンテナンス）ベースのアプリ内スプレッドシート
- 「セルバインディング」：範囲選択→行/列のどちらが何の軸かを指定→固定ディメンション値を設定、という最小機能のみ。数式サポート・リッチ書式・完全なバインディング崩れ検知はPhase 2以降に明示的に先送り

**技術スタック**
- バックエンド：Go、標準ライブラリ志向（`net/http`のGo 1.22+強化ルーティング、`database/sql`+`sqlc`、ORM不使用）
- フロントエンド：React + Vite（SPA、TypeScript）。Next.jsは不採用（SSR不要、スプレッドシートエンジンがクライアント専用、本番のNode.jsランタイムを増やさずGoバイナリのみで完結させ攻撃面を減らす）。スタイリングはTailwind CSS v4（`@tailwindcss/vite`）
- DB：MySQL（将来のTiDB移行を意識した設計）
- 認証：サーバーサイドセッション（HttpOnly/Secure/SameSite Cookie）＋CSRF対策
- ホスティング：AWS東京リージョン想定（未確定、インフラ前提は軽めに）
- セキュリティ重点：CSV/Excel取り込みが最大の攻撃面（CSVフォーミュラインジェクション、XLSXのXXEリスク、zip爆弾、ファイルサイズ制限）

## データモデル（スタースキーマ）

サロゲートキーはすべて`BIGINT UNSIGNED`、InnoDB、`utf8mb4_0900_ai_ci`。トリガー・ストアドプロシージャは使わず（TiDB移植性のため）、整合性はアプリ層でも担保する。

**ローリング見込のバージョニング**：`scenario_version`テーブルが「版」を表す。見込（forecast）は月次サイクルごとに新しい版を作り、`as_of_period_id`（その見込を作成した月）を持つ。同じ対象期間（`period_id`）でも版が異なれば別データとして共存でき、「4月時点で見た8月の見込」と「5月時点で見た8月の見込」を衝突なく比較できる。予算・実績も同じ仕組みを再利用する。

```sql
-- ===== ディメンション / マスタ =====
CREATE TABLE dim_business (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(50) NOT NULL,
  name VARCHAR(200) NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_business_code (code)
) ENGINE=InnoDB;

CREATE TABLE dim_department (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(50) NOT NULL,
  name VARCHAR(200) NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_department_code (code)
) ENGINE=InnoDB;
-- 事業×部門は親子関係を持たない自由なマトリクス。ファクト行が両IDを独立に持つことで、
-- 配賦エンジンなしでも将来の配賦導入を妨げない設計にしている。

CREATE TABLE dim_account (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(50) NOT NULL,
  name VARCHAR(200) NOT NULL,
  account_type ENUM('revenue','cost','other') NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_account_code (code)
) ENGINE=InnoDB;

CREATE TABLE dim_period (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  fiscal_year SMALLINT NOT NULL,
  fiscal_month TINYINT NOT NULL,        -- 1-12（期首月起点）
  calendar_year SMALLINT NOT NULL,
  calendar_month TINYINT NOT NULL,
  start_date DATE NOT NULL,
  end_date DATE NOT NULL,
  label VARCHAR(20) NOT NULL,           -- 例: "FY2026-04"
  UNIQUE KEY uq_period_fy_fm (fiscal_year, fiscal_month)
) ENGINE=InnoDB;
-- マイグレーション/シードスクリプトでローリング約6年分を事前投入。

-- ===== ユーザー / セッション =====
CREATE TABLE app_user (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  email VARCHAR(255) NOT NULL,
  name VARCHAR(200) NOT NULL,
  role ENUM('field_user','office_admin') NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_user_email (email)
) ENGINE=InnoDB;

CREATE TABLE user_session (
  token_hash CHAR(64) PRIMARY KEY,      -- CookieトークンのSHA-256（生トークンは保存しない）
  user_id BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TIMESTAMP NOT NULL,
  last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_address VARCHAR(45),
  user_agent VARCHAR(255),
  FOREIGN KEY (user_id) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE TABLE user_assignment (   -- 現場担当者が担当する事業×部門
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE KEY uq_assignment (user_id, business_id, department_id),
  FOREIGN KEY (user_id) REFERENCES app_user(id),
  FOREIGN KEY (business_id) REFERENCES dim_business(id),
  FOREIGN KEY (department_id) REFERENCES dim_department(id)
) ENGINE=InnoDB;

-- ===== シナリオ版管理（予算/見込バージョン/実績） =====
CREATE TABLE scenario_version (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  scenario_type ENUM('budget','forecast','actual') NOT NULL,
  fiscal_year SMALLINT NOT NULL,
  as_of_period_id BIGINT UNSIGNED NULL,   -- 見込の起点月。予算/実績はNULL
  version_label VARCHAR(100) NOT NULL,
  status ENUM('draft','submitted','locked') NOT NULL DEFAULT 'draft',
  is_current BOOLEAN NOT NULL DEFAULT FALSE, -- (type, fy)ごとに1件のみ、アプリ層で担保
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  submitted_at TIMESTAMP NULL,
  locked_at TIMESTAMP NULL,
  FOREIGN KEY (as_of_period_id) REFERENCES dim_period(id),
  FOREIGN KEY (created_by) REFERENCES app_user(id),
  INDEX idx_scenario_lookup (scenario_type, fiscal_year, is_current)
) ENGINE=InnoDB;

-- ===== ファクトテーブル =====
CREATE TABLE fact_amount (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  account_id BIGINT UNSIGNED NOT NULL,
  period_id BIGINT UNSIGNED NOT NULL,      -- 金額が対象とする期間
  amount DECIMAL(18,2) NOT NULL,
  source_type ENUM('manual_entry','sheet_binding','csv_import') NOT NULL,
  submission_id BIGINT UNSIGNED NULL,      -- manual_entry/sheet_bindingで設定
  import_batch_id BIGINT UNSIGNED NULL,    -- csv_importで設定
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_fact_dims (scenario_version_id, business_id, department_id, account_id, period_id),
  INDEX idx_fact_report (period_id, scenario_version_id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id),
  FOREIGN KEY (business_id) REFERENCES dim_business(id),
  FOREIGN KEY (department_id) REFERENCES dim_department(id),
  FOREIGN KEY (account_id) REFERENCES dim_account(id),
  FOREIGN KEY (period_id) REFERENCES dim_period(id)
) ENGINE=InnoDB;
-- 同一シート/期間の再提出はこのユニークキーへのUPSERTで処理する。

-- ===== 入力層：自由シート + セルバインディング =====
CREATE TABLE input_sheet (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(200) NOT NULL,
  sheet_snapshot LONGTEXT NOT NULL,   -- Univerワークブックのシリアライズ済みJSON
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (owner_user_id) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE TABLE input_binding (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  input_sheet_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(200) NOT NULL,
  range_sheet_name VARCHAR(100) NOT NULL,
  start_row INT NOT NULL, end_row INT NOT NULL,
  start_col INT NOT NULL, end_col INT NOT NULL,
  header_rows TINYINT NOT NULL DEFAULT 1,  -- 先頭何行が軸ラベルか
  header_cols TINYINT NOT NULL DEFAULT 1,  -- 先頭何列が軸ラベルか
  row_axis_dimension ENUM('account','period','business','department','none') NOT NULL,
  col_axis_dimension ENUM('account','period','business','department','none') NOT NULL,
  fixed_dimensions JSON NOT NULL,   -- 例: {"business_id":3,"department_id":7,"scenario_type":"forecast"}
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_by BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (input_sheet_id) REFERENCES input_sheet(id),
  FOREIGN KEY (created_by) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE TABLE input_binding_axis_label (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  binding_id BIGINT UNSIGNED NOT NULL,
  axis ENUM('row','col') NOT NULL,
  axis_index INT NOT NULL,                 -- 範囲内の0始まり位置
  raw_label_text VARCHAR(255) NOT NULL,    -- バインディング定義時点のラベル（形状チェック用）
  resolved_dimension_type ENUM('account','period','business','department') NOT NULL,
  resolved_dimension_id BIGINT UNSIGNED NOT NULL,
  UNIQUE KEY uq_axis_label (binding_id, axis, axis_index),
  FOREIGN KEY (binding_id) REFERENCES input_binding(id)
) ENGINE=InnoDB;

CREATE TABLE submission (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  input_sheet_id BIGINT UNSIGNED NOT NULL,
  binding_id BIGINT UNSIGNED NOT NULL,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  submitted_by BIGINT UNSIGNED NOT NULL,
  submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  status ENUM('draft','submitted','superseded') NOT NULL DEFAULT 'submitted',
  validation_status ENUM('ok','warning','error') NOT NULL DEFAULT 'ok',
  validation_detail JSON NULL,   -- 形状チェックの不一致内容
  FOREIGN KEY (input_sheet_id) REFERENCES input_sheet(id),
  FOREIGN KEY (binding_id) REFERENCES input_binding(id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id),
  FOREIGN KEY (submitted_by) REFERENCES app_user(id)
) ENGINE=InnoDB;

CREATE TABLE submission_scope (   -- 提出状況ダッシュボード用：どの事業×部門をカバーする提出か
  submission_id BIGINT UNSIGNED NOT NULL,
  business_id BIGINT UNSIGNED NOT NULL,
  department_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (submission_id, business_id, department_id),
  FOREIGN KEY (submission_id) REFERENCES submission(id)
) ENGINE=InnoDB;

-- ===== 実績取り込み =====
CREATE TABLE import_batch (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  uploaded_by BIGINT UNSIGNED NOT NULL,
  original_filename VARCHAR(255) NOT NULL,
  file_size_bytes INT NOT NULL,
  scenario_version_id BIGINT UNSIGNED NOT NULL,
  status ENUM('processing','completed','failed') NOT NULL DEFAULT 'processing',
  row_count INT NOT NULL DEFAULT 0,
  error_count INT NOT NULL DEFAULT 0,
  error_detail JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  FOREIGN KEY (uploaded_by) REFERENCES app_user(id),
  FOREIGN KEY (scenario_version_id) REFERENCES scenario_version(id)
) ENGINE=InnoDB;

-- ===== 監査ログ =====
CREATE TABLE audit_log (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NULL,
  action VARCHAR(100) NOT NULL,
  entity_type VARCHAR(100) NOT NULL,
  entity_id BIGINT UNSIGNED NULL,
  detail JSON NULL,
  ip_address VARCHAR(45),
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_audit_entity (entity_type, entity_id)
) ENGINE=InnoDB;
```

**TiDB移植性メモ**：FKは宣言するが、TiDBのFK実装は歴史的に制約が多い（`ON UPDATE CASCADE`の版差異など）ため、参照整合性はGoサービス層でも担保する。トリガー・ストアドプロシージャは使用しない。`AUTO_INCREMENT`はTiDBのシャード採番により厳密な連番にならない前提でロジックを組む。JSON列・生成列はどちらでも問題ない。

## フェーズ分け

| フェーズ | スコープ | 明示的に除外 |
|---|---|---|
| **0 — 土台構築** | モノレポ構成、Goモジュール、Viteアプリ、goose移行、docker compose（MySQL/backend/frontendの3サービス）によるコンテナ開発環境、セッションログイン骨格、CI（両サイドのlint+test）、ヘルスチェック | 業務ロジック全般 |
| **1 — スキーマと予実差異の価値検証（スプレッドシートなし）** | ディメンションマスタCRUD（事務局）、素朴なHTMLフォーム入力（Univer未使用）で`fact_amount`に`source_type='manual_entry'`として直接書き込み、`scenario_version`作成/一覧、予実差異レポートAPI＋基本テーブルUI（事業/部門/勘定科目/期間でフィルタ） | スプレッドシートUI、バインディング、CSV取り込み、提出状況ダッシュボード |
| **2 — 入力層** | Univer.js組み込み、`input_sheet`永続化、バインディング定義ウィザード（範囲選択・行列軸・固定ディメンション）、`fact_amount`を生成する提出パイプライン、再提出時の最小限の形状チェック | バインディング範囲内の数式サポート、書式要件、完全な変更検知/アラート |
| **3 — 実績取り込み・レポート強化** | セキュリティを固めたCSV/XLSXアップロード、会計エクスポート向け列マッピングウィザード、`import_batch`トラッキング、予実差異レポートのドリルダウン | 会計システムとの直接API連携 |
| **4 — 運用面** | 提出状況ダッシュボード（`user_assignment`＋`submission_scope`）、事務局向けバインディング検証可視化、監査ログビューア | 多段階承認ワークフロー |

**フェーズ順の理由**：Phase 1は、最も後戻りコストの高い部分（ディメンションモデル＋予実差異集計が正しく・有用か）を、リスクの高いUniver.js統合に投資する前に、使い捨ての簡易入力UIで検証する。Phase 1と2は同じ`fact_amount`/`scenario_version`コアを共有するため、Phase 1の成果は無駄にならず、簡易フォームはPhase 2以降も事務局側の手動入力・フォールバック経路として残る。

## プロジェクト構成

```
f-panda-app/
  backend/
    cmd/server/main.go
    internal/
      auth/            セッションCookie管理、CSRF、ミドルウェア
      dimension/        事業/部門/勘定科目CRUD
      period/
      scenario/          scenario_version + 見込バージョニングロジック
      fact/               fact_amount書き込み、予実差異クエリサービス
      inputsheet/       シート・バインディング・提出・形状チェック
      importer/           CSV/XLSXパース、セキュリティ制御
      audit/
      httpapi/           net/httpマルチプレクサ、ルーティング、DTO、ミドルウェアチェーン
      config/
      db/                sqlc生成コード
    db/
      migrations/        gooseの.sqlファイル
      queries/            sqlc入力用.sql
      sqlc.yaml
    web/dist/            （生成物）ビルド済みSPAアセット、go:embed対象
    go.mod
  frontend/
    src/
      features/{dimensions,entry,sheet,variance,dashboard,auth}/
      components/
      api/                型付きfetchクライアント
      lib/univer/       Univer.js統合ラッパー
      routes.tsx
    vite.config.ts        開発時は /api を :8080 にプロキシ
    package.json
  docker-compose.yml     mysql / backend / frontend の全サービスをまとめて起動
  backend/Dockerfile       マルチステージ（devステージ: air等でホットリロード / prodステージ: distroless実行イメージ）
  frontend/Dockerfile      devステージ（vite devサーバー）
  Makefile                  migrate, sqlc generate, dev, build
  .github/workflows/ci.yml
```

**開発環境はコンテナベースとする**：ローカルにGo/Node/MySQLを直接インストールする前提を置かず、`docker compose up`（`make dev`のエイリアス）でMySQL・Go API（ホットリロード）・Vite開発サーバーの3サービスを一括起動する。ホストにはDocker/Docker Composeのみあればよい。

**本番ビルド**：`vite build`で`frontend/dist`を生成→`backend/web/dist`にコピー→`cmd/server`内の`//go:embed web/dist`でGoバイナリに埋め込む。GoバイナリがAPI（`/api/*`）とSPA（それ以外はindex.htmlへフォールバック）の両方を配信する。本番の配布物は**このGoバイナリを含むDockerイメージ**（`backend/Dockerfile`の`prod`ステージ、distroless等の最小ベースイメージ）とし、コンテナオーケストレーション基盤（AWS ECS Fargate等、東京リージョン想定）にデプロイする想定とする。実行時はコンテナ＋MySQL（RDS等）のみで完結する。

## 主要なアーキテクチャ判断

- **ルーター**：標準ライブラリの`net/http`（Go 1.22+の強化されたServeMux）で、このAPIのフラットなREST構成には十分。認証・CSRF・ロギング・panicリカバリは自前の`http.Handler`ラッパーとして実装し、フレームワークは導入しない。
- **認証/セッション**：HttpOnly/Secure/SameSite=Laxクッキーに不透明なランダムトークンを格納。DBには`user_session`に`SHA-256(トークン)`のみを保存し、生トークンは保存しない（DB読み取りだけではセッションを乗っ取れない）。CSRFはダブルサブミットクッキー方式：非HttpOnlyの補助クッキーにCSRFトークンを持たせ、SPAが更新系リクエストで`X-CSRF-Token`ヘッダーとして返し、ミドルウェアで両者を照合する。
- **スプレッドシートエンジン**：Univer.js（`@univerjs/presets`のsheets限定バンドルでdoc/slideの肥大化を回避）を第一候補とする。canvasレンダリングで大きめのシートにも対応でき、開発も活発、V2で数式エンジンが必要になっても拡張できる。代替候補はFortune-Sheet（DOMベースで軽量、バインディングウィザード用のセルテキスト抽出がしやすい反面、開発の勢いはUniverに劣る）。判断ルール：Phase 2着手時に3〜5日のスパイクでバインディングウィザードの中核（選択範囲座標の取得、セルごとのテキスト、ワークブック状態のJSONシリアライズ）を両方で試作し、致命的な統合上の問題がない限りUniverを採用する。
- **アップロードファイルのセキュリティ（Go）**：`http.MaxBytesReader`でパース前に上限（例：20MB）を設定。宣言されたcontent-type/拡張子に加えてマジックバイト判定（XLSX＝ZIP署名`PK\x03\x04`）を行う。XLSXパースは実績のあるライブラリ（例：`excelize`）を使用し、XML外部実体参照が解決されないことを確認する（Goの`encoding/xml`は既定で外部実体を展開しないが、ライブラリ側の独自処理がないか要確認）。zip爆弾対策として展開後の累積バイト数とエントリ数に上限を設け、パース処理にタイムアウトを設定する。CSVフォーミュラインジェクションは主に「エクスポート」側のリスク（`=`/`+`/`-`/`@`で始まるセルをExcelが実行してしまう）であり、MVPにはエクスポート機能がないが、将来のエクスポート機能実装時の必須要件としてここに明記しておく。
- **マイグレーション**：goose を採用（golang-migrateではなく）。素の`.sql`ファイルで完結でき、標準ライブラリ志向と相性が良い。当初は`dim_period`のローリング年度投入もgoose自体のGoマイグレーション機能で行う想定だったが、実装時に方針転換：goose CLIバイナリは`.sql`しか実行できず、Goマイグレーションを使うには別途カスタムマイグレーションバイナリを組む必要があると分かった。`dim_period`の投入は「一度きりのスキーマ変更」ではなく「年月が経つごとに窓をずらして再実行する運用タスク」なので、そもそもマイグレーションの形にそぐわない。そのため独立した冪等（UPSERT）なCLI（`backend/cmd/seedperiods`）として実装し、goose本体は`.sql`マイグレーションのみを扱う素直な使い方に統一した。
- **テスト戦略**：Go側はロジック単体（予実差異計算、形状チェックの比較ロジック）＋sqlcクエリの実DB（CI上のdocker-compose MySQL）結合テスト。フロント側はVitest/RTLのコンポーネントテスト＋ログイン→入力→予実差異表示のクリティカルパスに対する小規模なPlaywrightスモークテスト。API型はMVPの規模ではOpenAPIコード生成を省略し、`frontend/src/api`で手動管理する。

## 検証計画

- **Phase 0**：`make dev`でGo+MySQLが起動、空DBからgooseのマイグレーションが通る、`/healthz`が200を返す、curlでのログインがセッションCookie＋CSRFトークンを往復させて保護エンドポイントに到達できる、些末なPRでCIがグリーンになる。
- **Phase 1**：事務局管理者がUI経由でディメンションマスタをCRUDでき変更が永続化される。フィクスチャデータ（事業3×部門2×勘定科目5×月3）を簡易フォームから入力し、予実差異レポートの数値が手計算した期待値と一致することを自動結合テストで確認する。予算/見込/実績がブラウザ上で並列表示される。
- **Phase 2**：現場担当者がシートを作成しバインディングを定義して提出し、結果の`fact_amount`行がシートのグリッド値＋バインディング設定から算出した値と厳密に一致することを結合テストで確認する。構造を変えず値だけ変更しての再提出がUPSERTで正しく処理される。バインディング済みの行/列ラベルを変更して再提出すると、検証警告/エラーが表示されることを形状チェックのテストで確認する。
- **Phase 3**：サンプルの会計エクスポートをアップロードして正しい`actual`ファクト行が生成され予実差異レポートに反映される。オーバーサイズファイル・content-type/マジックバイト不一致・意図的に悪意あるXLSXフィクスチャ（zip爆弾相当）が、時間/メモリの上限内で正しく拒否されることを自動テストで確認する。集計からディメンション組み合わせへのドリルダウンが正しい明細行を返す。
- **Phase 4**：`user_assignment`5件（うち3件提出済み）のフィクスチャに対しダッシュボードが提出/未提出を正しく判定する。事務局管理者が提出のバリデーション状態を確認できる。Phase 1〜3で発生させた各操作について`audit_log`に対応する行がトレース可能であることをアサートする。

### 実装の重要ファイル
- `backend/db/migrations/`（上記DDLを実装する初期スキーママイグレーション）
- `backend/internal/fact/`（予実差異クエリサービス — Phase 1の価値検証の中核）
- `backend/internal/inputsheet/`（バインディング定義・提出・形状チェックロジック — 差別化機能の中核）
- `backend/internal/importer/`（セキュリティを固めたCSV/XLSX取り込み）
- `frontend/src/lib/univer/`（スプレッドシートエンジン統合ラッパー）

## 開発運用方針

- **リモートリポジトリ**：`git@github.com:rnapyzz/f-panda-app.git`（既存のoriginを使用）。
- **計画ドキュメントの保存場所**：この実装計画は`docs/plan.md`としてリポジトリにコミットし、Phase 0の一部として最初にコミットする。フェーズが進み設計が変わった場合はこのファイルも合わせて更新する。
- **README.md**：セットアップ手順（`make dev`での起動、docker-composeでのMySQL起動、環境変数）、開発時のビルド/テストコマンド、本番ビルド手順（`vite build`→Go埋め込み→`go build`）を記載する。Phase 0のスキャフォールディング時点で最小限のREADMEを用意し、以降のフェーズで随時更新する。
- **コミット/ブランチ運用**：
  - 意味のある単位（1機能・1タスク完了ごと）でこまめにコミットする。作業をまとめて1つの巨大なコミットにしない。
  - フェーズ内で複数のタスクに分かれる場合や、mainを安定させたまま作業したい場合は、タスク単位でブランチを切り、完了したらPRを作成してmainにマージする。小さな修正やドキュメント更新など、被害範囲が小さく後戻りしやすいものはmain直接コミットでも可。
  - 各フェーズの区切り（本計画のPhase 0〜4）では、そのフェーズの「検証計画」の基準を満たした時点で区切りのコミット/マージを行う。
