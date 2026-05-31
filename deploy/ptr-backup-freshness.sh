#!/bin/sh
# ptr-backup-freshness.sh — catches the "backup didn't run at all" failure mode
# (drive unmounted, timer disabled, box was off at trigger). Pushes an ntfy alert
# if the newest DB backup is missing or older than the threshold. Run daily by
# ptr-backup-freshness.timer, a few hours after the backup window.
set -eu

DEST="${DEST:-/mnt/backup/ptr}"
DBNAME="${DBNAME:-kh222}"          # backup filename stem (matches ptr-backup.sh)
MAX_AGE_HOURS="${MAX_AGE_HOURS:-26}"
. /etc/ptr-alert.env               # provides NTFY_URL

alert(){ curl -fsS -m 15 -H "Title: $1" -H "Priority: high" -H "Tags: $2" -d "$3" "$NTFY_URL" >/dev/null || true; }

newest="$(ls -t "$DEST/$DBNAME"-*.db 2>/dev/null | head -1 || true)"
if [ -z "$newest" ]; then
  alert "ptr1 backup MISSING" rotating_light "No $DBNAME-*.db backup found in $DEST on ptr1 — backups are not landing."
  exit 0
fi

age=$(( $(date +%s) - $(stat -c %Y "$newest") ))
max=$(( MAX_AGE_HOURS * 3600 ))
if [ "$age" -gt "$max" ]; then
  alert "ptr1 backup STALE" warning "Newest backup is $(( age / 3600 ))h old ($newest) — exceeds ${MAX_AGE_HOURS}h. The daily DB backup may have stopped."
fi
