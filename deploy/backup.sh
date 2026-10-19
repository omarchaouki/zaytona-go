#!/bin/sh
# Nightly backup: the database (pg_dump, custom format) and the uploaded photos.
# Install: sudo install -m 0750 deploy/backup.sh /usr/local/sbin/zaytona-backup
# Cron:    /etc/cron.d/zaytona-backup (see docs/DEPLOY_UBUNTU.md)
# Restore: pg_restore --clean --if-exists --no-owner --dbname="$DATABASE_URL" db-YYYY-MM-DD.dump
set -eu
umask 077

ENV_FILE=/etc/zaytona/zaytona.env
DEST=/var/backups/zaytona
UPLOADS=/var/lib/zaytona/uploads
KEEP_DAYS=14

# DATABASE_URL and the rest come from the app's environment file.
set -a
. "$ENV_FILE"
set +a

mkdir -p "$DEST"
day=$(date +%F)
pg_dump --format=custom --no-owner --file="$DEST/db-$day.dump" "$DATABASE_URL"
tar -C "$(dirname "$UPLOADS")" -czf "$DEST/uploads-$day.tar.gz" "$(basename "$UPLOADS")"

# Keep two weeks; copy the folder off the server too (rsync, rclone, ...).
find "$DEST" -type f \( -name 'db-*.dump' -o -name 'uploads-*.tar.gz' \) -mtime +"$KEEP_DAYS" -delete
