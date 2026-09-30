#!/usr/bin/env bash
# E2E テストを、開発環境とは別の専用スタック（プロジェクト名 fpanda-e2e）で実行する。
#   scripts/e2e.sh            起動 → テスト → 停止（データも削除）
#   KEEP=1 scripts/e2e.sh     テスト後もスタックを残す（http://localhost:18080）
#   scripts/e2e.sh --headed   Playwright の引数をそのまま渡せる
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME=fpanda-e2e
export APP_PORT="${E2E_PORT:-18080}"
export DB_PORT_HOST="${E2E_DB_PORT:-13307}"
export E2E_BASE_URL="http://localhost:${APP_PORT}"
export E2E_EMAIL="${E2E_EMAIL:-e2e-admin@example.com}"
export E2E_PASSWORD="${E2E_PASSWORD:-e2e-admin-password}"

cleanup() {
  if [ "${KEEP:-}" != "1" ]; then
    docker compose down -v >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

docker compose up --build -d
echo "waiting for ${E2E_BASE_URL}/api/health ..."
for _ in $(seq 1 60); do
  if curl -fsS "${E2E_BASE_URL}/api/health" 2>/dev/null | grep -q '"database":"ok"' && curl -fsS "${E2E_BASE_URL}/" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

# テスト用の管理者（既にいれば作成エラーを無視する）
printf '%s\n' "$E2E_PASSWORD" | docker compose run --rm -T migrate createuser -email "$E2E_EMAIL" -name "E2E 管理者" -role fpa_admin || true

cd web
npx playwright test "$@"
