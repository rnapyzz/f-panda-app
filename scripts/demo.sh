#!/usr/bin/env bash
# 研修・試用の環境（docs/demo.md、I-28）。開発環境とは別のスタック（プロジェクト名 fpanda-demo）で起動し、デモのデータを入れる。
#
#   scripts/demo.sh            起動（初回はデモのデータを入れる）。http://localhost:18100
#   scripts/demo.sh down       止めて、データも消す
#   scripts/demo.sh reset      データを消して、入れ直す（研修の前に初期の状態へ戻す）
#   scripts/demo.sh seed       起動済みの環境に、デモのデータだけを入れる（DEMO_URL で別の環境も指定できる）
#
# 外への通知（Slack）は送らない。施策が1件でもある環境には、デモのデータを入れない。
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME=fpanda-demo
export APP_PORT="${DEMO_PORT:-18100}"
export DB_PORT_HOST="${DEMO_DB_PORT:-13308}"
export SLACK_WEBHOOK_URL=""
export APP_BASE_URL="http://localhost:${APP_PORT}"
URL="${DEMO_URL:-http://localhost:${APP_PORT}}"
ADMIN_EMAIL="${DEMO_ADMIN_EMAIL:-demo-admin@example.com}"
ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD:-demo-admin-password}"

seed() {
  echo "デモのデータを入れています（${URL}）..."
  (cd web && E2E_BASE_URL="$URL" E2E_EMAIL="$ADMIN_EMAIL" E2E_PASSWORD="$ADMIN_PASSWORD" npx playwright test --config=playwright.demo.config.ts --reporter=line)
}

up() {
  docker compose up --build -d
  echo "起動を待っています（${URL}）..."
  for _ in $(seq 1 60); do
    if curl -fsS "${URL}/api/health" 2>/dev/null | grep -q '"database":"ok"' && curl -fsS "${URL}/" >/dev/null 2>&1; then break; fi
    sleep 2
  done
  # FP&A のアカウント（既にいれば作らない）
  printf '%s\n' "$ADMIN_PASSWORD" | docker compose run --rm -T migrate createuser -email "$ADMIN_EMAIL" -name "デモ FP&A" -role fpa_admin >/dev/null 2>&1 || true
  # 施策がなければ、デモのデータを入れる
  cookie="$(mktemp)"
  curl -fs -c "$cookie" -H 'Content-Type: application/json' -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" "$URL/api/auth/login" >/dev/null
  if curl -fs -b "$cookie" "$URL/api/activities" | grep -q '"items":\[\]'; then
    seed
  else
    echo "データはそのままです（初期の状態に戻すときは scripts/demo.sh reset）"
  fi
  rm -f "$cookie"
  cat <<EOF

研修・試用の環境: ${URL}
  FP&A: ${ADMIN_EMAIL} / ${ADMIN_PASSWORD}
  マネージャー: sato@example.com、担当者: suzuki@example.com・tanaka@example.com、閲覧者（経営陣）: yamada@example.com
  （FP&A 以外のパスワードは demo-member-password）
止める: scripts/demo.sh down
EOF
}

case "${1:-up}" in
  up) up ;;
  down) docker compose down -v ;;
  reset) docker compose down -v && up ;;
  seed) seed ;;
  *) echo "使い方: scripts/demo.sh [up|down|reset|seed]" >&2; exit 1 ;;
esac
