#!/usr/bin/env bash
# 開発環境（make up、http://localhost:8080）のデータをすべて消し、デモのデータ（docs/demo.md）を入れ直す。
#
#   scripts/dev-demo.sh           確認してから実行する
#   FORCE=1 scripts/dev-demo.sh   確認しない
#
# 消すのはこの PC の開発環境の Docker のボリュームだけ（docker compose down -v）。別の URL や DB は指定できない。
# 研修・試用の環境（別のスタック、18100）は scripts/demo.sh。
set -euo pipefail
cd "$(dirname "$0")/.."

# 研修用・E2E のスタックの設定を引き継がず、必ず開発環境を対象にする
unset COMPOSE_PROJECT_NAME APP_PORT DB_PORT_HOST APP_BASE_URL DEMO_URL
URL="http://localhost:8080"
ADMIN_EMAIL="demo-admin@example.com"
ADMIN_PASSWORD="demo-admin-password"

if [ "${FORCE:-}" != "1" ]; then
  printf '開発環境（%s）のデータをすべて消して、デモのデータを入れます。よろしいですか（y/N）: ' "$URL"
  read -r answer
  case "$answer" in
    y | Y | yes) ;;
    *) echo "やめました"; exit 1 ;;
  esac
fi

docker compose down -v
docker compose up --build -d
echo "起動を待っています（${URL}）..."
ready=""
for _ in $(seq 1 60); do
  if curl -fsS "${URL}/api/health" 2>/dev/null | grep -q '"database":"ok"' && curl -fsS "${URL}/" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
if [ -z "$ready" ]; then
  echo "開発環境が起動しませんでした（docker compose logs で確かめてください）" >&2
  exit 1
fi

printf '%s\n' "$ADMIN_PASSWORD" | docker compose run --rm -T migrate createuser -email "$ADMIN_EMAIL" -name "デモ FP&A" -role fpa_admin
# 施策が1件でもある環境には入れない（scripts/demo.sh seed の確認）
DEMO_URL="$URL" DEMO_ADMIN_EMAIL="$ADMIN_EMAIL" DEMO_ADMIN_PASSWORD="$ADMIN_PASSWORD" scripts/demo.sh seed

cat <<MSG

開発環境にデモのデータを入れました: ${URL}
  FP&A: ${ADMIN_EMAIL} / ${ADMIN_PASSWORD}
  マネージャー: sato@example.com、担当者: suzuki@example.com・tanaka@example.com、閲覧者（経営陣）: yamada@example.com
  （FP&A 以外のパスワードは demo-member-password）
MSG
