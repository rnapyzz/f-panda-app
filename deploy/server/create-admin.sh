#!/usr/bin/env bash
# FP&A の管理者を作る（docs/deploy.md「2. 最初の構築」）。Session Manager で入り、root で実行する。
#
#   sudo /opt/fpanda/create-admin.sh <メールアドレス> <氏名>
#
# パスワード（12文字以上）は画面に出さずに入力する。
set -euo pipefail
cd "$(dirname "$0")"

email="${1:?使い方: create-admin.sh <メールアドレス> <氏名>}"
name="${2:?使い方: create-admin.sh <メールアドレス> <氏名>}"
read -rs -p "パスワード（12文字以上）: " pw
echo
printf '%s\n' "$pw" | docker compose -f compose.prod.yaml run --rm -T migrate createuser -email "$email" -name "$name" -role fpa_admin
