#!/usr/bin/env bash
# ptr-backup.sh — WAL-safe online backup of the PTR SQLite DB to the backup drive.
# Phase 5 (see WORKFLOW.md / PROJECT_HISTORY.md Phase 5). Runs as ptrapp via
# ptr-backup.service + ptr-backup.timer.
#
# Why an ONLINE backup and not `cp`: the DB runs in WAL mode with the webapp and
# the import timer attached. A raw `cp` of a live WAL database can capture a
# torn/inconsistent file (the -wal sidecar may hold committed pages not yet in
# the main file). sqlite3.Connection.backup() takes a transactionally consistent
# snapshot while readers/writers continue. There is NO sqlite3 CLI on ptr1
# (PHASE_1 §6), so we drive it from the always-present venv Python's stdlib.
#
# Path-driven by design: $DB defaults to the CURRENT live file (kh222.db) so the
# backup keeps working after the coordinated kh222.db -> pretrial_release.db
# rename — just change $DB (or the unit's Environment=) at rename time.
set -euo pipefail

DB="${DB:-/opt/ptr-knoxc/db/kh222.db}"            # live database (path-driven)
DEST="${DEST:-/mnt/backup/ptr}"                   # mount point of the backup drive
PYTHON="${PYTHON:-/opt/ptr-knoxc/venv/bin/python3}"
# Space-separated label:path pairs of config/secret files to snapshot beside the
# DB, so a bare-metal restore has the app + import settings, not just the data.
# Override via the unit's Environment= if paths change.
CONFIG_FILES="${CONFIG_FILES:-app.env:/opt/ptr-knoxc/webapp/.env import.env:/etc/ptr-import.env}"
MIGRATIONS="${MIGRATIONS:-/opt/ptr-knoxc/db/migrations}"
RETAIN_DAYS="${RETAIN_DAYS:-30}"

stamp="$(date +%F)"                               # YYYY-MM-DD
dbname="$(basename "$DB" .db)"
out="$DEST/${dbname}-${stamp}.db"

[ -x "$PYTHON" ] || { echo "FATAL: python not found/executable: $PYTHON" >&2; exit 1; }
[ -f "$DB" ]     || { echo "FATAL: DB not found: $DB" >&2; exit 1; }
[ -d "$DEST" ]   || { echo "FATAL: backup dir missing (drive not mounted?): $DEST" >&2; exit 1; }
mountpoint -q "$DEST" 2>/dev/null || echo "WARN: $DEST is not a mountpoint — writing to the root fs?" >&2

# 1) WAL-safe online snapshot (NOT cp, NOT the sqlite3 CLI).
"$PYTHON" - "$DB" "$out" <<'PY'
import sqlite3, sys
src = sqlite3.connect(sys.argv[1])
dst = sqlite3.connect(sys.argv[2])
with dst:
    src.backup(dst)           # consistent online backup of the whole DB
src.close(); dst.close()
PY

# 2) Verify the snapshot immediately — a backup you haven't checked is a hope.
ic="$("$PYTHON" - "$out" <<'PY'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
print(c.execute("PRAGMA integrity_check").fetchone()[0])
c.close()
PY
)"
if [ "$ic" != "ok" ]; then
  echo "FATAL: integrity_check on fresh backup = '$ic'" >&2
  rm -f "$out"
  exit 1
fi
chmod 600 "$out" 2>/dev/null || true   # the DB holds defendant PII — keep it private

# 3) Capture the config/secret files + schema alongside the DB in the dated set.
#    Each lands as <label>-<stamp>, chmod 600 (these hold secrets).
for pair in $CONFIG_FILES; do
  label="${pair%%:*}"; path="${pair#*:}"
  if cp -f "$path" "$DEST/${label}-${stamp}" 2>/dev/null; then
    chmod 600 "$DEST/${label}-${stamp}" 2>/dev/null || true
  else
    echo "WARN: could not copy config $path (label=$label)" >&2
  fi
done
if [ -d "$MIGRATIONS" ]; then
  tar -czf "$DEST/migrations-${stamp}.tar.gz" \
    -C "$(dirname "$MIGRATIONS")" "$(basename "$MIGRATIONS")" \
    || echo "WARN: could not archive $MIGRATIONS" >&2
fi

# 4) Prune anything older than RETAIN_DAYS (cheap — DB is ~8 MB; root has ~86 GB).
find "$DEST" -maxdepth 1 -type f \
  \( -name "${dbname}-*.db" -o -name "*.env-*" -o -name "migrations-*.tar.gz" \) \
  -mtime +"$RETAIN_DAYS" -print -delete

bytes="$(stat -c %s "$out" 2>/dev/null || echo '?')"
echo "OK: $out (${bytes} bytes, integrity_check=ok); pruned backups older than ${RETAIN_DAYS}d"
