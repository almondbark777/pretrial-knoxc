#!/usr/bin/env bash
# install-on-ptr1.sh — deploy the Go app onto ptr1. Run ON ptr1 as `alex`,
# from an extracted bundle that contains: server, templates/, static/,
# ptr-webapp-go.service, and this script. Uses sudo for privileged steps
# (you'll be asked for your sudo password once). Safe + re-runnable:
#   - aborts BEFORE touching the live service if the DB isn't where expected
#   - backs up the current binary, unit, and DB first
#   - updates .env without clobbering existing secrets
#   - prints a one-line rollback at the end
set -euo pipefail

APP=/opt/ptr-knoxc
SVC=ptr-webapp
DB="$APP/db/kh222.db"                 # current DB (rename to pretrial_release.db not done yet)
SRC="$(cd "$(dirname "$0")" && pwd)"
STAMP="$(date +%Y%m%d-%H%M%S)"
BK="$APP/backups"

say(){ printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

say "0. sanity"
for f in server templates static ptr-webapp-go.service; do
  test -e "$SRC/$f" || { echo "  MISSING $f in bundle ($SRC)"; exit 1; }
done
echo "  bundle ok: $SRC"
sudo -v   # cache sudo creds (one prompt)

say "1. current state (before any change)"
systemctl is-active "$SVC" cloudflared ptr-import.timer 2>&1 | paste -sd' ' | sed 's/^/  active: /' || true
systemctl cat "$SVC" 2>/dev/null | grep -E 'ExecStart|EnvironmentFile' | sed 's/^/  /' || true
ls -la "$DB" 2>/dev/null | sed 's/^/  /' || true

say "2. require the DB (abort before touching the service if missing)"
if ! sudo test -f "$DB"; then
  echo "  ERROR: $DB not found. Nothing changed."
  echo "  Find the real path:  sudo find /opt/ptr-knoxc -name '*.db' ; then re-run after telling Claude the path."
  exit 1
fi
echo "  ok: $DB present"

say "3. safety backups -> $BK"
sudo mkdir -p "$BK"
[ -f "$APP/server" ] && sudo cp -a "$APP/server" "$BK/server.$STAMP" && echo "  saved server.$STAMP"
sudo cp -a "/etc/systemd/system/$SVC.service" "$BK/$SVC.service.$STAMP" 2>/dev/null && echo "  saved $SVC.service.$STAMP" || true
if command -v sqlite3 >/dev/null 2>&1; then
  sudo sqlite3 "$DB" ".backup '$BK/db.$STAMP.sqlite'" && echo "  DB online-backup -> db.$STAMP.sqlite"
else
  sudo cp -a "$DB" "$BK/db.$STAMP.sqlite" && echo "  DB file-copy -> db.$STAMP.sqlite (sqlite3 not installed)"
fi

say "4. install binary + templates + static + deploy scripts"
sudo install -m0755 "$SRC/server" "$APP/server"
sudo rm -rf "$APP/templates" "$APP/static"
sudo cp -r "$SRC/templates" "$SRC/static" "$APP/"
if id ptrapp >/dev/null 2>&1; then OWN=ptrapp:ptrapp; else OWN=root:root; fi
sudo chown -R "$OWN" "$APP/server" "$APP/templates" "$APP/static"
sudo mkdir -p "$APP/deploy"
for script in ptr-backup.sh install-netdata.sh; do
  [ -f "$SRC/$script" ] && sudo install -m0755 "$SRC/$script" "$APP/deploy/$script" && echo "  installed $script"
done
# The daily importer (ptr-import.service runs it from $APP/webapp). Backed up
# like the binary; skipped silently for older bundles that don't carry it.
if [ -f "$SRC/sharepoint_import.py" ]; then
  [ -f "$APP/webapp/sharepoint_import.py" ] && sudo cp -a "$APP/webapp/sharepoint_import.py" "$BK/sharepoint_import.py.$STAMP" && echo "  saved sharepoint_import.py.$STAMP"
  sudo install -m0755 "$SRC/sharepoint_import.py" "$APP/webapp/sharepoint_import.py"
  sudo chown "$OWN" "$APP/webapp/sharepoint_import.py" 2>/dev/null || true
  echo "  installed sharepoint_import.py (next import stamps import_meta -> the 'Data refreshed' footer)"
fi
# The CSV reconcile tool behind the web upload page (/console/import). It
# imports the column mapping from sharepoint_import.py — ships together.
if [ -f "$SRC/reconcile_import.py" ]; then
  [ -f "$APP/webapp/reconcile_import.py" ] && sudo cp -a "$APP/webapp/reconcile_import.py" "$BK/reconcile_import.py.$STAMP" && echo "  saved reconcile_import.py.$STAMP"
  sudo install -m0755 "$SRC/reconcile_import.py" "$APP/webapp/reconcile_import.py"
  sudo chown "$OWN" "$APP/webapp/reconcile_import.py" 2>/dev/null || true
  echo "  installed reconcile_import.py (backs the /console/import upload page)"
fi
echo "  installed (owner $OWN)"

say "5. update .env (idempotent; existing keys left as-is)"
ENV="$APP/webapp/.env"
sudo test -f "$ENV" || { echo "  WARN: $ENV missing; creating"; echo | sudo tee "$ENV" >/dev/null; }
add_kv(){ if sudo grep -q "^$1=" "$ENV"; then echo "  $1 already set (kept)"; else echo "$1=$2" | sudo tee -a "$ENV" >/dev/null; echo "  + $1"; fi; }
add_kv SUPERVISOR_EMAILS "Daniel.Harris@knoxsheriff.org,Justin.Webber@knoxsheriff.org,shellie.medford@knoxsheriff.org,Donna.Ogle@knoxsheriff.org,Renee.Russell@knoxsheriff.org,Stoney.Gentry@knoxsheriff.org,alexander.bentley@knoxsheriff.org"
add_kv IMPORTER_RETIRED false
add_kv COOKIE_SECURE true
sudo chown "$OWN" "$ENV" 2>/dev/null || true
sudo chmod 600 "$ENV" 2>/dev/null || true

say "6. install Go systemd unit (pointed at the CURRENT db path)"
sudo cp "$SRC/ptr-webapp-go.service" "/etc/systemd/system/$SVC.service"
sudo sed -i "s#^Environment=SQLITE_DB_PATH=.*#Environment=SQLITE_DB_PATH=$DB#" "/etc/systemd/system/$SVC.service"
sudo chmod 644 "/etc/systemd/system/$SVC.service"
sudo grep -E 'ExecStart|SQLITE_DB_PATH' "/etc/systemd/system/$SVC.service" | sed 's/^/  /' || true
sudo systemctl daemon-reload

say "7. restart + health"
sudo systemctl restart "$SVC"
sleep 2
systemctl is-active "$SVC" | sed 's/^/  service: /'
if curl -fsS http://127.0.0.1:8000/health; then
  echo "  <- /health OK"
  echo
  echo "  ✅ DEPLOYED. The Go app is live. Check it through the tunnel; run deploy/smoke.sh for a full check."
else
  echo "  ❌ HEALTH FAILED. Logs:  journalctl -u $SVC -n 40 --no-pager"
fi

say "rollback (if needed)"
echo "  sudo cp $BK/$SVC.service.$STAMP /etc/systemd/system/$SVC.service && sudo systemctl daemon-reload && sudo systemctl restart $SVC"
echo "  (that restores the previous service definition; the previous binary is $BK/server.$STAMP)"
