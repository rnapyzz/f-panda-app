# 本番環境の構築・リリース・復旧の手順（AWS）

構成と方針は [architecture.md](architecture.md) の「本番環境（AWS）」と「認証」を参照。趣味・小規模の利用を前提に、**月 2,000〜3,500 円程度**で動かす。**EC2 を1台**だけ使い、開発環境と同じ docker compose の構成（画面・API・MySQL）を載せる。**main にマージすると、GitHub Actions が自動でデプロイする**。

以降の例では、リージョンを `ap-northeast-1`（東京）、アプリの URL を `https://fpanda.example.com` とする。

## 1. 構成と費用

```
  ブラウザ ──HTTPS──▶ EC2（1台、Amazon Linux 2023、Arm）
                     ┌───────────────────────────────────────────┐
                     │ caddy（HTTPS の終端。Let's Encrypt の証明書を自動で取得・更新） │
                     │   └▶ web（nginx。ビルドした画面、/api を転送）                │
                     │        └▶ api（Go）──▶ db（MySQL 8.4、EBS のボリューム）      │
                     └───────────────────────────────────────────┘
  GitHub Actions ──(OIDC)──▶ ECR にイメージを置き、SSM で EC2 にデプロイを指示
  毎日: EBS のスナップショット（7日分）と、DB のダンプ（サーバー内、スナップショットに含まれる）
```

| リソース | 設定 | 費用の目安（月） |
| -------- | ---- | ---------------- |
| EC2 | `t4g.micro`（Arm、メモリ 1GB）＋スワップ 2GB で始める。遅ければ `t4g.small`（2GB）に変える（停止 → 種類の変更 → 起動の数分） | micro 約 $6〜7 / small 約 $13〜16 |
| パブリック IPv4（Elastic IP） | 固定の IP。DNS をこれに向ける | 約 $3.6 |
| EBS（gp3 20GB） | OS・Docker のイメージ・DB のデータ | 約 $2 |
| EBS のスナップショット | 毎日、7日分（増分だけ課金） | 約 $0.5〜1 |
| ECR | `fpanda-api`・`fpanda-web`。新しい 10 個だけを残す | 約 $0.1 |
| Route 53 | ホストゾーン | $0.5 |
| ドメイン | Route 53 で取る（`.com` など。年払い） | 年 $15 前後 |
| 合計 | | **micro で約 $14（2,000 円台）、small で約 $23（3,500 円前後）** |

- 価格は目安。作る前に [AWS の料金計算ツール](https://calculator.aws/) で確かめる。円安・価格改定で変わる。
- ロードバランサー（ALB）・NAT ゲートウェイ・RDS は使わない。どれも月に数千円かかるため。
- 割り切ること: サーバーは1台なので、デプロイの間は数秒〜十数秒つながらない。サーバーが壊れたら、スナップショットからの復元になる（最大で1日分のデータが戻る）。利用者が増えて止められなくなったら「7. 構成を大きくするとき」を参照。

### 構成のコード（CloudFormation）

AWS のリソースは `deploy/aws/fpanda.yaml`（CloudFormation のテンプレート）にまとめる。CloudFormation のコンソールでこのファイルを読み込むだけで作れる（手元にツールを入れなくてよい）。テンプレートで作るもの:

- セキュリティ グループ（受けるのは 80・443 だけ。SSH は開けない。サーバーの操作は Session Manager で行う）
- EC2 と Elastic IP。初回の起動時に Docker・docker compose を入れ、スワップを作る（ユーザーデータ）
- EC2 のロール: Session Manager（SSM）と ECR からの取得だけ
- ECR のリポジトリ 2 つ（ライフサイクル: 新しい 10 個を残す）
- EBS のスナップショットの自動作成（Data Lifecycle Manager。毎日、7日分）
- GitHub Actions の OIDC プロバイダーとデプロイ用のロール（このリポジトリの main ブランチからだけ使える。ECR への push・デプロイ用の S3 バケットへの書き込み・この EC2 への SSM のコマンドだけ）
- デプロイ用の S3 バケット（サーバーのファイルのまとめを置く。30日で消える。公開しない）
- Route 53 の A レコード（`fpanda.example.com` → Elastic IP）
- EC2 の状態のアラーム（ステータスチェックの失敗で自動復旧し、メールで知らせる）

### サーバーの中（`deploy/server/`）

| ファイル | 内容 |
| -------- | ---- |
| `compose.prod.yaml` | caddy・web・api・db・migrate。イメージは ECR のタグ（コミットのハッシュ）を `.env` の `IMAGE_TAG` で指定する |
| `Caddyfile` | `fpanda.example.com` で HTTPS を受け、`web` に転送する。HTTP は HTTPS へ転送する |
| `my.cnf` | 小さいメモリ向けの MySQL の設定（`innodb_buffer_pool_size = 128M`、`performance_schema = OFF` など） |
| `deploy.sh` | デプロイの手順（下の「3. リリース」）。前のタグに戻す `rollback` もできる |
| `backup.sh` | DB のダンプ（毎日 3:00。systemd のタイマー `fpanda-backup.timer`。7日分を残す） |
| `create-admin.sh` | FP&A の管理者を作る（パスワードは画面に出さずに入力） |

サーバーでは `/opt/fpanda/` に置く（デプロイのたびに、そのコミットのものに置き換わる）。秘密情報は `/opt/fpanda/.env`（権限 600）にだけ置き、リポジトリと GitHub には入れない。`.env` は最初のデプロイで `deploy.sh` が作る（DB のパスワードはその場で生成する）。

| `.env` の名前 | 値 |
| ------------- | -- |
| `APP_DOMAIN` | `fpanda.example.com` |
| `ACME_EMAIL` | 証明書の期限の連絡先のメールアドレス |
| `MYSQL_ROOT_PASSWORD` / `MYSQL_PASSWORD` | 最初のデプロイで生成する長いランダムな文字列。変えない（DB のデータと合わなくなる） |
| `ECR_REGISTRY` / `AWS_REGION` | イメージの取得先（`deploy.sh` が書く） |
| `IMAGE_TAG` / `PREVIOUS_TAG` | 動いているイメージのタグと、その前のタグ（`deploy.sh` が書き換える） |
| `SLACK_WEBHOOK_URL` | 使う場合だけ |
| `OIDC_*` | Google の SSO を使う場合だけ（「6. SSO を使う場合」） |

`api` には、このほか `COOKIE_SECURE=true`・`TRUST_PROXY=true`（caddy の後ろで動かすため）・`TZ=Asia/Tokyo`・`APP_BASE_URL=https://${APP_DOMAIN}` を `compose.prod.yaml` で渡す。

## 2. 最初の構築（1回だけ）

AWS のアカウントの作成・支払いの設定・ドメインの購入は、本人が行う（パスワード・カード番号を扱うため）。

1. **AWS のアカウント**を作り、ルート ユーザーに MFA を付ける。日々の作業用に IAM Identity Center（または IAM ユーザー）を作り、ルート ユーザーは使わない。
2. **予算のアラート**: Billing の「Budgets」で月 $30 の予算を作り、80% でメールが届くようにする（想定外の課金に気づくため）。
3. **ドメイン**: Route 53 の「ドメインの登録」で取る（例: `example.com`）。ホストゾーンは自動で作られる。
4. **CloudFormation**: コンソールで「スタックの作成」→「テンプレートファイルのアップロード」で `deploy/aws/fpanda.yaml` を選ぶ。スタックの名前は `fpanda`。パラメータ:

   | パラメータ | 入れる値 |
   | ---------- | -------- |
   | `DomainName` | アプリの URL のホスト名（例: `fpanda.example.com`） |
   | `HostedZoneId` | 3 で作られたホストゾーン（一覧から選ぶ） |
   | `InstanceType` | `t4g.micro`（既定）。遅ければ後で `t4g.small` に変える |
   | `AlertEmail` | サーバーの異常を知らせるメールアドレス |
   | `GitHubRepository` | `rnapyzz/f-panda-app`（既定） |
   | `CreateGitHubOidcProvider` | `true`（既定）。ほかの用途で GitHub の OIDC プロバイダーを作ってあれば `false` |

   「IAM リソースを作成する」ことの確認にチェックを入れて作る。10 分ほどで終わる。`AlertEmail` に届く確認メールのリンクを開く（開かないと知らせが届かない）。
5. **GitHub**: リポジトリの Settings → Secrets and variables → Actions の **Variables**（秘密情報ではない）に、次を入れる。AWS の鍵は GitHub に置かない（OIDC で一時的な権限を受け取る）。

   | 名前 | 値 |
   | ---- | -- |
   | `AWS_REGION` | `ap-northeast-1` |
   | `AWS_DEPLOY_ROLE_ARN` | スタックの出力（Outputs）の `DeployRoleArn` |
   | `AWS_INSTANCE_ID` | 出力の `InstanceId` |
   | `ECR_REGISTRY` | 出力の `EcrRegistry` |
   | `DEPLOY_BUCKET` | 出力の `DeployBucket` |
   | `APP_DOMAIN` | `fpanda.example.com` |
   | `ACME_EMAIL` | 証明書の期限の連絡先のメールアドレス |

   Variables が入るまでは、デプロイのワークフローは何もしない（失敗にもならない）。
6. **最初のデプロイ**: GitHub の Actions で「Deploy」のワークフローを開き、「Run workflow」で手動で実行する（10 分ほど）。サーバーの `.env` が作られ、アプリが起動する。以後は main へのマージで自動で動く。
7. **FP&A の管理者を作る**: EC2 のコンソールでインスタンスを選び「接続」→「Session Manager」で入り、次を実行する（パスワードを聞かれる）。

   ```bash
   sudo /opt/fpanda/create-admin.sh you@example.com 氏名
   ```

8. `https://fpanda.example.com` を開き、ログインできることを確かめる。初めて開いたときは証明書の取得に数十秒かかることがある。

### 構成を変えるとき

- **サーバーを大きくする**: CloudFormation のスタックを「更新」し、`InstanceType` を `t4g.small` に変える（停止 → 起動で数分止まる。IP・データはそのまま）。
- **`.env` を変える**（Slack・SSO など）: Session Manager で入り、`sudo vi /opt/fpanda/.env` で直してから `sudo /opt/fpanda/deploy.sh <今のタグ>`（今のタグは `.env` の `IMAGE_TAG`）で入れ替える。
- **やめる**: スタックを削除する。ディスク（EBS）は誤って消さないよう、スタックを消しても残る設定にしている。要らなければ EC2 の「ボリューム」と「スナップショット」から消す。ドメインは Route 53 で自動更新を止める。

## 3. リリース（自動）

`.github/workflows/deploy.yml`。main への push（PR のマージ）で、CI と同じテストが通った後に動く。手動でも実行できる。同時に2つは動かさない（後のものは前のものが終わるのを待つ）。

1. **イメージを作る**: `api`・`web`（`prod` ステージ）を `linux/arm64` で作り、タグをコミットのハッシュにして ECR に置く。ビルドは GitHub のランナー（x86）で行い、Go はクロス コンパイル、画面のビルド結果は CPU に依存しないので、Arm のエミュレーションは最後の段だけで済む。
2. **デプロイを指示する**: `deploy/server/` のファイルをまとめて S3 に置き、SSM の Run Command で EC2 に取得させてから `deploy.sh <タグ>` を実行する。`compose.prod.yaml`・`Caddyfile`・`my.cnf`・スクリプトも、そのコミットのものに置き換わる。
3. **`deploy.sh` の中**:
   1. ECR にログインし、新しいイメージを取得する
   2. `migrate up` を実行する。失敗したら、入れ替えずに終わる（前のイメージのまま動き続ける）
   3. `IMAGE_TAG` を書き換えて、`api`・`web` を入れ替える（`docker compose up -d`）
   4. `/api/health` が 200 を返すまで最大 60 秒待つ。返らなければ前のタグに戻して、失敗として終わる
   5. 古いイメージを消す（ディスクの節約）
4. **結果**: ワークフローの画面に `deploy.sh` の出力が出る。失敗したら GitHub からメールが届く。

マイグレーションは追加だけで作る（CLAUDE.md）ので、前のイメージに戻しても DB はそのままで動く。

### 手で戻すとき

Session Manager で入り、`sudo /opt/fpanda/deploy.sh rollback`（直前のタグに戻す）か `sudo /opt/fpanda/deploy.sh <タグ>` を実行する。タグは GitHub のコミットのハッシュ（40 文字）。ECR に残っている新しい 10 個から選べる。

## 4. 監視

- **EC2 のステータスチェック**: 失敗したら自動で復旧し（同じ IP・ディスクで起動し直す）、メールで知らせる（テンプレートで作る）。
- **アプリのログ**: Session Manager で入り、`cd /opt/fpanda && sudo docker compose -f compose.prod.yaml logs --tail 100 api` で見る。ログは Docker の設定で 1 コンテナ 10MB × 3 ファイルまでにする。CloudWatch Logs には送らない（費用を抑えるため）。
- **ディスク**: 四半期に1回、`df -h` と DB の大きさを確かめる。監査ログ（`audit_logs`）は消さない。

```sql
SELECT table_name, ROUND((data_length + index_length) / 1024 / 1024) AS mb
FROM information_schema.tables WHERE table_schema = 'fpanda' ORDER BY mb DESC LIMIT 10;
```

- **費用**: 予算のアラート（「2. 最初の構築」）。

## 5. バックアップと復旧

- **EBS のスナップショット**: 毎日、7日分を残す（Data Lifecycle Manager）。ディスク全体（DB のデータとダンプを含む）を戻せる。
- **DB のダンプ**: 毎日 3:00 に `mysqldump --single-transaction` を `/opt/fpanda/backup/` に取る（7日分）。スナップショットは動いている DB をそのまま写すので、ダンプも残しておくと確実に戻せる。
- **大きな変更の前**（年度の締め・組織変更など）: EC2 のコンソールで、手動でスナップショットを取る（手動のものは消えない）。

### 復旧の手順

- **データだけを戻す**（誤操作など）: Session Manager で入り、`/opt/fpanda/backup/` のダンプを読み込む（`cd /opt/fpanda && gzip -dc backup/<ファイル> | sudo docker compose -f compose.prod.yaml exec -T db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot'`）。戻した時点より後の変更は失われるので、変更履歴をもとに入力し直す。
- **サーバーごと戻す**（壊れた・消した）: スナップショットからボリュームを作ってインスタンスに付け替える。または、スナップショットから作った AMI で起動し、Elastic IP を付け替える。

### 復旧の練習

年に1回、スナップショットから別のインスタンスを起動し、ダンプを読み込んでアプリが開けることを確かめる。終わったら、そのインスタンスとボリュームを消す。

## 6. SSO を使う場合

既定では、パスワードでログインする（ユーザーは FP&A がアプリで登録し、パスワードを設定する）。Google Workspace の SSO を使う場合:

1. Google Cloud のコンソールでプロジェクトを作り、「OAuth 同意画面」をユーザーの種類「内部」で設定する。
2. 「認証情報」で OAuth クライアント ID（ウェブ アプリケーション）を作る。承認済みのリダイレクト URI は `https://fpanda.example.com/api/auth/oidc/callback`。
3. `.env` に `OIDC_ISSUER=https://accounts.google.com`・`OIDC_CLIENT_ID`・`OIDC_CLIENT_SECRET`・`OIDC_REDIRECT_URL`・`OIDC_ALLOWED_DOMAINS` を入れ、`sudo /opt/fpanda/deploy.sh <今のタグ>` で入れ替える（「構成を変えるとき」）。

SSO を有効にすると、パスワードでログインできるのは FP&A の非常用だけになる（architecture.md「認証」）。

## 7. 構成を大きくするとき

利用者が増え、止められない・データを失えない運用になったら、次の順で置き換える（アプリの変更は要らない）。

1. DB を RDS for MySQL に移す（自動バックアップ・時点復元）。`.env` の接続先を変える
2. ALB と ACM の証明書を置き、EC2 を2台以上にする（ECS Fargate に移す）
3. 証明書は ACM に、ログは CloudWatch Logs に移す

## 8. 開発環境との違い

| 項目 | 開発環境（`make up`） | 本番環境 |
| ---- | --------------------- | -------- |
| 画面 | Vite の開発サーバー | ビルドした画面を nginx で配信（`web/nginx.prod.conf`） |
| HTTPS | なし | caddy で終端（Let's Encrypt）。HSTS などのヘッダーは nginx で付ける |
| ログイン | パスワード | パスワード（SSO を設定すれば Google の SSO） |
| DB | compose の MySQL（ボリューム） | compose の MySQL（EBS。小さいメモリ向けの設定）。毎日のスナップショットとダンプ |
| マイグレーション | compose の `migrate` サービス | `deploy.sh` が入れ替えの前に実行 |
| リリース | — | main へのマージで GitHub Actions が自動でデプロイ |
