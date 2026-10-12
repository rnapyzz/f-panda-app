#!/usr/bin/env bash
# 本番のデプロイ（docs/deploy.md「3. リリース」）。EC2 の /opt/fpanda/ で root として動かす。
#
#   deploy.sh <タグ>     ECR のイメージ <タグ>（コミットのハッシュ）にする。マイグレーション → 入れ替え → 確認
#   deploy.sh rollback   直前のタグに戻す（マイグレーションは実行しない。マイグレーションは追加だけなので、前のイメージでも動く）
#
# GitHub Actions からは SSM の Run Command で、ECR_REGISTRY・AWS_REGION（と、初回だけ APP_DOMAIN・ACME_EMAIL）を付けて呼ぶ。
# .env がなければ、DB のパスワードを生成して作る（権限 600）。.env はリポジトリにも GitHub にも置かない。
set -euo pipefail
cd "$(dirname "$0")"

ENV_FILE=.env
COMPOSE=(docker compose -f compose.prod.yaml)

log() { echo "[deploy $(date '+%H:%M:%S')] $*"; }
get_var() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | tail -n1 | cut -d= -f2- || true; }
set_var() {
  local tmp
  tmp="$(mktemp "$ENV_FILE.XXXXXX")"
  awk -v k="$1" -v v="$2" 'index($0, k "=") == 1 { print k "=" v; done = 1; next } { print } END { if (!done) print k "=" v }' "$ENV_FILE" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

# .env を用意する（初回だけ作る。その後は ECR_REGISTRY・AWS_REGION だけを最新にする）
if [[ ! -f "$ENV_FILE" ]]; then
  : "${APP_DOMAIN:?初回は APP_DOMAIN（例: fpanda.example.com）が必要です。GitHub の Variables に入れてください}"
  : "${ACME_EMAIL:?初回は ACME_EMAIL（証明書の連絡先のメールアドレス）が必要です。GitHub の Variables に入れてください}"
  umask 077
  cat >"$ENV_FILE" <<EOF
# 本番の設定と秘密情報（docs/deploy.md）。権限 600。リポジトリには入れない
APP_DOMAIN=${APP_DOMAIN}
ACME_EMAIL=${ACME_EMAIL}
MYSQL_ROOT_PASSWORD=$(openssl rand -hex 24)
MYSQL_PASSWORD=$(openssl rand -hex 24)
EOF
  log ".env を作りました（DB のパスワードを生成）"
fi
chmod 600 "$ENV_FILE"
[[ -n "${ECR_REGISTRY:-}" ]] && set_var ECR_REGISTRY "$ECR_REGISTRY"
[[ -n "${AWS_REGION:-}" ]] && set_var AWS_REGION "$AWS_REGION"
ECR_REGISTRY="$(get_var ECR_REGISTRY)"
AWS_REGION="$(get_var AWS_REGION)"
: "${ECR_REGISTRY:?ECR_REGISTRY がありません}"
: "${AWS_REGION:?AWS_REGION がありません}"

current="$(get_var IMAGE_TAG)"
previous="$(get_var PREVIOUS_TAG)"
migrate=true
case "${1:-}" in
  "") echo "使い方: deploy.sh <タグ> | rollback" >&2; exit 2 ;;
  rollback)
    tag="$(get_var PREVIOUS_TAG)"
    [[ -n "$tag" ]] || { echo "戻す先のタグ（PREVIOUS_TAG）がありません" >&2; exit 1; }
    migrate=false
    ;;
  *) tag="$1" ;;
esac
log "デプロイ: ${current:-（なし）} → ${tag}"

# 新しいイメージを取得する（ここで失敗しても、動いているものは変わらない）
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_REGISTRY" >/dev/null
export IMAGE_TAG="$tag"
"${COMPOSE[@]}" pull --quiet api web caddy db

# マイグレーション（失敗したら入れ替えない）
"${COMPOSE[@]}" up -d --wait db
if $migrate; then
  log "マイグレーション"
  "${COMPOSE[@]}" run --rm -T migrate
fi

# 入れ替える
set_var IMAGE_TAG "$tag"
[[ -n "$current" && "$current" != "$tag" ]] && set_var PREVIOUS_TAG "$current"
"${COMPOSE[@]}" up -d --remove-orphans

# 確認: nginx（web）経由で /api/health が 200 を返すまで待つ。返らなければ前のタグに戻す
healthy=false
for _ in $(seq 1 30); do
  if "${COMPOSE[@]}" exec -T web wget -q -O /dev/null http://127.0.0.1/api/health 2>/dev/null; then
    healthy=true
    break
  fi
  sleep 2
done
if ! $healthy; then
  log "確認に失敗しました。api のログ:"
  "${COMPOSE[@]}" logs --tail 50 api || true
  if [[ -n "$current" && "$current" != "$tag" ]]; then
    log "前のタグ ${current} に戻します"
    set_var IMAGE_TAG "$current"
    [[ -n "$previous" ]] && set_var PREVIOUS_TAG "$previous"
    export IMAGE_TAG="$current"
    "${COMPOSE[@]}" up -d --remove-orphans
  fi
  exit 1
fi

# 使っていないイメージを消す（戻すときは ECR から取り直す）
docker image prune -af >/dev/null
log "完了: ${tag}"
