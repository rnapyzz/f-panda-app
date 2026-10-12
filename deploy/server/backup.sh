#!/usr/bin/env bash
# DB のダンプ（docs/deploy.md「5. バックアップと復旧」）。systemd のタイマー（fpanda-backup.timer）で毎日 3:00 に動く。
# /opt/fpanda/backup/ に 7日分を残す。ディスクごと EBS のスナップショット（毎日 4:00）にも含まれる。
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p backup
chmod 700 backup
file="backup/fpanda-$(date +%Y%m%d-%H%M).sql.gz"
# パスワードはコンテナの環境変数から読む（コマンドラインに出さない）
docker compose -f compose.prod.yaml exec -T db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --routines --triggers --databases fpanda' | gzip >"$file.tmp"
mv "$file.tmp" "$file"
find backup -name 'fpanda-*.sql.gz' -mtime +7 -delete
echo "ダンプを取りました: $file"
