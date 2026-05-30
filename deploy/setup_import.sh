#!/usr/bin/env bash
# Installs the daily SharePoint import timer on ptr1. Run from the package root:
#   sudo bash deploy/setup_import.sh
set -euo pipefail
APP_DIR=/opt/ptr-knoxc
[[ $EUID -eq 0 ]] || { echo "run with sudo"; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 1. make sure the importer is in place
install -m 0755 "$PKG_ROOT/webapp/sharepoint_import.py" "$APP_DIR/webapp/sharepoint_import.py"
chown ptrapp:ptrapp "$APP_DIR/webapp/sharepoint_import.py"

# 2. seed the env file if missing
if [[ ! -f /etc/ptr-import.env ]]; then
  cp "$PKG_ROOT/deploy/ptr-import.env.example" /etc/ptr-import.env
  chmod 600 /etc/ptr-import.env
  echo "!! Edit /etc/ptr-import.env with your mailbox + app password."
fi

# 3. install + enable the timer
cp "$PKG_ROOT/deploy/ptr-import.service" /etc/systemd/system/ptr-import.service
cp "$PKG_ROOT/deploy/ptr-import.timer"   /etc/systemd/system/ptr-import.timer
systemctl daemon-reload
systemctl enable --now ptr-import.timer

cat <<MSG

Import timer installed.  Next:
  1. sudo nano /etc/ptr-import.env        # fill IMAP_USER / IMAP_PASS
  2. Test once now:   sudo systemctl start ptr-import.service
     Watch result:    journalctl -u ptr-import.service -n 40 --no-pager
  3. Timer status:    systemctl list-timers ptr-import.timer
MSG
