#!/usr/bin/env bash
# install-netdata.sh — set up Netdata host + service + app monitoring on ptr1.
# Run ON ptr1 as `alex`. Uses sudo for privileged steps (asked once). Safe +
# re-runnable: skips the install if Netdata is already present, and rewrites the
# two collector configs each run (idempotent).
#
# What it does:
#   - installs Netdata (stable, telemetry off) if not already installed
#   - binds the dashboard to 127.0.0.1 only (never public; view via SSH tunnel
#     or behind Cloudflare Access — see deploy/MONITORING.md)
#   - scrapes the app's /metrics endpoint (request rate / latency / errors)
#   - watches the ptr-webapp, cloudflared, and ptr-import systemd units
#   - leaves Netdata's rich built-in system alarms (CPU/RAM/disk/IO/OOM) on
#
# After this runs, see deploy/MONITORING.md for viewing + alert-notification setup.
set -euo pipefail

APP_METRICS_URL="http://127.0.0.1:8000/metrics"
NDDIR=""   # detected after install — varies by install method (see find_nddir)

say(){ printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

# find_nddir locates Netdata's config dir. Native packages use /etc/netdata; the
# kickstart static build uses /opt/netdata/etc/netdata. The edit-config script
# lives in the config dir on every install, so it's the reliable marker.
find_nddir(){
  for d in /etc/netdata /opt/netdata/etc/netdata; do
    if [ -x "$d/edit-config" ] || [ -f "$d/netdata.conf" ] || [ -d "$d" ]; then
      echo "$d"; return
    fi
  done
  echo /etc/netdata
}

# find_exe returns the first existing path among its args (for tools whose
# location also depends on install method), else empty.
find_exe(){ for p in "$@"; do [ -x "$p" ] && { echo "$p"; return; }; done; }

say "0. sudo"
sudo -v

say "1. install Netdata (skip if present)"
if command -v netdata >/dev/null 2>&1 || systemctl list-unit-files 2>/dev/null | grep -q '^netdata\.service'; then
  echo "  Netdata already installed — skipping install, just (re)configuring."
else
  echo "  running official kickstart (stable channel, telemetry disabled)…"
  wget -qO /tmp/netdata-kickstart.sh https://my-netdata.io/kickstart.sh \
    || curl -fsSL https://my-netdata.io/kickstart.sh -o /tmp/netdata-kickstart.sh
  sudo sh /tmp/netdata-kickstart.sh --stable-channel --disable-telemetry --non-interactive
fi

# Detect where THIS install put its config (native /etc/netdata vs static
# /opt/netdata/etc/netdata) — writing to the wrong dir would silently no-op.
NDDIR="$(find_nddir)"
echo "  config dir: $NDDIR"
sudo mkdir -p "$NDDIR/go.d"

say "2. bind dashboard to localhost only"
# Set [web] bind to = 127.0.0.1 in netdata.conf without disturbing other keys.
NDCONF="$NDDIR/netdata.conf"
if [ ! -f "$NDCONF" ]; then
  # Pull the running config the daemon serves, then edit it.
  sudo sh -c "curl -fsS http://127.0.0.1:19999/netdata.conf > '$NDCONF'" 2>/dev/null || true
fi
if [ -f "$NDCONF" ] && grep -qE '^\s*\[web\]' "$NDCONF"; then
  if grep -qE '^\s*bind to' "$NDCONF"; then
    sudo sed -i -E 's|^\s*bind to.*|	bind to = 127.0.0.1|' "$NDCONF"
  else
    sudo sed -i -E 's|^\s*\[web\]|[web]\n	bind to = 127.0.0.1|' "$NDCONF"
  fi
  echo "  netdata.conf [web] bind to = 127.0.0.1"
else
  # Minimal drop-in if we couldn't fetch the full file.
  printf '[web]\n\tbind to = 127.0.0.1\n' | sudo tee "$NDCONF" >/dev/null
  echo "  wrote minimal netdata.conf binding to 127.0.0.1"
fi

say "3. scrape the app /metrics endpoint (go.d prometheus collector)"
sudo tee "$NDDIR/go.d/prometheus.conf" >/dev/null <<EOF
# Managed by deploy/install-netdata.sh — scrapes the ptr-webapp Go app.
jobs:
  - name: ptr_webapp
    url: $APP_METRICS_URL
    # 1s default scrape; app exposes ptr_http_* + ptr_go_* series.
EOF
echo "  -> $NDDIR/go.d/prometheus.conf (job: ptr_webapp)"

say "4. watch the ptr systemd units (go.d systemdunits collector)"
sudo tee "$NDDIR/go.d/systemdunits.conf" >/dev/null <<'EOF'
# Managed by deploy/install-netdata.sh — surfaces up/down state of our units.
jobs:
  - name: ptr
    include:
      - 'ptr-webapp.service'
      - 'cloudflared.service'
      - 'ptr-import.service'
      - 'ptr-import.timer'
      - 'ptr-backup.service'
      - 'ptr-backup.timer'
EOF
echo "  -> $NDDIR/go.d/systemdunits.conf (ptr-webapp, cloudflared, import, backup)"

say "5. restart Netdata"
sudo systemctl restart netdata
sleep 3
systemctl is-active netdata | sed 's/^/  netdata: /'

say "6. verify"
if curl -fsS http://127.0.0.1:8000/metrics >/dev/null 2>&1; then
  echo "  app /metrics reachable ✔"
else
  echo "  ⚠ app /metrics NOT reachable at $APP_METRICS_URL"
  echo "    (deploy the current Go binary first — the /metrics endpoint ships with it.)"
fi
if curl -fsS "http://127.0.0.1:19999/api/v1/info" >/dev/null 2>&1; then
  echo "  Netdata API up on 127.0.0.1:19999 ✔"
  # Has Netdata picked up our app series yet?
  if curl -fsS "http://127.0.0.1:19999/api/v1/allmetrics?format=prometheus" 2>/dev/null | grep -q 'ptr_webapp'; then
    echo "  Netdata is collecting the ptr_webapp job ✔"
  else
    echo "  (ptr_webapp series not visible yet — give it ~10s, then recheck the dashboard.)"
  fi
else
  echo "  ⚠ Netdata API not responding on 127.0.0.1:19999 — check: journalctl -u netdata -n 40 --no-pager"
fi

say "7. (optional) phone-push alerts via ntfy"
# ntfy is the lowest-friction phone alert: no MTA, no account. Free public server
# is ntfy.sh; alarm text is low-sensitivity (e.g. "ptr-webapp down", "disk 90%"),
# but it IS a third party — self-host ntfy or use email instead if you prefer
# (see deploy/MONITORING.md). Skippable: blank answer (or no TTY) = no change.
NTFY_TOPIC=""
read -r -p "  ntfy topic for phone alerts (pick a long, hard-to-guess name; blank = skip): " NTFY_TOPIC 2>/dev/null || true
if [ -n "${NTFY_TOPIC:-}" ]; then
  NOTIFY="$NDDIR/health_alarm_notify.conf"
  # Materialize the editable copy without launching $EDITOR (EDITOR=true = no-op).
  if [ ! -f "$NOTIFY" ] && [ -x "$NDDIR/edit-config" ]; then
    sudo sh -c "cd '$NDDIR' && EDITOR=true ./edit-config health_alarm_notify.conf" >/dev/null 2>&1 || true
  fi
  if [ -f "$NOTIFY" ]; then
    set_kv(){ # key value — replace existing assignment or append
      if sudo grep -qE "^[[:space:]]*$1=" "$NOTIFY"; then
        sudo sed -i -E "s|^[[:space:]]*$1=.*|$1=\"$2\"|" "$NOTIFY"
      else
        echo "$1=\"$2\"" | sudo tee -a "$NOTIFY" >/dev/null
      fi
    }
    set_kv SEND_NTFY YES
    set_kv DEFAULT_RECIPIENT_NTFY "https://ntfy.sh/$NTFY_TOPIC"
    sudo systemctl restart netdata || true
    echo "  ntfy enabled -> https://ntfy.sh/$NTFY_TOPIC"
    echo "  Subscribe in the ntfy phone app (Add subscription -> topic: $NTFY_TOPIC)."
    AN="$(find_exe /usr/libexec/netdata/plugins.d/alarm-notify.sh /opt/netdata/usr/libexec/netdata/plugins.d/alarm-notify.sh)"
    if [ -n "$AN" ] && sudo "$AN" test >/dev/null 2>&1; then
      echo "  test alert sent — check your phone."
    else
      echo "  (couldn't auto-send a test; trigger one from the dashboard or wait for a real alarm.)"
    fi
  else
    echo "  couldn't create $NOTIFY automatically — set ntfy by hand (see deploy/MONITORING.md)."
  fi
else
  echo "  skipped — you can enable phone/email/Slack alerts later (see deploy/MONITORING.md)."
fi

cat <<'EONOTE'

== done ==
View the dashboard from OUTSIDE the office via Cloudflare Access (chosen path):
  set up the netdata-ptr.<domain> hostname per deploy/MONITORING.md, then browse
  https://netdata-ptr.<domain> with your @knoxsheriff.org login. Nothing new
  leaves the box — it rides your existing tunnel + Access gate.

Quick deep-dive fallback (no Cloudflare changes):
  ssh -L 19999:127.0.0.1:19999 alex@ptr1   then   http://localhost:19999

Netdata stays bound to 127.0.0.1 either way. Full notes: deploy/MONITORING.md.
EONOTE
