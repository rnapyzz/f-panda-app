# 研修・試用の環境（I-28）

現場に使い方を覚えてもらう研修や、新しい機能を試すための環境。本番とは別のデータ（デモのデータ）で動かすので、練習の変更履歴や通知が本番に残らない。

## 起動と片付け

```bash
make demo        # 起動（初回はデモのデータを入れる）。http://localhost:18100
make demo-reset  # デモのデータの初期の状態に戻す（研修の前に）
make demo-down   # 止めて、データも消す
```

- 開発環境（`make up`、8080）とは別のスタック（`fpanda-demo`、ポート 18100・DB 13308）で動く。開発環境と同時に起動できる。
- Slack には送らない（アプリ内のお知らせは届く）。
- 研修では、この PC の画面を共有するか、同じネットワークの PC から `http://<この PC のアドレス>:18100` を開く。

## 開発環境にデモのデータを入れる

開発中に、具体的なデータで画面を確かめたいときは、開発環境（`make up`、8080）をデモのデータで作り直せる。

```bash
make dev-demo          # 確認のあと、開発環境のデータをすべて消して、デモのデータを入れる
FORCE=1 make dev-demo  # 確認しない
```

- 消すのはこの PC の開発環境の Docker のボリューム（DB）だけ。開発環境で作ったアカウント・データ・計測用の DB（`make perf`）も消える。
- アカウントは下の表と同じ（FP&A は demo-admin@example.com）。
- 研修・試用の環境（18100）とは別。研修には `make demo` を使う。

## アカウント

| ロール | メールアドレス | パスワード |
| --- | --- | --- |
| FP&A | demo-admin@example.com | demo-admin-password |
| マネージャー | sato@example.com（佐藤 健） | demo-member-password |
| 担当者 | suzuki@example.com（鈴木 花子）、tanaka@example.com（田中 一郎） | demo-member-password |
| 閲覧者（経営陣） | yamada@example.com（山田 誠） | demo-member-password |

研修用の値で、本番では使わない。

## デモのデータ

- 2026年度。期初計画 → 9月見込 → 10月見込（今回の見込。締切 2026-10-16）。2026年4〜9月の実績を取り込み済み。
- 2つのユニット（SaaS・受託開発）に 10 件の施策（運用型・プロジェクト型・コストプール型、確度 A〜E、ダウンサイドの内訳、期日を過ぎたマイルストーンを含む）。
- 10月見込は更新の途中（「SaaS 月額プラン」は完了、ほかは未着手・入力中）。担当者で入って「説明して完了」まで練習できる。
- 中身は `web/screenshots/demo-data.ts`。手引きの画像（`make manual-screenshots`）も同じデータで撮る。日付は固定なので、年度が変わったら更新する。

## 研修の進め方の例

1. FP&A が `make demo-reset` で初期の状態にする。
2. 担当者のアカウント（鈴木）で入り、ホーム → 施策の「今回の更新」→ 数字を直す → 比較を見る → 説明して完了、を一緒にやる（[現場担当の手引き](../web/src/manual/member.md)）。
3. マネージャー（佐藤）でホームのカードと、施策の一覧の図を見る。
4. FP&A で「今月の作業」と催促を見る。

## 別の環境に入れるとき

研修用にサーバーを用意したときは、FP&A のアカウントを作ってから、デモのデータだけを入れられる。

```bash
DEMO_URL=https://training.example.com DEMO_ADMIN_EMAIL=... DEMO_ADMIN_PASSWORD=... scripts/demo.sh seed
```

- 施策が1件でもある環境には入れない（本番に誤って入れないため）。
- SSO（Google）を有効にした環境では、パスワードでログインできるのは FP&A だけ（docs/architecture.md「認証」）。デモのアカウント（example.com）で現場の練習をするときは、SSO を設定しない研修用の環境にする。
